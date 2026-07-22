package storage

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sync"
)

// Record представляет одну запись в хранилище.
type Record struct {
	UUID        string `json:"uuid"`
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
}

// Store – потокобезопасное in-memory хранилище сокращённых URL с опциональным сохранением в файл.
// Файл хранится в формате JSON Lines: каждая строка – отдельный JSON-объект Record.
type Store struct {
	mu       sync.RWMutex
	data     map[string]Record // ключ – короткий идентификатор (short_url)
	filename string            // если пустая строка – работа только в памяти
}

// NewStore создаёт новый экземпляр Store.
// Если передан путь к файлу, данные будут загружены из него (формат JSON Lines).
// Если файл не существует, он будет создан при первом вызове Save.
// Вызов без аргументов создаёт хранилище только в памяти.
func NewStore(filename ...string) (*Store, error) {
	s := &Store{
		data: make(map[string]Record),
	}

	if len(filename) > 0 && filename[0] != "" {
		s.filename = filename[0]

		file, err := os.Open(s.filename)
		if err != nil {
			if os.IsNotExist(err) {
				// Файла нет – это допустимо, хранилище пока пустое.
				return s, nil
			}
			return nil, fmt.Errorf("не удалось открыть файл хранилища: %w", err)
		}
		defer file.Close()

		// Читаем файл построчно, каждая строка – отдельная запись.
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
			// При дублировании short_url останется последняя запись (актуальная).
			s.data[rec.ShortURL] = rec
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("ошибка чтения файла хранилища: %w", err)
		}
	}

	return s, nil
}

// Save сохраняет пару (короткий идентификатор, оригинальный URL).
func (s *Store) Save(id, originalURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := Record{
		UUID:        newUUID(),
		ShortURL:    id,
		OriginalURL: originalURL,
	}
	s.data[id] = record

	if s.filename != "" {
		if err := s.appendRecord(record); err != nil {
			fmt.Fprintf(os.Stderr, "ошибка сохранения в файл: %v\n", err)
		}
	}
}

// Get возвращает оригинальный URL по идентификатору. Второе значение – флаг наличия.
func (s *Store) Get(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.data[id]
	if !ok {
		return "", false
	}
	return record.OriginalURL, true
}

// appendRecord дозаписывает одну запись в конец файла.
func (s *Store) appendRecord(rec Record) error {
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

// newUUID генерирует UUID версии 4.
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("не удалось сгенерировать UUID: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
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
