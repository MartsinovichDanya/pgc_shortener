package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/MartsinovichDanya/pgc_shortener/internal/auth"
	"github.com/MartsinovichDanya/pgc_shortener/internal/handler"
	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// mockStore — минимальное in-memory хранилище для примеров.
//
// Реализует интерфейс storage.Store в объёме, который используют примеры.
// Если у вас в интерфейсе есть дополнительные методы — допишите их сюда,
// иначе компилятор не даст собрать пакет.
// ---------------------------------------------------------------------------
type mockStore struct {
	mu      sync.Mutex
	byID    map[string]string
	byURL   map[string]string
	users   map[string][]string // userID -> список shortID
	deleted map[string]bool
}

func newMockStore() *mockStore {
	return &mockStore{
		byID:    make(map[string]string),
		byURL:   make(map[string]string),
		users:   make(map[string][]string),
		deleted: make(map[string]bool),
	}
}

func (m *mockStore) Save(_ context.Context, id, url, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byURL[url]; ok {
		return storage.ErrURLExists
	}
	m.byID[id] = url
	m.byURL[url] = id
	if userID != "" {
		m.users[userID] = append(m.users[userID], id)
	}
	return nil
}

func (m *mockStore) Get(_ context.Context, id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleted[id] {
		return "", storage.ErrURLDeleted
	}
	url, ok := m.byID[id]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return url, nil
}

func (m *mockStore) GetByOriginalURL(_ context.Context, url string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byURL[url]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return id, nil
}

func (m *mockStore) SaveBatch(_ context.Context, records map[string]string, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, url := range records {
		m.byID[id] = url
		m.byURL[url] = id
		if userID != "" {
			m.users[userID] = append(m.users[userID], id)
		}
	}
	return nil
}

func (m *mockStore) GetUserURLs(_ context.Context, userID string) ([]model.UserURL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.UserURL, 0, len(m.users[userID]))
	for _, id := range m.users[userID] {
		if m.deleted[id] {
			continue
		}
		out = append(out, model.UserURL{
			ShortURL:    id,
			OriginalURL: m.byID[id],
		})
	}
	return out, nil
}

func (m *mockStore) BatchDelete(_ context.Context, ids []string, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		m.deleted[id] = true
	}
	return nil
}

// Ping не нужен при UseDB=false, но метод полезно иметь на будущее.
func (m *mockStore) Ping(_ context.Context) error { return nil }

// ---------------------------------------------------------------------------
// Вспомогательный конструктор обработчика для примеров.
// ---------------------------------------------------------------------------
func newTestHandler(store storage.Store) *handler.ShortenerHandler {
	return handler.NewShortenerHandler(
		store,
		"http://short.local", // BaseURL
		4096,                 // MaxBodySize
		8,                    // IDLength
		false,                // UseDB
		nil,                  // auditObserver (Noop внутри)
	)
}

// ---------------------------------------------------------------------------
// Пример: POST / — создание короткой ссылки (plain text).
// ---------------------------------------------------------------------------
func ExampleShortenerHandler_CreateShortLink() {
	store := newMockStore()
	h := newTestHandler(store)
	defer h.Shutdown()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("https://example.com"))
	rec := httptest.NewRecorder()

	h.CreateShortLink(rec, req)

	fmt.Println("Status:", rec.Code)
	fmt.Println("Content-Type:", rec.Header().Get("Content-Type"))
	fmt.Println("Body has prefix:", strings.HasPrefix(rec.Body.String(), "http://short.local/"))
	// Output:
	// Status: 201
	// Content-Type: text/plain
	// Body has prefix: true
}

