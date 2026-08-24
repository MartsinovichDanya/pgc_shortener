package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

// Record представляет одну запись в хранилище.
type Record struct {
	UUID        string `json:"uuid"`
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
	UserID      string `json:"user_id"`
	IsDeleted   bool   `json:"is_deleted"`
}

// FileStore – потокобезопасное in-memory хранилище сокращённых URL с сохранением в файл.
type FileStore struct {
	mu            sync.RWMutex
	data          map[string]Record
	originalIndex map[string]string
	filename      string
}

// NewFileStore создаёт новый экземпляр FileStore, реализующий Store.
// Если передан путь к файлу, данные загружаются из него; иначе – только в памяти.
func NewFileStore(filename ...string) (Store, error) {
	s := &FileStore{
		data:          make(map[string]Record),
		originalIndex: make(map[string]string),
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
			s.originalIndex[rec.OriginalURL] = rec.ShortURL
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("ошибка чтения файла хранилища: %w", err)
		}
	}

	return s, nil
}

// Save сохраняет пару (короткий идентификатор, оригинальный URL) для пользователя userID.
func (s *FileStore) Save(ctx context.Context, id, originalURL, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// O(1) проверка дубликата по обратному индексу (включая удалённые записи)
	if _, exists := s.originalIndex[originalURL]; exists {
		return ErrURLExists
	}

	record := Record{
		UUID:        utils.NewUUID(),
		ShortURL:    id,
		OriginalURL: originalURL,
		UserID:      userID,
		IsDeleted:   false,
	}
	s.data[id] = record
	s.originalIndex[originalURL] = id // обновляем индекс

	if s.filename != "" {
		if err := s.appendRecord(record); err != nil {
			return fmt.Errorf("ошибка сохранения в файл: %w", err)
		}
	}
	return nil
}

// SaveBatch сохраняет множество пар (id, originalURL) за одну операцию для пользователя userID.
func (s *FileStore) SaveBatch(ctx context.Context, records map[string]string, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Проверяем на дубликаты до вставки: как внутри самого батча, так и относительно существующих данных.
	seen := make(map[string]struct{}, len(records))
	for _, originalURL := range records {
		if _, exists := s.originalIndex[originalURL]; exists {
			return ErrURLExists
		}
		if _, exists := seen[originalURL]; exists {
			return ErrURLExists
		}
		seen[originalURL] = struct{}{}
	}

	newRecords := make([]Record, 0, len(records))
	for id, originalURL := range records {
		rec := Record{
			UUID:        utils.NewUUID(),
			ShortURL:    id,
			OriginalURL: originalURL,
			UserID:      userID,
			IsDeleted:   false,
		}
		s.data[id] = rec
		s.originalIndex[originalURL] = id // обновляем индекс
		newRecords = append(newRecords, rec)
	}

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

// BatchDelete помечает несколько ссылок как удалённые.
// Пользователь может удалять только свои ссылки.
// Обратный индекс не изменяется: он по-прежнему хранит соответствие originalURL -> shortURL
// для проверки дубликатов (включая удалённые), что соответствует поведению PostgresStore.
func (s *FileStore) BatchDelete(ctx context.Context, shortURLs []string, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(shortURLs) == 0 {
		return nil
	}

	for _, id := range shortURLs {
		rec, ok := s.data[id]
		if ok && rec.UserID == userID {
			rec.IsDeleted = true
			s.data[id] = rec
		}
		// Записи, не найденные или чужие, игнорируются
	}

	if s.filename != "" {
		if err := s.writeAll(); err != nil {
			return fmt.Errorf("ошибка перезаписи файла хранилища: %w", err)
		}
	}

	return nil
}

// Get возвращает оригинальный URL по идентификатору (только для неудалённых записей).
func (s *FileStore) Get(ctx context.Context, id string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.data[id]
	if !ok {
		return "", fmt.Errorf("идентификатор %s не найден", id)
	}
	if record.IsDeleted {
		return "", ErrURLDeleted
	}
	return record.OriginalURL, nil
}

// GetByOriginalURL возвращает короткий идентификатор по оригинальному URL (только для неудалённых записей).
// Использует обратный индекс для O(1) поиска.
func (s *FileStore) GetByOriginalURL(ctx context.Context, originalURL string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	shortID, exists := s.originalIndex[originalURL]
	if !exists {
		return "", fmt.Errorf("original URL не найден")
	}
	// Проверяем, что запись не удалена
	if rec, ok := s.data[shortID]; ok && rec.IsDeleted {
		return "", fmt.Errorf("original URL не найден")
	}
	return shortID, nil
}

// GetUserURLs возвращает все сокращённые пользователем ссылки (исключая удалённые).
func (s *FileStore) GetUserURLs(ctx context.Context, userID string) ([]model.UserURL, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []model.UserURL
	for _, rec := range s.data {
		if rec.UserID == userID && !rec.IsDeleted {
			result = append(result, model.UserURL{
				ShortURL:    rec.ShortURL,
				OriginalURL: rec.OriginalURL,
			})
		}
	}
	return result, nil
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

// writeAll полностью перезаписывает файл хранилища текущим содержимым map.
func (s *FileStore) writeAll() error {
	file, err := os.OpenFile(s.filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("не удалось открыть файл для перезаписи: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	for _, rec := range s.data {
		data, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("ошибка сериализации записи: %w", err)
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("ошибка записи в файл: %w", err)
		}
	}
	return nil
}
