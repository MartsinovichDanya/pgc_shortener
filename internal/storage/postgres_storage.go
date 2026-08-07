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

func (s *PostgresStore) Save(id, originalURL string) error {
	uuid := utils.NewUUID()
	query := `
        INSERT INTO service_data.urls (uuid, short_url, original_url)
        VALUES ($1, $2, $3)
        ON CONFLICT (original_url) DO NOTHING;
    `
	tag, err := s.pool.Exec(context.Background(), query, uuid, id, originalURL)
	if err != nil {
		return fmt.Errorf("ошибка сохранения: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrURLExists
	}
	return nil
}

// SaveBatch сохраняет множество пар (id, originalURL) в одной транзакции.
func (s *PostgresStore) SaveBatch(records map[string]string) error {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("не удалось начать транзакцию: %w", err)
	}
	defer tx.Rollback(ctx) // откат в случае ошибки или паники

	query := `
		INSERT INTO service_data.urls (uuid, short_url, original_url)
		VALUES ($1, $2, $3)
		ON CONFLICT (original_url) DO NOTHING;
	`
	for id, originalURL := range records {
		uuid := utils.NewUUID()
		if _, err := tx.Exec(ctx, query, uuid, id, originalURL); err != nil {
			return fmt.Errorf("ошибка вставки для %s: %w", id, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ошибка коммита транзакции: %w", err)
	}
	return nil
}

func (s *PostgresStore) Get(id string) (string, error) {
	var originalURL string
	err := s.pool.QueryRow(context.Background(),
		`SELECT original_url FROM service_data.urls WHERE short_url = $1`, id,
	).Scan(&originalURL)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("url с идентификатором %s не найден: %w", id, err)
		}
		return "", fmt.Errorf("ошибка получения: %w", err)
	}
	return originalURL, nil
}

func (s *PostgresStore) GetByOriginalURL(originalURL string) (string, error) {
	var shortURL string
	err := s.pool.QueryRow(context.Background(),
		`SELECT short_url FROM service_data.urls WHERE original_url = $1`, originalURL,
	).Scan(&shortURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("original URL не найден: %w", err)
		}
		return "", fmt.Errorf("ошибка получения short URL: %w", err)
	}
	return shortURL, nil
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}
