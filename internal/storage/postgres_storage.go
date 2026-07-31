package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

// PostgresStore реализует интерфейс Store для работы с PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore создаёт новое хранилище, работающее с PostgreSQL.
// Принимает строку подключения (например, "postgres://user:pass@localhost/dbname").
// Автоматически создаёт таблицу urls, если она ещё не существует.
func NewPostgresStore(databaseURL string) (Store, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать пул подключений к PostgreSQL: %w", err)
	}

	// Создаём таблицу, если её нет
	createTableSQL := `
        CREATE TABLE IF NOT EXISTS service_data.urls (
            uuid         UUID PRIMARY KEY,
            short_url    TEXT NOT NULL UNIQUE,
            original_url TEXT NOT NULL
        );
    `
	if _, err := pool.Exec(context.Background(), createTableSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("не удалось создать таблицу urls: %w", err)
	}

	return &PostgresStore{pool: pool}, nil
}

// Save сохраняет пару (короткий идентификатор, оригинальный URL).
// Если запись с таким short_url уже существует, её original_url и uuid обновляются.
// Возвращает ошибку в случае проблем с базой данных.
func (s *PostgresStore) Save(id, originalURL string) error {
	uuid := utils.NewUUID()
	query := `
        INSERT INTO service_data.urls (uuid, short_url, original_url)
        VALUES ($1, $2, $3)
        ON CONFLICT (short_url) DO UPDATE
            SET original_url = EXCLUDED.original_url,
                uuid         = EXCLUDED.uuid;
    `
	_, err := s.pool.Exec(context.Background(), query, uuid, id, originalURL)
	if err != nil {
		return fmt.Errorf("ошибка сохранения в postgres: %w", err)
	}
	return nil
}

// Get возвращает оригинальный URL по короткому идентификатору.
// Если запись не найдена, возвращает ошибку, которую можно проверить через errors.Is(err, pgx.ErrNoRows).
func (s *PostgresStore) Get(id string) (string, error) {
	var originalURL string
	err := s.pool.QueryRow(context.Background(),
		`SELECT original_url FROM service_data.urls WHERE short_url = $1`, id,
	).Scan(&originalURL)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("url с идентификатором %s не найден: %w", id, err)
		}
		return "", fmt.Errorf("ошибка получения из postgres: %w", err)
	}
	return originalURL, nil
}

// Ping проверяет доступность базы данных. (опционально)
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close освобождает пул соединений с базой данных. (опционально)
func (s *PostgresStore) Close() {
	s.pool.Close()
}
