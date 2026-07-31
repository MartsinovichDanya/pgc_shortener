package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mailru/easyjson"
	"go.uber.org/zap"

	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

// ShortenerHandler содержит зависимости HTTP-обработчиков.
type ShortenerHandler struct {
	Store       storage.Store // просто интерфейс, без указателя
	BaseURL     string
	MaxBodySize int
	IDLength    int
	UseDB       bool
}

// NewShortenerHandler – конструктор обработчиков.
func NewShortenerHandler(store storage.Store, baseURL string, maxBodySize, idLength int, UseDB bool) *ShortenerHandler {
	return &ShortenerHandler{
		Store:       store,
		BaseURL:     baseURL,
		MaxBodySize: maxBodySize,
		IDLength:    idLength,
		UseDB:       UseDB,
	}
}

// CreateShortLink обрабатывает POST / – создание короткой ссылки.
func (h *ShortenerHandler) CreateShortLink(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(h.MaxBodySize)))
	if err != nil {
		logger.Log.Debug("Ошибка чтения тела", zap.Error(err))
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

	id, err := utils.GenerateID(h.IDLength)
	if err != nil {
		logger.Log.Debug("Ошибка генерации ID", zap.Error(err))
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	// Теперь обрабатываем ошибку сохранения
	if err := h.Store.Save(id, originalURL); err != nil {
		logger.Log.Error("Ошибка сохранения URL", zap.Error(err))
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	shortURL := fmt.Sprintf("%s/%s", h.BaseURL, id)

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprint(w, shortURL)
}

// ShortenAPI обрабатывает POST /api/shorten – создание короткой ссылки через JSON.
func (h *ShortenerHandler) ShortenAPI(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(h.MaxBodySize)))
	if err != nil {
		writeJSONError(w, "Ошибка чтения запроса", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req model.Request
	if err := easyjson.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Некорректный JSON", http.StatusBadRequest)
		return
	}

	originalURL := strings.TrimSpace(req.URL)
	if originalURL == "" {
		writeJSONError(w, "URL не может быть пустым", http.StatusBadRequest)
		return
	}

	if !isValidURL(originalURL) {
		writeJSONError(w, "Некорректный URL", http.StatusBadRequest)
		return
	}

	id, err := utils.GenerateID(h.IDLength)
	if err != nil {
		logger.Log.Debug("Ошибка генерации ID", zap.Error(err))
		writeJSONError(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	if err := h.Store.Save(id, originalURL); err != nil {
		logger.Log.Error("Ошибка сохранения URL", zap.Error(err))
		writeJSONError(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	shortURL := fmt.Sprintf("%s/%s", h.BaseURL, id)
	resp := model.Response{Result: shortURL}
	jsonResp, err := easyjson.Marshal(resp)
	if err != nil {
		writeJSONError(w, "Ошибка формирования ответа", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(jsonResp)
}

// Redirect обрабатывает GET /{id} – перенаправление на оригинальный URL.
func (h *ShortenerHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	originalURL, err := h.Store.Get(id)
	if err != nil {
		logger.Log.Debug("Идентификатор не найден", zap.String("id", id), zap.Error(err))
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
		return
	}

	logger.Log.Debug("Редирект", zap.String("id", id), zap.String("originalURL", originalURL))
	w.Header().Set("Location", originalURL)
	w.WriteHeader(http.StatusTemporaryRedirect)
}

func (h *ShortenerHandler) PingHandler(w http.ResponseWriter, r *http.Request) {
	if !h.UseDB {
		w.WriteHeader(http.StatusOK)
		return
	}

	// При использовании БД хранилище должно поддерживать интерфейс pinger
	type pinger interface {
		Ping(context.Context) error
	}
	p, ok := h.Store.(pinger)
	if !ok {
		logger.Log.Error("хранилище не поддерживает Ping")
		http.Error(w, "Ping not supported", http.StatusInternalServerError)
		return
	}

	if err := p.Ping(r.Context()); err != nil {
		logger.Log.Error("ошибка подключения к базе данных", zap.Error(err))
		http.Error(w, "Database ping failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// writeJSONError отправляет JSON-ошибку с заданным статусом.
func writeJSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// isValidURL проверяет, что строка является валидным HTTP(S) URL.
func isValidURL(raw string) bool {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}
