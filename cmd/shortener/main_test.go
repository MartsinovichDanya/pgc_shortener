package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MartsinovichDanya/pgc_shortener/internal/auth"
	"github.com/MartsinovichDanya/pgc_shortener/internal/handler"
	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
)

const (
	testBaseURL      = "http://localhost:8080"
	testMaxBodySize  = 2048
	testIDLength     = 8
	testUseDB        = false
	testCookieSecret = "test-secret-key-32-bytes-long!!"
)

// testRequest выполняет HTTP-запрос к тестовому серверу и возвращает ответ и тело.
func testRequest(t *testing.T, ts *httptest.Server, method, path string, body io.Reader) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, body)
	require.NoError(t, err, "ошибка создания запроса")

	client := &http.Client{
		Transport: ts.Client().Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	require.NoError(t, err, "ошибка выполнения запроса")
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "ошибка чтения тела ответа")

	return resp, string(respBody)
}

// testRequestWithCookie выполняет запрос с предустановленной кукой.
func testRequestWithCookie(t *testing.T, ts *httptest.Server, method, path string, body io.Reader, cookie *http.Cookie) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, body)
	require.NoError(t, err)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	client := &http.Client{
		Transport: ts.Client().Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(respBody)
}

// getCookieFromResponse извлекает куку user_token из ответа.
func getCookieFromResponse(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == "user_token" {
			return c
		}
	}
	return nil
}

// getIDFromResponse извлекает ID из тела ответа после создания короткой ссылки.
func getIDFromResponse(t *testing.T, body string) string {
	t.Helper()
	require.True(t, strings.HasPrefix(body, testBaseURL+"/"),
		"тело ответа должно начинаться с %s/, получено: %s", testBaseURL, body)
	id := strings.TrimPrefix(body, testBaseURL+"/")
	require.Len(t, id, testIDLength, "ожидалась длина ID %d, получена %d", testIDLength, len(id))
	return id
}

// newTestRouter создаёт chi.Router с middleware аутентификации.
func newTestRouter(store storage.Store) http.Handler {
	h := handler.NewShortenerHandler(store, testBaseURL, testMaxBodySize, testIDLength, testUseDB)

	r := chi.NewRouter()
	r.Use(auth.AuthMiddleware(testCookieSecret))

	r.Post("/", h.CreateShortLink)
	r.Get("/{id}", h.Redirect)
	r.Get("/api/user/urls", h.UserURLs)
	r.Post("/api/shorten", h.ShortenAPI)
	r.Post("/api/shorten/batch", h.ShortenBatch)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})

	return r
}

// ---------- CreateShortLink ----------

func TestCreateShortLink_ValidURL(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	originalURL := "https://example.com/path?q=1"
	resp, body := testRequest(t, ts, http.MethodPost, "/", strings.NewReader(originalURL))

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"))

	id := getIDFromResponse(t, body)
	saved, err := store.Get(id)
	require.NoError(t, err, "ID %s не найден в хранилище", id)
	assert.Equal(t, originalURL, saved)

	// Кука должна быть установлена
	cookie := getCookieFromResponse(t, resp)
	require.NotNil(t, cookie, "кука user_token должна быть установлена")
	assert.NotEmpty(t, cookie.Value)
}

func TestCreateShortLink_EmptyBody(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, body := testRequest(t, ts, http.MethodPost, "/", strings.NewReader(""))

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "URL не может быть пустым\n", body)
}

func TestCreateShortLink_InvalidURL(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, body := testRequest(t, ts, http.MethodPost, "/", strings.NewReader("not-a-valid-url"))

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "Некорректный URL\n", body)
}

func TestCreateShortLink_BodyTruncation(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	longPath := strings.Repeat("a", 3000)
	longURL := "http://example.com/" + longPath
	resp, body := testRequest(t, ts, http.MethodPost, "/", strings.NewReader(longURL))

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	id := getIDFromResponse(t, body)
	saved, err := store.Get(id)
	require.NoError(t, err, "ID не найден в хранилище после усечения")

	assert.Len(t, saved, testMaxBodySize, "длина сохранённого URL должна быть равна testMaxBodySize")
	assert.True(t, strings.HasPrefix(longURL, saved), "сохранённый URL не является префиксом исходного")
}

// ---------- Redirect ----------

func TestRedirect_ExistingID(t *testing.T) {
	store, _ := storage.NewFileStore()
	id := "test1234"
	originalURL := "https://example.com/redirect-target"
	err := store.Save(id, originalURL, "test-user-id")
	require.NoError(t, err)

	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, _ := testRequest(t, ts, http.MethodGet, "/"+id, nil)

	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	assert.Equal(t, originalURL, resp.Header.Get("Location"))
}

func TestRedirect_NonExistentID(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, body := testRequest(t, ts, http.MethodGet, "/nonexistent", nil)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "Некорректный запрос\n", body)
}

// ---------- ShortenAPI ----------

func TestShortenAPI_ValidURL(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	reqBody := `{"url":"https://example.com/api-test"}`
	resp, body := testRequest(t, ts, http.MethodPost, "/api/shorten", strings.NewReader(reqBody))

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var respJSON model.Response
	err := json.Unmarshal([]byte(body), &respJSON)
	require.NoError(t, err)

	shortURL := respJSON.Result
	assert.True(t, strings.HasPrefix(shortURL, testBaseURL+"/"), "short_url должен начинаться с base URL")

	id := strings.TrimPrefix(shortURL, testBaseURL+"/")
	assert.Len(t, id, testIDLength)

	saved, err := store.Get(id)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/api-test", saved)
}

