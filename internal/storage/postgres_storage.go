package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(databaseURL string) (Store, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать пул подключений: %w", err)
	}

	if err := runMigrations(databaseURL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("не удалось применить миграции: %w", err)
	}

	return &PostgresStore{pool: pool}, nil
}

func runMigrations(databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("sql.Open: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrationsFS)

	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("goose.Up: %w", err)
	}
	return nil
}

// Save сохраняет короткую ссылку с привязкой к пользователю.
func (s *PostgresStore) Save(ctx context.Context, id, originalURL, userID string) error {
	uuid := utils.NewUUID()
	query := `
        INSERT INTO service_data.urls (uuid, short_url, original_url, user_id)
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (original_url) DO NOTHING;
    `
	tag, err := s.pool.Exec(ctx, query, uuid, id, originalURL, userID)
	if err != nil {
		return fmt.Errorf("ошибка сохранения: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrURLExists
	}
	return nil
}

// SaveBatch сохраняет множество ссылок в одной транзакции с привязкой к пользователю.
// Используется pgx.Batch для отправки всех INSERT одним сообщением.
func (s *PostgresStore) SaveBatch(ctx context.Context, records map[string]string, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("не удалось начать транзакцию: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	query := `
		INSERT INTO service_data.urls (uuid, short_url, original_url, user_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (original_url) DO NOTHING;
	`
	for id, originalURL := range records {
		uuid := utils.NewUUID()
		batch.Queue(query, uuid, id, originalURL, userID)
	}

	br := tx.SendBatch(ctx, batch)
	defer br.Close()

	// Обрабатываем результаты каждого запроса из батча.
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("ошибка вставки в батче (запрос %d): %w", i, err)
		}
	}

	if err := br.Close(); err != nil {
		return fmt.Errorf("ошибка закрытия батча: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ошибка коммита транзакции: %w", err)
	}
	return nil
}

// Get возвращает оригинальный URL по сокращённому идентификатору.
func (s *PostgresStore) Get(ctx context.Context, id string) (string, error) {
	var originalURL string
	var isDeleted bool
	err := s.pool.QueryRow(ctx,
		`SELECT original_url, is_deleted FROM service_data.urls WHERE short_url = $1`, id,
	).Scan(&originalURL, &isDeleted)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("url с идентификатором %s не найден: %w", id, err)
		}
		return "", fmt.Errorf("ошибка получения: %w", err)
	}
	if isDeleted {
		return "", ErrURLDeleted
	}
	return originalURL, nil
}

// GetByOriginalURL возвращает короткий идентификатор по оригинальному URL.
func (s *PostgresStore) GetByOriginalURL(ctx context.Context, originalURL string) (string, error) {
	var shortURL string
	err := s.pool.QueryRow(ctx,
		`SELECT short_url FROM service_data.urls 
         	 WHERE original_url = $1 AND is_deleted = FALSE`, originalURL,
	).Scan(&shortURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("original URL не найден: %w", err)
		}
		return "", fmt.Errorf("ошибка получения short URL: %w", err)
	}
	return shortURL, nil
}

// GetUserURLs возвращает все когда-либо сокращённые пользователем ссылки.
func (s *PostgresStore) GetUserURLs(ctx context.Context, userID string) ([]model.UserURL, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT short_url, original_url FROM service_data.urls 
         	 WHERE user_id = $1 AND is_deleted = FALSE`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения URL пользователя: %w", err)
	}
	defer rows.Close()

	var urls []model.UserURL
	for rows.Next() {
		var shortID, origURL string
		if err := rows.Scan(&shortID, &origURL); err != nil {
			return nil, fmt.Errorf("ошибка чтения строки: %w", err)
		}
		urls = append(urls, model.UserURL{
			ShortURL:    shortID,
			OriginalURL: origURL,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка итерации: %w", err)
	}

	return urls, nil
}

// BatchDelete помечает несколько сокращённых ссылок как удалённые одним запросом.
func (s *PostgresStore) BatchDelete(ctx context.Context, shortURLs []string, userID string) error {
	if len(shortURLs) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("не удалось начать транзакцию: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
        UPDATE service_data.urls
        SET is_deleted = TRUE
        WHERE short_url = $1 AND user_id = $2
    `
	for _, shortURL := range shortURLs {
		if _, err := tx.Exec(ctx, query, shortURL, userID); err != nil {
			return fmt.Errorf("ошибка обновления для %s: %w", shortURL, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ошибка коммита транзакции: %w", err)
	}
	return nil
}

// Ping проверяет доступность базы данных.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close закрывает пул соединений.
func (s *PostgresStore) Close() {
	s.pool.Close()
}
