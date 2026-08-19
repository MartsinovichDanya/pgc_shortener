package storage

import (
	"errors"

	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
)

var ErrURLExists = errors.New("original URL already exists")
var ErrURLDeleted = errors.New("url is deleted")

type Store interface {
	Save(id string, originalURL string, userID string) error
	Get(id string) (string, error)
	GetByOriginalURL(originalURL string) (string, error)
	SaveBatch(records map[string]string, userID string) error
	GetUserURLs(userID string) ([]model.UserURL, error)
	BatchDelete(shortURLs []string, userID string) error
}
