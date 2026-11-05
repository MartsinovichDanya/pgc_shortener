package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mailru/easyjson"
	"go.uber.org/zap"

	"github.com/MartsinovichDanya/pgc_shortener/internal/auth"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
	"github.com/MartsinovichDanya/pgc_shortener/internal/service"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

// ShortenerHandler содержит зависимости HTTP-обработчиков.
type ShortenerHandler struct {
	Store           storage.Store
	svc             *service.Service
	deleteCh        chan deleteRequest
	BaseURL         string
	deleteWg        sync.WaitGroup
	MaxBodySize     int
	IDLength        int
	deleteCloseOnce sync.Once
	UseDB           bool
	TrustedSubnet   *net.IPNet
}

// deleteRequest — элемент очереди на удаление.
// generate:reset
type deleteRequest struct {
	userID  string
	shortID string
}

// NewShortenerHandler – конструктор обработчиков.
// Сервис (общая бизнес-логика с gRPC) передаётся готовым — хендлер его не
// создаёт и ничего не знает про audit/observer.
func NewShortenerHandler(
	store storage.Store,
	svc *service.Service,
	baseURL string,
	maxBodySize, idLength int,
	useDB bool,
	trustedSubnet string,
) *ShortenerHandler {
	var subnet *net.IPNet
	if ts := strings.TrimSpace(trustedSubnet); ts != "" {
		_, parsed, err := net.ParseCIDR(ts)
		if err != nil {
			logger.Log.Error("invalid trusted_subnet",
				zap.String("trusted_subnet", ts),
				zap.Error(err),
			)
		} else {
			subnet = parsed
		}
	}

	h := &ShortenerHandler{
		Store:         store,
		svc:           svc,
		BaseURL:       baseURL,
		MaxBodySize:   maxBodySize,
		IDLength:      idLength,
		UseDB:         useDB,
		deleteCh:      make(chan deleteRequest, 1000),
		TrustedSubnet: subnet,
	}

	h.deleteWg.Add(1)
	go h.deleteWorker()
	return h
}

// Shutdown корректно останавливает фоновый воркер удаления.
func (h *ShortenerHandler) Shutdown(ctx context.Context) {
	h.deleteCloseOnce.Do(func() {
		close(h.deleteCh)

		done := make(chan struct{})
		go func() {
			h.deleteWg.Wait()
			close(done)
		}()

		select {
		case <-done:
			logger.Log.Info("delete worker stopped")
		case <-ctx.Done():
			logger.Log.Error("delete worker shutdown timed out", zap.Error(ctx.Err()))
		}
	})
}

// CreateShortLink godoc
// @Summary      Создать короткую ссылку
// @Tags         links
// @Accept       text/plain
// @Produce      text/plain
// @Param        url body string true "Оригинальный URL"
// @Success      201 {string} string "Короткая ссылка"
// @Failure      409 {string} string "URL уже существует"
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

	userID, _ := r.Context().Value(auth.UserIDKey).(string)

	shortURL, exists, err := h.svc.Shorten(r.Context(), originalURL, userID)
	if err != nil {
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	if exists {
		w.WriteHeader(http.StatusConflict)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	fmt.Fprint(w, shortURL)
}

// ShortenAPI godoc
// @Summary      Создать короткую ссылку (JSON)
// @Tags         links
// @Accept       json
// @Produce      json
// @Param        request body model.Request true "Запрос"
// @Success      201 {object} model.Response
// @Failure      409 {object} model.Response "URL уже существует"
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

	userID, _ := r.Context().Value(auth.UserIDKey).(string)

	shortURL, exists, err := h.svc.Shorten(r.Context(), originalURL, userID)
	if err != nil {
		writeJSONError(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	resp := model.Response{Result: shortURL}
	jsonResp, err := easyjson.Marshal(resp)
	if err != nil {
		writeJSONError(w, "Ошибка формирования ответа", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if exists {
		w.WriteHeader(http.StatusConflict)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
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
	userID, _ := r.Context().Value(auth.UserIDKey).(string)

	originalURL, err := h.svc.Expand(r.Context(), id, userID)
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

// Stats godoc
// @Summary      Внутренняя статистика сервиса
// @Tags         internal
// @Produce      json
// @Param        X-Real-IP header string true "IP клиента из доверенной подсети"
// @Success      200 {object} model.StatsResponse
// @Failure      403 {object} model.ErrorResponse
// @Failure      500 {object} model.ErrorResponse
// @Router       /api/internal/stats [get]
func (h *ShortenerHandler) Stats(w http.ResponseWriter, r *http.Request) {
	if h.TrustedSubnet == nil {
		writeJSONError(w, "Forbidden", http.StatusForbidden)
		return
	}

	ipStr := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	ip := net.ParseIP(ipStr)
	if ip == nil || !h.TrustedSubnet.Contains(ip) {
		writeJSONError(w, "Forbidden", http.StatusForbidden)
		return
	}

	urls, err := h.Store.CountURLs(r.Context())
	if err != nil {
		logger.Log.Error("Ошибка получения количества URL", zap.Error(err))
		writeJSONError(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	users, err := h.Store.CountUsers(r.Context())
	if err != nil {
		logger.Log.Error("Ошибка получения количества пользователей", zap.Error(err))
		writeJSONError(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	resp := model.StatsResponse{URLs: urls, Users: users}
	jsonResp, err := easyjson.Marshal(resp)
	if err != nil {
		writeJSONError(w, "Ошибка формирования ответа", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jsonResp)
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

	userID, _ := r.Context().Value(auth.UserIDKey).(string)

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

	urls, err := h.svc.ListByUser(r.Context(), userID)
	if err != nil {
		logger.Log.Error("Ошибка получения URL пользователя", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
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
	if invalid, ok := r.Context().Value(auth.TokenInvalidKey).(bool); ok && invalid {
		writeJSONError(w, "user token is invalid", http.StatusUnauthorized)
		return
	}

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

	for _, id := range shortURLs {
		req := deleteRequest{userID: userID, shortID: id}
		select {
		case h.deleteCh <- req:
		default:
			logger.Log.Warn("Канал удаления переполнен, запрос отброшен",
				zap.String("userID", userID),
				zap.String("shortID", id),
			)
		}
	}

	w.WriteHeader(http.StatusAccepted)
}

// deleteWorker — фоновый воркер, накапливающий идентификаторы и отправляющий их батчами в хранилище.
func (h *ShortenerHandler) deleteWorker() {
	defer h.deleteWg.Done()

	const (
		flushInterval = time.Second
		maxBufferSize = 100
		flushTimeout  = 5 * time.Second
	)

	buffer := make(map[string][]string)
	totalItems := 0
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func() {
		if totalItems == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
		defer cancel()

		for userID, ids := range buffer {
			if err := h.Store.BatchDelete(ctx, ids, userID); err != nil {
				logger.Log.Error("Ошибка пакетного удаления URL",
					zap.Error(err),
					zap.String("userID", userID),
				)
			}
		}
		buffer = make(map[string][]string)
		totalItems = 0
	}

	for {
		select {
		case req, ok := <-h.deleteCh:
			if !ok {
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
