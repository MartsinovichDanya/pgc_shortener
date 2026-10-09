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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mailru/easyjson"
	"go.uber.org/zap"

	"github.com/MartsinovichDanya/pgc_shortener/internal/audit"
	"github.com/MartsinovichDanya/pgc_shortener/internal/auth"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

// ShortenerHandler содержит зависимости HTTP-обработчиков.
type ShortenerHandler struct {
	Store         storage.Store
	BaseURL       string
	MaxBodySize   int
	IDLength      int
	UseDB         bool
	auditObserver audit.Observer

	deleteCh        chan deleteRequest
	deleteWg        sync.WaitGroup
	deleteCloseOnce sync.Once
}

// deleteRequest — элемент очереди на удаление.
type deleteRequest struct {
	userID  string
	shortID string
}

// NewShortenerHandler – конструктор обработчиков.
func NewShortenerHandler(store storage.Store, baseURL string, maxBodySize, idLength int, UseDB bool, auditObserver audit.Observer) *ShortenerHandler {
	if auditObserver == nil {
		auditObserver = audit.NoopNotifier{}
	}

	h := &ShortenerHandler{
		Store:         store,
		BaseURL:       baseURL,
		MaxBodySize:   maxBodySize,
		IDLength:      idLength,
		UseDB:         UseDB,
		auditObserver: auditObserver,
		deleteCh:      make(chan deleteRequest, 1000), // буфер на 1000 элементов
	}

	h.deleteWg.Add(1)
	go h.deleteWorker()
	return h
}

// Shutdown корректно останавливает фоновый воркер удаления.
func (h *ShortenerHandler) Shutdown() {
	h.deleteCloseOnce.Do(func() {
		close(h.deleteCh) // сигнал воркеру: обработать все оставшиеся элементы и завершиться
		h.deleteWg.Wait()
	})
}

// CreateShortLink godoc
// @Summary      Создать короткую ссылку
// @Tags         links
// @Accept       text/plain
// @Produce      text/plain
// @Param        url body string true "Оригинальный URL"
// @Success      201 {string} string "Короткая ссылка"
// @Success      409 {string} string "URL уже существует"
// @Failure      400 {string} string "Некорректный запрос"
// @Failure      500 {string} string "Внутренняя ошибка сервера"
// @Router       / [post]
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
	if err := h.Store.Save(r.Context(), id, originalURL, userID); err != nil {
		if errors.Is(err, storage.ErrURLExists) {
			existingShort, errGet := h.Store.GetByOriginalURL(r.Context(), originalURL)
			if errGet != nil {
				logger.Log.Error("Ошибка получения существующего URL", zap.Error(errGet))
				http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
				return
			}
			fullShortURL := joinShortURL(h.BaseURL, existingShort)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, fullShortURL)
			return
		}
		logger.Log.Error("Ошибка сохранения URL", zap.Error(err))
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	shortURL := joinShortURL(h.BaseURL, id)

	h.auditObserver.Notify(audit.NewEvent(audit.ActionShorten, userID, originalURL))

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprint(w, shortURL)
}

