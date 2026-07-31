package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MartsinovichDanya/pgc_shortener/internal/handler"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
)

const (
	testBaseURL     = "http://localhost:8080"
	testMaxBodySize = 2048
	testIDLength    = 8
	testUseDB       = false
)

// testRequest выполняет HTTP-запрос к тестовому серверу и возвращает ответ и тело.
func testRequest(t *testing.T, ts *httptest.Server, method, path string, body io.Reader) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, body)
	require.NoError(t, err, "ошибка создания запроса")

	client := &http.Client{
		Transport: ts.Client().Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // не следовать редиректам
		},
	}

	resp, err := client.Do(req)
	require.NoError(t, err, "ошибка выполнения запроса")
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "ошибка чтения тела ответа")

	return resp, string(respBody)
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

// newTestRouter создаёт chi.Router с тестовыми параметрами.
func newTestRouter(store storage.Store) http.Handler {
	handler := handler.NewShortenerHandler(store, testBaseURL, testMaxBodySize, testIDLength, testUseDB)

	r := chi.NewRouter()
	r.Post("/", handler.CreateShortLink)
	r.Get("/{id}", handler.Redirect)

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
	err := store.Save(id, originalURL)
	require.NoError(t, err, "ошибка сохранения тестовых данных")

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
