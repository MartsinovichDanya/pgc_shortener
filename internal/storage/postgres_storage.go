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

// migrationsFS встраивает SQL-миграции в бинарник.
// Файлы лежат в ./migrations/*.sql и применяются автоматически
// при создании PostgresStore через goose.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// PostgresStore — реализация Store на базе PostgreSQL (pgxpool).
//
// Все запросы выполняются через пул соединений, безопасный для
// параллельного использования. Удаление ссылок реализовано как мягкое:
// столбец is_deleted помечается TRUE, запись остаётся в таблице,
// но перестаёт возвращаться в Get, GetByOriginalURL и GetUserURLs.
//
// Схема БД создаётся/обновляется автоматически при вызове
// NewPostgresStore (через встроенные миграции goose).
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore создаёт PostgresStore и применяет миграции.
//
// databaseURL — строка подключения в формате, который понимает pgx
// (например, postgres://user:pass@host:5432/db?sslmode=disable).
//
// При создании:
//  1. создаётся пул соединений pgxpool;
//  2. последовательно применяются встроенные миграции из ./migrations
//     (goose.Up, идемпотентно — уже применённые пропускаются).
//
// Если миграции не применились, пул закрывается и возвращается ошибка,
// чтобы не оставлять висящих соединений.
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

// runMigrations применяет встроенные SQL-миграции через goose.
//
// Открывает отдельное подключение database/sql поверх драйвера "pgx"
// (это требование goose), устанавливает в качестве источника миграций
// migrationsFS и вызывает goose.Up. Соединение закрывается по завершении.
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
//
// Использует INSERT ... ON CONFLICT (original_url) DO NOTHING: если
// originalURL уже присутствует в таблице (в том числе как мягко удалённая
// запись), вставка не происходит и возвращается ErrURLExists.
// Вызывающий код в этом случае должен получить существующий shortID
// через GetByOriginalURL.
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

// SaveBatch сохраняет множество ссылок в одной транзакции с привязкой
// к пользователю.
//
// Все INSERT отправляются одним сообщением через pgx.Batch, что снижает
// число round-trip к БД. Транзакция атомарна: если хотя бы один запрос
// из батча завершится ошибкой, изменения откатываются.
//
// Конфликты по original_url обрабатываются через ON CONFLICT DO NOTHING
// (аналогично Save); факт конфликта при этом не возвращается — метод
// считает операцию успешной.
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
//
// Если запись не найдена, возвращает обёрнутую pgx.ErrNoRows —
// вызывающий код может отличить «нет записи» через errors.Is.
// Если запись помечена is_deleted = TRUE, возвращает ErrURLDeleted,
// на который handler отвечает 410 Gone.
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
//
// Мягко удалённые записи (is_deleted = TRUE) не возвращаются: по ним
// вернётся ошибка «не найден». Используется в handler при обработке
// конфликта ErrURLExists, чтобы вернуть клиенту уже существующую ссылку.
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

// GetUserURLs возвращает все неудалённые ссылки, принадлежащие пользователю.
//
// Возвращает nil-slice без ошибки, если у пользователя нет ссылок.
// Порядок строк определяется БД и не гарантирован.
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

// BatchDelete помечает несколько сокращённых ссылок как удалённые.
//
// Обновление выполняется в одной транзакции: либо все переданные ссылки
// будут помечены, либо ни одна. В UPDATE есть условие user_id = $2,
// поэтому пользователь может удалять только свои ссылки; чужие
// идентификаторы молча игнорируются (rows affected = 0).
//
// Пустой список shortURLs — no-op.
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
//
// Используется HTTP-обработчиком /ping. Реализует интерфейс storage.Pinger.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close закрывает пул соединений pgxpool.
//
// После вызова Close все методы PostgresStore становятся непригодны
// к использованию — попытки обращений вернут ошибку от pgxpool.
// Следует вызывать при завершении приложения.
func (s *PostgresStore) Close() {
	s.pool.Close()
}
