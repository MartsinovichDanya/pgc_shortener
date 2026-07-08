package storage

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
)

// Store – потокобезопасное in-memory хранилище сокращённых URL.
type Store struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewStore создаёт новый экземпляр Store.
func NewStore() *Store {
	return &Store{
		data: make(map[string]string),
	}
}

// Save сохраняет пару (короткий идентификатор, оригинальный URL).
func (s *Store) Save(id, originalURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = originalURL
}

// Get возвращает оригинальный URL по идентификатору. Второе значение – флаг наличия.
func (s *Store) Get(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	originalURL, ok := s.data[id]
	return originalURL, ok
}

// base62Chars – алфавит для генерации коротких идентификаторов.
var base62Chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateID создаёт криптографически случайный идентификатор заданной длины.
func GenerateID(length int) (string, error) {
	id := make([]byte, length)
	for i := range id {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(base62Chars))))
		if err != nil {
			return "", fmt.Errorf("ошибка генерации случайного числа: %w", err)
		}
		id[i] = base62Chars[idx.Int64()]
	}
	return string(id), nil
}
