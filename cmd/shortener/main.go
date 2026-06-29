package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// ---------- Конфигурация ----------
const (
	defaultAddr = "localhost:8080"
	baseURL     = "http://localhost:8080"
	idLength    = 8
	maxBodySize = 2048
)

// ---------- Хранилище ----------
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

// POST / – создание короткой ссылки
func (h *ShortenerHandler) createShortLink(w http.ResponseWriter, r *http.Request) {
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

	if !isValidURL(originalURL) {
		http.Error(w, "Некорректный URL", http.StatusBadRequest)
		return
	}

	id, err := generateID()
	if err != nil {
		log.Printf("Ошибка генерации ID: %v", err)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	h.store.Save(id, originalURL)
	shortURL := fmt.Sprintf("%s/%s", baseURL, id)

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprint(w, shortURL)
}

// GET /{id} – перенаправление на оригинальный URL
func (h *ShortenerHandler) redirect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id") // извлечение параметра маршрута
	originalURL, found := h.store.Get(id)
	if !found {
		log.Printf("Идентификатор %s не найден", id)
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
		return
	}

	log.Printf("Редирект: %s -> %s", id, originalURL)
	w.Header().Set("Location", originalURL)
	w.WriteHeader(http.StatusTemporaryRedirect)
}

// Проверка корректности URL
func isValidURL(raw string) bool {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func newRouter(store *Store) chi.Router {
	handler := NewShortenerHandler(store)
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/", handler.createShortLink)
	r.Get("/{id}", handler.redirect)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})

	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})

	return r
}

// ---------- Запуск сервера с плавным завершением ----------
func main() {
	store := NewStore()
	r := newRouter(store)

	log.Fatal(http.ListenAndServe(defaultAddr, r))
}
