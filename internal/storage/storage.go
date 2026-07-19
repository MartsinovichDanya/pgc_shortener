package storage

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
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

// Store – потокобезопасное in-memory хранилище сокращённых URL с сохранением в файл.
type Store struct {
	mu       sync.RWMutex
	data     map[string]Record // ключ – короткий идентификатор (short_url)
	filename string
}

// NewStore создаёт новый экземпляр Store, загружая данные из указанного файла.
// Если файл не существует, он будет создан с пустым JSON-массивом.
func NewStore(filename string) (*Store, error) {
	s := &Store{
		data:     make(map[string]Record),
		filename: filename,
	}

	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// Файла нет – создаём его с пустым массивом JSON
			if createErr := createEmptyFile(filename); createErr != nil {
				return nil, createErr
			}
			// Открываем только что созданный файл для чтения
			file, err = os.Open(filename)
			if err != nil {
				return nil, fmt.Errorf("не удалось открыть свежесозданный файл хранилища: %w", err)
			}
		} else {
			return nil, fmt.Errorf("не удалось открыть файл хранилища: %w", err)
		}
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	var records []Record
	if err := decoder.Decode(&records); err != nil {
		// если файл пуст, то io.EOF не является ошибкой
		if err == io.EOF {
			return s, nil
		}
		return nil, fmt.Errorf("ошибка чтения файла хранилища: %w", err)
	}

	for _, r := range records {
		s.data[r.ShortURL] = r
	}
	return s, nil
}

// createEmptyFile создаёт файл с пустым JSON-массивом "[]".
func createEmptyFile(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("не удалось создать файл хранилища: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString("[]")
	if err != nil {
		return fmt.Errorf("не удалось записать пустой массив в файл: %w", err)
	}
	return nil
}

// Save сохраняет пару (короткий идентификатор, оригинальный URL) и записывает всё хранилище в файл.
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
		if err := s.saveToFile(); err != nil {
			// логирование ошибки (заглушка, можно заменить на log.Printf)
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

// saveToFile записывает всё содержимое хранилища в JSON-файл.
// Вызывается только при удерживаемой блокировке записи.
func (s *Store) saveToFile() error {
	records := make([]Record, 0, len(s.data))
	for _, rec := range s.data {
		records = append(records, rec)
	}

	file, err := os.Create(s.filename)
	if err != nil {
		return fmt.Errorf("не удалось создать файл: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ") // для читаемости, соответствует примеру
	if err := encoder.Encode(records); err != nil {
		return fmt.Errorf("ошибка записи JSON: %w", err)
	}
	return nil
}

// newUUID генерирует UUID версии 4.
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("не удалось сгенерировать UUID: %v", err))
	}
	// устанавливаем версию 4 (биты 7-4 байта 6)
	b[6] = (b[6] & 0x0f) | 0x40
	// устанавливаем вариант 10 (биты 7-5 байта 8)
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
