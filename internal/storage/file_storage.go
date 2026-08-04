package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

// Record представляет одну запись в хранилище.
type Record struct {
	UUID        string `json:"uuid"`
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
}

// FileStore – потокобезопасное in-memory хранилище сокращённых URL с сохранением в файл.
type FileStore struct {
	mu       sync.RWMutex
	data     map[string]Record
	filename string
}

// NewFileStore создаёт новый экземпляр FileStore, реализующий Store.
// Если передан путь к файлу, данные загружаются из него; иначе – только в памяти.
func NewFileStore(filename ...string) (Store, error) {
	s := &FileStore{
		data: make(map[string]Record),
	}

	if len(filename) > 0 && filename[0] != "" {
		s.filename = filename[0]

		file, err := os.Open(s.filename)
		if err != nil {
			if os.IsNotExist(err) {
				return s, nil
			}
			return nil, fmt.Errorf("не удалось открыть файл хранилища: %w", err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var rec Record
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				return nil, fmt.Errorf("ошибка разбора строки хранилища: %w", err)
			}
			s.data[rec.ShortURL] = rec
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("ошибка чтения файла хранилища: %w", err)
		}
	}

	return s, nil
}

// Save сохраняет пару (короткий идентификатор, оригинальный URL).
func (s *FileStore) Save(id, originalURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Проверяем, нет ли уже такого original_url
	for _, rec := range s.data {
		if rec.OriginalURL == originalURL {
			return ErrURLExists
		}
	}

	record := Record{
		UUID:        utils.NewUUID(),
		ShortURL:    id,
		OriginalURL: originalURL,
	}
	s.data[id] = record

	if s.filename != "" {
		if err := s.appendRecord(record); err != nil {
			return fmt.Errorf("ошибка сохранения в файл: %w", err)
		}
	}
	return nil
}

// SaveBatch сохраняет множество пар (id, originalURL) за одну операцию.
// Все записи попадают в память и файл (если задан) с одной блокировкой.
func (s *FileStore) SaveBatch(records map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Собираем все новые записи
	newRecords := make([]Record, 0, len(records))
	for id, originalURL := range records {
		rec := Record{
			UUID:        utils.NewUUID(),
			ShortURL:    id,
			OriginalURL: originalURL,
		}
		s.data[id] = rec
		newRecords = append(newRecords, rec)
	}

	// Пишем в файл, если путь задан
	if s.filename != "" {
		file, err := os.OpenFile(s.filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return fmt.Errorf("не удалось открыть файл для пакетной записи: %w", err)
		}
		defer file.Close()

		for _, rec := range newRecords {
			data, err := json.Marshal(rec)
			if err != nil {
				return fmt.Errorf("ошибка сериализации записи: %w", err)
			}
			if _, err := file.Write(append(data, '\n')); err != nil {
				return fmt.Errorf("ошибка записи в файл: %w", err)
			}
		}
	}

	return nil
}

// Get возвращает оригинальный URL по идентификатору.
func (s *FileStore) Get(id string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.data[id]
	if !ok {
		return "", fmt.Errorf("идентификатор %s не найден", id)
	}
	return record.OriginalURL, nil
}

func (s *FileStore) GetByOriginalURL(originalURL string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rec := range s.data {
		if rec.OriginalURL == originalURL {
			return rec.ShortURL, nil
		}
	}
	return "", fmt.Errorf("original URL не найден")
}

// appendRecord дописывает запись в конец файла.
func (s *FileStore) appendRecord(rec Record) error {
	file, err := os.OpenFile(s.filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("не удалось открыть файл для дозаписи: %w", err)
	}
	defer file.Close()

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("ошибка сериализации записи: %w", err)
	}

	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("ошибка дозаписи в файл: %w", err)
	}
	return nil
}
