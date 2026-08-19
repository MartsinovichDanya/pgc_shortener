package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/mailru/easyjson"
	"go.uber.org/zap"

	"github.com/MartsinovichDanya/pgc_shortener/internal/auth"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

// ShortenerHandler содержит зависимости HTTP-обработчиков.
type ShortenerHandler struct {
	Store       storage.Store // интерфейс изменён: Save и SaveBatch теперь принимают userID
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

	// Извлекаем userID из контекста (установлен middleware)
	userID, _ := r.Context().Value(auth.UserIDKey).(string)

	// Сохраняем с привязкой к пользователю
	if err := h.Store.Save(id, originalURL, userID); err != nil {
		if errors.Is(err, storage.ErrURLExists) {
			existingShort, errGet := h.Store.GetByOriginalURL(originalURL)
			if errGet != nil {
				logger.Log.Error("Ошибка получения существующего URL", zap.Error(errGet))
				http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
				return
			}
			fullShortURL := fmt.Sprintf("%s/%s", h.BaseURL, existingShort)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, fullShortURL)
			return
		}
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

	// Получаем userID из контекста
	userID, _ := r.Context().Value(auth.UserIDKey).(string)

	if err := h.Store.Save(id, originalURL, userID); err != nil {
		if errors.Is(err, storage.ErrURLExists) {
			existingShort, errGet := h.Store.GetByOriginalURL(originalURL)
			if errGet != nil {
				logger.Log.Error("Ошибка получения существующего URL", zap.Error(errGet))
				writeJSONError(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
				return
			}
			fullShortURL := fmt.Sprintf("%s/%s", h.BaseURL, existingShort)
			resp := model.Response{Result: fullShortURL}
			jsonResp, errMarshal := easyjson.Marshal(resp)
			if errMarshal != nil {
				writeJSONError(w, "Ошибка формирования ответа", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write(jsonResp)
			return
		}

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
		if errors.Is(err, storage.ErrURLDeleted) {
			http.Error(w, "Gone", http.StatusGone)
			return
		}
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

func (h *ShortenerHandler) ShortenBatch(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(h.MaxBodySize)))
	if err != nil {
		writeJSONError(w, "Ошибка чтения тела", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var items []model.BatchRequestItem
	if err := json.Unmarshal(body, &items); err != nil {
		writeJSONError(w, "Некорректный JSON", http.StatusBadRequest)
		return
	}

	if len(items) == 0 {
		writeJSONError(w, "Пустой список URL", http.StatusBadRequest)
		return
	}

	type corrToID struct {
		correlationID string
		shortID       string
	}
	corrList := make([]corrToID, 0, len(items))
	records := make(map[string]string, len(items))

	for _, item := range items {
		cid := strings.TrimSpace(item.CorrelationID)
		if cid == "" {
			writeJSONError(w, "correlation_id не может быть пустым", http.StatusBadRequest)
			return
		}

		origURL := strings.TrimSpace(item.OriginalURL)
		if origURL == "" {
			writeJSONError(w, "original_url не может быть пустым", http.StatusBadRequest)
			return
		}
		if !isValidURL(origURL) {
			writeJSONError(w, fmt.Sprintf("Некорректный URL: %s", origURL), http.StatusBadRequest)
			return
		}

		id, err := utils.GenerateID(h.IDLength)
		if err != nil {
			logger.Log.Debug("Ошибка генерации ID", zap.Error(err))
			writeJSONError(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
			return
		}

		records[id] = origURL
		corrList = append(corrList, corrToID{correlationID: cid, shortID: id})
	}

	// Получаем userID для привязки всех ссылок
	userID, _ := r.Context().Value(auth.UserIDKey).(string)

	// Атомарная пакетная вставка
	if err := h.Store.SaveBatch(records, userID); err != nil {
		logger.Log.Error("Ошибка пакетного сохранения", zap.Error(err))
		writeJSONError(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	resp := make([]model.BatchResponseItem, 0, len(corrList))
	for _, c := range corrList {
		shortURL := fmt.Sprintf("%s/%s", h.BaseURL, c.shortID)
		resp = append(resp, model.BatchResponseItem{
			CorrelationID: c.correlationID,
			ShortURL:      shortURL,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Log.Debug("Ошибка записи ответа", zap.Error(err))
	}
}

// UserURLs обрабатывает GET /api/user/urls – возвращает все ссылки пользователя.
func (h *ShortenerHandler) UserURLs(w http.ResponseWriter, r *http.Request) {
	// Если при проверке куки был обнаружен невалидный токен – 401
	if invalid, ok := r.Context().Value(auth.TokenInvalidKey).(bool); ok && invalid {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "user token is invalid"})
		return
	}

	userID, ok := r.Context().Value(auth.UserIDKey).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	urls, err := h.Store.GetUserURLs(userID)
	if err != nil {
		logger.Log.Error("Ошибка получения URL пользователя", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	for i := range urls {
		urls[i].ShortURL = fmt.Sprintf("%s/%s", h.BaseURL, urls[i].ShortURL)
	}

	if len(urls) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(urls); err != nil {
		logger.Log.Debug("Ошибка сериализации ответа", zap.Error(err))
	}
}

// DeleteUserURLs обрабатывает DELETE /api/user/urls.
func (h *ShortenerHandler) DeleteUserURLs(w http.ResponseWriter, r *http.Request) {
	// Если токен был невалиден – сразу 401, как в UserURLs
	if invalid, ok := r.Context().Value(auth.TokenInvalidKey).(bool); ok && invalid {
		writeJSONError(w, "user token is invalid", http.StatusUnauthorized)
		return
	}

	// userID гарантированно присутствует после middleware
	userID, _ := r.Context().Value(auth.UserIDKey).(string)

	body, err := io.ReadAll(io.LimitReader(r.Body, int64(h.MaxBodySize)))
	if err != nil {
		writeJSONError(w, "Ошибка чтения тела", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var shortURLs []string
	if err := json.Unmarshal(body, &shortURLs); err != nil {
		writeJSONError(w, "Некорректный JSON", http.StatusBadRequest)
		return
	}

	if len(shortURLs) == 0 {
		writeJSONError(w, "Список идентификаторов пуст", http.StatusBadRequest)
		return
	}

	// Запускаем асинхронное удаление с использованием паттерна fan-in
	go h.deleteURLsAsync(shortURLs, userID)

	// Немедленно отвечаем 202 Accepted
	w.WriteHeader(http.StatusAccepted)
}

// deleteURLsAsync выполняет удаление в фоне, используя fan-in для параллельной обработки батчей.
func (h *ShortenerHandler) deleteURLsAsync(shortURLs []string, userID string) {
	const numWorkers = 4

	// Канал для батчей идентификаторов
	batchCh := make(chan []string, numWorkers)

	// Разбиваем список на батчи
	batchSize := (len(shortURLs) + numWorkers - 1) / numWorkers
	for i := 0; i < len(shortURLs); i += batchSize {
		end := i + batchSize
		if end > len(shortURLs) {
			end = len(shortURLs)
		}
		batchCh <- shortURLs[i:end]
	}
	close(batchCh)

	// Канал для сбора ошибок (fan-in)
	errCh := make(chan error, numWorkers)

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range batchCh {
				if err := h.Store.BatchDelete(batch, userID); err != nil {
					errCh <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	// Логируем ошибки (результат пользователю не отправляется)
	for err := range errCh {
		logger.Log.Error("Ошибка удаления URL", zap.Error(err))
	}
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
