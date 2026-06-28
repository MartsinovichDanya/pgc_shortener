package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ---------- Конфигурация ----------
const (
	defaultAddr     = "localhost:8080"        // адрес и порт сервера
	baseURL         = "http://localhost:8080" // базовый URL для сокращённых ссылок
	idLength        = 8                       // длина генерируемого идентификатора
	maxBodySize     = 2048                    // максимальный размер тела запроса (байт)
	readTimeout     = 5 * time.Second         // таймаут чтения запроса
	writeTimeout    = 10 * time.Second        // таймаут записи ответа
	shutdownTimeout = 5 * time.Second         // таймаут плавного завершения
)

// ---------- Хранилище ----------
// Потокобезопасное отображение короткий_id -> оригинальный_URL
type Store struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewStore() *Store {
	return &Store{
		data: make(map[string]string),
	}
}

func (s *Store) Save(id, originalURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = originalURL
}

func (s *Store) Get(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	originalURL, ok := s.data[id]
	return originalURL, ok
}

// ---------- Генерация короткого идентификатора ----------
var base62Chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateID() (string, error) {
	id := make([]byte, idLength)
	for i := range id {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(base62Chars))))
		if err != nil {
			return "", fmt.Errorf("ошибка генерации случайного числа: %w", err)
		}
		id[i] = base62Chars[idx.Int64()]
	}
	return string(id), nil
}

// ---------- Обработчики HTTP ----------
type ShortenerHandler struct {
	store *Store
}

func NewShortenerHandler(store *Store) *ShortenerHandler {
	return &ShortenerHandler{store: store}
}

// Главный маршрутизатор: различает POST / и GET /{id}
func (h *ShortenerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("Запрос: %s %s", r.Method, r.URL.Path)

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/":
		h.createShortLink(w, r)
	case r.Method == http.MethodGet && strings.Count(r.URL.Path, "/") == 1 && len(r.URL.Path) > 1:
		h.redirect(w, r)
	default:
		// Любой другой запрос считается некорректным → 400
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	}
}

// POST / – создание короткой ссылки
func (h *ShortenerHandler) createShortLink(w http.ResponseWriter, r *http.Request) {
	// Ограничиваем размер тела
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		log.Printf("Ошибка чтения тела: %v", err)
		http.Error(w, "Ошибка чтения запроса", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	originalURL := strings.TrimSpace(string(body))
	if originalURL == "" {
		http.Error(w, "URL не может быть пустым", http.StatusBadRequest)
		return
	}

	// Базовая валидация URL
	if !isValidURL(originalURL) {
		http.Error(w, "Некорректный URL", http.StatusBadRequest)
		return
	}

	// Генерируем уникальный идентификатор
	id, err := generateID()
	if err != nil {
		log.Printf("Ошибка генерации ID: %v", err)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	// Сохраняем в хранилище
	h.store.Save(id, originalURL)

	shortURL := fmt.Sprintf("%s/%s", baseURL, id)

	// Отправляем ответ 201
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprint(w, shortURL)
}

// GET /{id} – перенаправление на оригинальный URL
func (h *ShortenerHandler) redirect(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/")
	originalURL, found := h.store.Get(id)
	if !found {
		// Несуществующий идентификатор теперь также считается некорректным запросом
		log.Printf("Идентификатор %s не найден", id)
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
		return
	}

	log.Printf("Редирект: %s -> %s", id, originalURL)
	w.Header().Set("Location", originalURL)
	w.WriteHeader(http.StatusTemporaryRedirect)
}

// Проверка корректности URL (должен содержать схему http/https)
func isValidURL(raw string) bool {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// ---------- Запуск сервера с плавным завершением ----------
func main() {
	store := NewStore()
	handler := NewShortenerHandler(store)

	server := &http.Server{
		Addr:         defaultAddr,
		Handler:      handler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	// Запуск сервера в горутине
	go func() {
		log.Printf("Сервис сокращения ссылок запущен на %s", defaultAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Ошибка сервера: %v", err)
		}
	}()

	// Ожидание сигнала завершения
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Получен сигнал завершения, плавно останавливаем сервер...")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Ошибка при остановке сервера: %v", err)
	}
	log.Println("Сервер остановлен")
}
