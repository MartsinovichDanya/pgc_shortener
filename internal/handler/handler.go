package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
)

// ShortenerHandler содержит зависимости HTTP-обработчиков.
type ShortenerHandler struct {
	Store       *storage.Store
	BaseURL     string
	MaxBodySize int
	IDLength    int
}

// NewShortenerHandler – конструктор обработчиков.
func NewShortenerHandler(store *storage.Store, baseURL string, maxBodySize, idLength int) *ShortenerHandler {
	return &ShortenerHandler{
		Store:       store,
		BaseURL:     baseURL,
		MaxBodySize: maxBodySize,
		IDLength:    idLength,
	}
}

// CreateShortLink обрабатывает POST / – создание короткой ссылки.
func (h *ShortenerHandler) CreateShortLink(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(h.MaxBodySize)))
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

	id, err := storage.GenerateID(h.IDLength)
	if err != nil {
		log.Printf("Ошибка генерации ID: %v", err)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	h.Store.Save(id, originalURL)
	shortURL := fmt.Sprintf("%s/%s", h.BaseURL, id)

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprint(w, shortURL)
}

// Redirect обрабатывает GET /{id} – перенаправление на оригинальный URL.
func (h *ShortenerHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	originalURL, found := h.Store.Get(id)
	if !found {
		log.Printf("Идентификатор %s не найден", id)
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
		return
	}

	log.Printf("Редирект: %s -> %s", id, originalURL)
	w.Header().Set("Location", originalURL)
	w.WriteHeader(http.StatusTemporaryRedirect)
}

// isValidURL проверяет, что строка является валидным HTTP(S) URL.
func isValidURL(raw string) bool {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}
