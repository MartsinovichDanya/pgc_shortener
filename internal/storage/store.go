package storage

import (
	"errors"
)

var ErrURLExists = errors.New("original URL already exists")

type Store interface {
	Save(id, originalURL string) error
	SaveBatch(records map[string]string) error
	Get(id string) (string, error)
	GetByOriginalURL(originalURL string) (string, error)
}