func TestShortenAPI_EmptyURL(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, body := testRequest(t, ts, http.MethodPost, "/api/shorten", strings.NewReader(`{"url":""}`))

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, body, "URL не может быть пустым")
}

func TestShortenAPI_InvalidJSON(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, body := testRequest(t, ts, http.MethodPost, "/api/shorten", strings.NewReader(`not json`))

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, body, "Некорректный JSON")
}

// ---------- ShortenBatch ----------

func TestShortenBatch_ValidBatch(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	reqBody := `[
		{"correlation_id":"1","original_url":"https://example.com/1"},
		{"correlation_id":"2","original_url":"https://example.com/2"}
	]`
	resp, body := testRequest(t, ts, http.MethodPost, "/api/shorten/batch", strings.NewReader(reqBody))

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var batchResp []model.BatchResponseItem
	err := json.Unmarshal([]byte(body), &batchResp)
	require.NoError(t, err)
	assert.Len(t, batchResp, 2)

	for _, item := range batchResp {
		assert.NotEmpty(t, item.CorrelationID)
		assert.True(t, strings.HasPrefix(item.ShortURL, testBaseURL+"/"))

		id := strings.TrimPrefix(item.ShortURL, testBaseURL+"/")
		assert.Len(t, id, testIDLength)

		_, err := store.Get(id)
		assert.NoError(t, err, "ID %s должен существовать в хранилище", id)
	}
}

func TestShortenBatch_EmptyBody(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, _ := testRequest(t, ts, http.MethodPost, "/api/shorten/batch", strings.NewReader(""))

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestShortenBatch_InvalidJSON(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, body := testRequest(t, ts, http.MethodPost, "/api/shorten/batch", strings.NewReader(`invalid`))

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, body, "Некорректный JSON")
}

func TestShortenBatch_EmptyCorrelationID(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	reqBody := `[{"correlation_id":"","original_url":"https://example.com/1"}]`
	resp, body := testRequest(t, ts, http.MethodPost, "/api/shorten/batch", strings.NewReader(reqBody))

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, body, "correlation_id не может быть пустым")
}

// ---------- UserURLs ----------

func TestUserURLs_NoURLs(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	// Первый запрос без куки – middleware создаст новую, вернётся 204
	resp, body := testRequest(t, ts, http.MethodGet, "/api/user/urls", nil)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Empty(t, body)
}

func TestUserURLs_WithURLs(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	// Сначала создадим две короткие ссылки, чтобы получить куку и userID
	resp1, _ := testRequest(t, ts, http.MethodPost, "/", strings.NewReader("https://example.com/urls1"))
	require.Equal(t, http.StatusCreated, resp1.StatusCode)
	cookie := getCookieFromResponse(t, resp1)
	require.NotNil(t, cookie)

	// Второй запрос с той же кукой
	resp2, _ := testRequestWithCookie(t, ts, http.MethodPost, "/", strings.NewReader("https://example.com/urls2"), cookie)
	require.Equal(t, http.StatusCreated, resp2.StatusCode)

	// Теперь запрос к /api/user/urls с кукой
	resp, body := testRequestWithCookie(t, ts, http.MethodGet, "/api/user/urls", nil, cookie)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var urls []model.UserURL
	err := json.Unmarshal([]byte(body), &urls)
	require.NoError(t, err)
	assert.Len(t, urls, 2)

	// Проверим, что возвращённые ссылки содержат наши оригинальные URL
	origURLs := make([]string, len(urls))
	for i, u := range urls {
		origURLs[i] = u.OriginalURL
		// ShortURL должен быть полным URL, начинающимся с testBaseURL
		assert.True(t, strings.HasPrefix(u.ShortURL, testBaseURL+"/"))
	}
	assert.Contains(t, origURLs, "https://example.com/urls1")
	assert.Contains(t, origURLs, "https://example.com/urls2")
}

func TestUserURLs_InvalidToken(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	// Создаём "невалидную" куку – просто набор символов, не соответствующий формату
	invalidCookie := &http.Cookie{
		Name:  "user_token",
		Value: "some-garbage-value",
	}

	resp, body := testRequestWithCookie(t, ts, http.MethodGet, "/api/user/urls", nil, invalidCookie)

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, body, "user token is invalid")
}

func TestUserURLs_InvalidTokenSignature(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	// Создаём куку с правильным userID, но с подписью от другого секрета
	userID := "test-user-id"
	// Вычислим корректную подпись для этого userID с "правильным" секретом, но подставим неверную
	wrongSignature := "0123456789abcdef"
	value := userID + ":" + wrongSignature
	invalidCookie := &http.Cookie{
		Name:  "user_token",
		Value: value,
	}

	resp, body := testRequestWithCookie(t, ts, http.MethodGet, "/api/user/urls", nil, invalidCookie)

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, body, "user token is invalid")
}

// ---------- Маршрутизация ----------

func TestServeHTTP_InvalidMethodOnRoot(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, _ := testRequest(t, ts, http.MethodPut, "/", nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServeHTTP_GetRootWithoutID(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, _ := testRequest(t, ts, http.MethodGet, "/", nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServeHTTP_PostToInvalidPath(t *testing.T) {
	store, _ := storage.NewFileStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, _ := testRequest(t, ts, http.MethodPost, "/somepath", nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