// ShortenAPI godoc
// @Summary      Создать короткую ссылку (JSON)
// @Tags         links
// @Accept       json
// @Produce      json
// @Param        request body model.Request true "Запрос"
// @Success      201 {object} model.Response
// @Success      409 {object} model.Response "URL уже существует"
// @Failure      400 {object} model.ErrorResponse
// @Failure      500 {object} model.ErrorResponse
// @Router       /api/shorten [post]
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

	if err := h.Store.Save(r.Context(), id, originalURL, userID); err != nil {
		if errors.Is(err, storage.ErrURLExists) {
			existingShort, errGet := h.Store.GetByOriginalURL(r.Context(), originalURL)
			if errGet != nil {
				logger.Log.Error("Ошибка получения существующего URL", zap.Error(errGet))
				writeJSONError(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
				return
			}
			fullShortURL := joinShortURL(h.BaseURL, existingShort)
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

	shortURL := joinShortURL(h.BaseURL, id)
	resp := model.Response{Result: shortURL}
	jsonResp, err := easyjson.Marshal(resp)
	if err != nil {
		writeJSONError(w, "Ошибка формирования ответа", http.StatusInternalServerError)
		return
	}

	h.auditObserver.Notify(audit.NewEvent(audit.ActionShorten, userID, originalURL))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(jsonResp)
}

// Redirect godoc
// @Summary      Редирект на оригинальный URL
// @Tags         links
// @Param        id path string true "Короткий идентификатор"
// @Success      307 "Temporary Redirect"
// @Failure      400 {string} string "Некорректный запрос"
// @Failure      410 {string} string "Ссылка удалена"
// @Router       /{id} [get]
func (h *ShortenerHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	originalURL, err := h.Store.Get(r.Context(), id)
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

	userID, _ := r.Context().Value(auth.UserIDKey).(string)
	h.auditObserver.Notify(audit.NewEvent(audit.ActionFollow, userID, originalURL))

	w.Header().Set("Location", originalURL)
	w.WriteHeader(http.StatusTemporaryRedirect)
}

// PingHandler godoc
// @Summary      Проверка соединения с БД
// @Tags         system
// @Success      200 "OK"
// @Failure      500 {string} string "Database ping failed"
// @Router       /ping [get]
func (h *ShortenerHandler) PingHandler(w http.ResponseWriter, r *http.Request) {
	if !h.UseDB {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Проверяем, реализует ли хранилище интерфейс storage.Pinger
	p, ok := h.Store.(storage.Pinger)
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

// ShortenBatch godoc
// @Summary      Массовое создание коротких ссылок
// @Tags         links
// @Accept       json
// @Produce      json
// @Param        request body []model.BatchRequestItem true "Список URL"
// @Success      201 {array} model.BatchResponseItem
// @Failure      400 {object} model.ErrorResponse
// @Failure      500 {object} model.ErrorResponse
// @Router       /api/shorten/batch [post]
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
	if err := h.Store.SaveBatch(r.Context(), records, userID); err != nil {
		logger.Log.Error("Ошибка пакетного сохранения", zap.Error(err))
		writeJSONError(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	resp := make([]model.BatchResponseItem, 0, len(corrList))
	for _, c := range corrList {
		shortURL := joinShortURL(h.BaseURL, c.shortID)
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

// UserURLs godoc
// @Summary      Получить ссылки пользователя
// @Tags         user
// @Security     CookieAuth
// @Produce      json
// @Success      200 {array} model.UserURL
// @Success      204 "Нет ссылок"
// @Failure      401 {object} model.ErrorResponse
// @Failure      500 {object} model.ErrorResponse
// @Router       /api/user/urls [get]
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

	urls, err := h.Store.GetUserURLs(r.Context(), userID)
	if err != nil {
		logger.Log.Error("Ошибка получения URL пользователя", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	for i := range urls {
		urls[i].ShortURL = joinShortURL(h.BaseURL, urls[i].ShortURL)
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

// DeleteUserURLs godoc
// @Summary      Удалить ссылки пользователя
// @Tags         user
// @Security     CookieAuth
// @Accept       json
// @Produce      json
// @Param        request body []string true "Список коротких идентификаторов"
// @Success      202 "Принято"
// @Failure      400 {object} model.ErrorResponse
// @Failure      401 {object} model.ErrorResponse
// @Router       /api/user/urls [delete]
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

	// Отправляем каждый идентификатор в канал фонового воркера (неблокирующе)
	for _, id := range shortURLs {
		req := deleteRequest{userID: userID, shortID: id}
		select {
		case h.deleteCh <- req:
			// Успешно отправлено
		default:
			// Канал переполнен – логируем и отбрасываем
			logger.Log.Warn("Канал удаления переполнен, запрос отброшен",
				zap.String("userID", userID),
				zap.String("shortID", id),
			)
		}
	}

	// Немедленно отвечаем 202 Accepted
	w.WriteHeader(http.StatusAccepted)
}

// deleteWorker — фоновый воркер, накапливающий идентификаторы и отправляющий их батчами в хранилище.
func (h *ShortenerHandler) deleteWorker() {
	defer h.deleteWg.Done()

	const (
		flushInterval = time.Second // интервал сброса
		maxBufferSize = 100         // максимальный суммарный размер буфера
	)

	buffer := make(map[string][]string) // userID -> slice of shortIDs
	totalItems := 0
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	// Внутренняя функция сброса буфера в хранилище
	flush := func() {
		if totalItems == 0 {
			return
		}
		// Используем фоновый контекст, так как здесь нет HTTP-запроса
		ctx := context.Background()
		for userID, ids := range buffer {
			if err := h.Store.BatchDelete(ctx, ids, userID); err != nil {
				logger.Log.Error("Ошибка пакетного удаления URL",
					zap.Error(err),
					zap.String("userID", userID),
				)
			}
		}
		// Очистка буфера
		buffer = make(map[string][]string)
		totalItems = 0
	}

	for {
		select {
		case req, ok := <-h.deleteCh:
			if !ok {
				// Канал закрыт – финальный сброс и выход
				flush()
				return
			}
			buffer[req.userID] = append(buffer[req.userID], req.shortID)
			totalItems++
			if totalItems >= maxBufferSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
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
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return false
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// joinShortURL склеивает baseURL и id без reflect-магии fmt.Sprintf.
func joinShortURL(baseURL, id string) string {
	var sb strings.Builder
	sb.Grow(len(baseURL) + 1 + len(id))
	sb.WriteString(baseURL)
	sb.WriteByte('/')
	sb.WriteString(id)
	return sb.String()
}
