package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Тестовые значения, которые будут использоваться вместо глобальных констант
const (
	testBaseURL     = "http://localhost:8080"
	testMaxBodySize = 2048
	testIDLength    = 8
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

// getIDFromResponse извлекает ID из тела ответа после создания короткой ссылки.
func getIDFromResponse(t *testing.T, body string) string {
	t.Helper()
	require.True(t, strings.HasPrefix(body, testBaseURL+"/"), "тело ответа должно начинаться с %s/, получено: %s", testBaseURL, body)
	id := strings.TrimPrefix(body, testBaseURL+"/")
	require.Len(t, id, testIDLength, "ожидалась длина ID %d, получена %d", testIDLength, len(id))
	return id
}

// newTestRouter создаёт роутер с фиксированными тестовыми параметрами
func newTestRouter(store *Store) http.Handler {
	return newRouter(store, testBaseURL, testMaxBodySize, testIDLength)
}

// ---------- createShortLink ----------

func TestCreateShortLink_ValidURL(t *testing.T) {
	store := NewStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	originalURL := "https://example.com/path?q=1"
	resp, body := testRequest(t, ts, http.MethodPost, "/", strings.NewReader(originalURL))

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"))

	id := getIDFromResponse(t, body)
	saved, ok := store.Get(id)
	require.True(t, ok, "ID %s не найден в хранилище", id)
	assert.Equal(t, originalURL, saved)
}

func TestCreateShortLink_EmptyBody(t *testing.T) {
	store := NewStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, body := testRequest(t, ts, http.MethodPost, "/", strings.NewReader(""))

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "URL не может быть пустым\n", body)
}

func TestCreateShortLink_InvalidURL(t *testing.T) {
	store := NewStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, body := testRequest(t, ts, http.MethodPost, "/", strings.NewReader("not-a-valid-url"))

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "Некорректный URL\n", body)
}

func TestCreateShortLink_BodyTruncation(t *testing.T) {
	store := NewStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	longPath := strings.Repeat("a", 3000)
	longURL := "http://example.com/" + longPath
	resp, body := testRequest(t, ts, http.MethodPost, "/", strings.NewReader(longURL))

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	id := getIDFromResponse(t, body)
	saved, ok := store.Get(id)
	require.True(t, ok, "ID не найден в хранилище после усечения")

	assert.Len(t, saved, testMaxBodySize, "длина сохранённого URL должна быть равна testMaxBodySize")
	assert.True(t, strings.HasPrefix(longURL, saved), "сохранённый URL не является префиксом исходного")
}

// ---------- redirect ----------

func TestRedirect_ExistingID(t *testing.T) {
	store := NewStore()
	id := "test1234"
	originalURL := "https://example.com/redirect-target"
	store.Save(id, originalURL)

	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, _ := testRequest(t, ts, http.MethodGet, "/"+id, nil)

	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	assert.Equal(t, originalURL, resp.Header.Get("Location"))
}

func TestRedirect_NonExistentID(t *testing.T) {
	store := NewStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, body := testRequest(t, ts, http.MethodGet, "/nonexistent", nil)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "Некорректный запрос\n", body)
}

// ---------- Маршрутизация ----------

func TestServeHTTP_InvalidMethodOnRoot(t *testing.T) {
	store := NewStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, _ := testRequest(t, ts, http.MethodPut, "/", nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServeHTTP_GetRootWithoutID(t *testing.T) {
	store := NewStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, _ := testRequest(t, ts, http.MethodGet, "/", nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServeHTTP_PostToInvalidPath(t *testing.T) {
	store := NewStore()
	ts := httptest.NewServer(newTestRouter(store))
	defer ts.Close()

	resp, _ := testRequest(t, ts, http.MethodPost, "/somepath", nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
