package storage

type Store interface {
	Save(id, originalURL string) error
	SaveBatch(records map[string]string) error // новый метод
	Get(id string) (string, error)
}