// ---------------------------------------------------------------------------
// Пример: POST /api/shorten — создание короткой ссылки (JSON).
// ---------------------------------------------------------------------------
func ExampleShortenerHandler_ShortenAPI() {
	store := newMockStore()
	h := newTestHandler(store)
	defer h.Shutdown()

	payload := `{"url":"https://example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ShortenAPI(rec, req)

	var resp model.Response
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	fmt.Println("Status:", rec.Code)
	fmt.Println("Content-Type:", rec.Header().Get("Content-Type"))
	fmt.Println("Has result:", strings.HasPrefix(resp.Result, "http://short.local/"))
	// Output:
	// Status: 201
	// Content-Type: application/json
	// Has result: true
}

// ---------------------------------------------------------------------------
// Пример: GET /{id} — редирект на оригинальный URL.
// ---------------------------------------------------------------------------
func ExampleShortenerHandler_Redirect() {
	store := newMockStore()
	// Предварительно «сохраняем» ссылку.
	_ = store.Save(context.Background(), "abc12345", "https://example.com", "")
	h := newTestHandler(store)
	defer h.Shutdown()

	req := httptest.NewRequest(http.MethodGet, "/abc12345", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc12345")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.Redirect(rec, req)

	fmt.Println("Status:", rec.Code)
	fmt.Println("Location:", rec.Header().Get("Location"))
	// Output:
	// Status: 307
	// Location: https://example.com
}

// ---------------------------------------------------------------------------
// Пример: GET /ping — проверка соединения с БД (UseDB=false).
// ---------------------------------------------------------------------------
func ExampleShortenerHandler_PingHandler() {
	store := newMockStore()
	h := newTestHandler(store)
	defer h.Shutdown()

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()

	h.PingHandler(rec, req)

	fmt.Println("Status:", rec.Code)
	// Output:
	// Status: 200
}

// ---------------------------------------------------------------------------
// Пример: POST /api/shorten/batch — массовое создание коротких ссылок.
// ---------------------------------------------------------------------------
func ExampleShortenerHandler_ShortenBatch() {
	store := newMockStore()
	h := newTestHandler(store)
	defer h.Shutdown()

	payload := `[
		{"correlation_id":"1","original_url":"https://example.com"},
		{"correlation_id":"2","original_url":"https://go.dev"}
	]`
	req := httptest.NewRequest(http.MethodPost, "/api/shorten/batch", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ShortenBatch(rec, req)

	var resp []model.BatchResponseItem
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	fmt.Println("Status:", rec.Code)
	for _, it := range resp {
		fmt.Printf("%s %v\n", it.CorrelationID, strings.HasPrefix(it.ShortURL, "http://short.local/"))
	}
	// Output:
	// Status: 201
	// 1 true
	// 2 true
}

// ---------------------------------------------------------------------------
// Пример: GET /api/user/urls — список ссылок пользователя.
// ---------------------------------------------------------------------------
func ExampleShortenerHandler_UserURLs() {
	store := newMockStore()
	_ = store.Save(context.Background(), "abc12345", "https://example.com", "user-1")
	h := newTestHandler(store)
	defer h.Shutdown()

	req := httptest.NewRequest(http.MethodGet, "/api/user/urls", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, "user-1"))
	rec := httptest.NewRecorder()

	h.UserURLs(rec, req)

	var urls []model.UserURL
	_ = json.Unmarshal(rec.Body.Bytes(), &urls)

	fmt.Println("Status:", rec.Code)
	for _, u := range urls {
		fmt.Println(u.ShortURL, u.OriginalURL)
	}
	// Output:
	// Status: 200
	// http://short.local/abc12345 https://example.com
}

// ---------------------------------------------------------------------------
// Пример: DELETE /api/user/urls — асинхронное удаление ссылок.
// ---------------------------------------------------------------------------
func ExampleShortenerHandler_DeleteUserURLs() {
	store := newMockStore()
	h := newTestHandler(store)
	defer h.Shutdown()

	body := `["abc12345","def67890"]`
	req := httptest.NewRequest(http.MethodDelete, "/api/user/urls", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, "user-1"))
	rec := httptest.NewRecorder()

	h.DeleteUserURLs(rec, req)

	fmt.Println("Status:", rec.Code)
	// Output:
	// Status: 202
}

// ---------------------------------------------------------------------------
// Пример: полный цикл через реальный роутер (httptest.Server).
//
// Показывает, как эндпоинты ведут себя вместе с маршрутизацией chi
// и заголовками, которые возвращает сервер.
// ---------------------------------------------------------------------------
func Example_fullFlow() {
	store := newMockStore()

	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	defer srv.Close()

	// Теперь знаем реальный BaseURL тестового сервера.
	h := handler.NewShortenerHandler(
		store,
		srv.URL, // <-- BaseURL = адрес httptest-сервера
		4096,
		8,
		false,
		nil,
	)
	defer h.Shutdown()

	r := chi.NewRouter()
	r.Post("/", h.CreateShortLink)
	r.Get("/{id}", h.Redirect)
	srv.Config.Handler = r

	// 1. Создаём короткую ссылку.
	resp, err := http.Post(srv.URL+"/", "text/plain", strings.NewReader("https://example.com"))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	shortURL := strings.TrimSpace(string(body))

	fmt.Println("create status:", resp.StatusCode)
	fmt.Println("short URL prefix:", strings.HasPrefix(shortURL, srv.URL+"/"))

	// 2. Переходим по ней.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp2, err := client.Get(shortURL)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	resp2.Body.Close()

	fmt.Println("redirect status:", resp2.StatusCode)
	fmt.Println("Location:", resp2.Header.Get("Location"))
	// Output:
	// create status: 201
	// short URL prefix: true
	// redirect status: 307
	// Location: https://example.com
}
