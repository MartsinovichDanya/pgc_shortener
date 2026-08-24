package storage

import (
	"context"
	"errors"

	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
)

var ErrURLExists = errors.New("original URL already exists")
var ErrURLDeleted = errors.New("url is deleted")

type Pinger interface {
	Ping(ctx context.Context) error
}

type Store interface {
	Save(ctx context.Context, id string, originalURL string, userID string) error
	Get(ctx context.Context, id string) (string, error)
	GetByOriginalURL(ctx context.Context, originalURL string) (string, error)
	SaveBatch(ctx context.Context, records map[string]string, userID string) error
	GetUserURLs(ctx context.Context, userID string) ([]model.UserURL, error)
	BatchDelete(ctx context.Context, shortURLs []string, userID string) error
}
