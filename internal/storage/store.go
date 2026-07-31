package storage

type Store interface {
	Save(id, originalURL string) error
	Get(id string) (string, error)
}
