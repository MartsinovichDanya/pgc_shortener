package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Вспомогательная функция для проверки сохранённого URL после создания короткой ссылки
func getIDFromResponse(t *testing.T, body string) string {
	t.Helper()
	if !strings.HasPrefix(body, baseURL+"/") {
		t.Fatalf("тело ответа должно начинаться с %s/, получено: %s", baseURL, body)
	}
	id := strings.TrimPrefix(body, baseURL+"/")
	if len(id) != idLength {
		t.Fatalf("ожидалась длина ID %d, получена %d", idLength, len(id))
	}
	return id
}

// ---------- createShortLink ----------

func TestCreateShortLink_ValidURL(t *testing.T) {
	store := NewStore()
	handler := NewShortenerHandler(store)

	originalURL := "https://example.com/path?q=1"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(originalURL))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("ожидался статус %d, получен %d", http.StatusCreated, resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "text/plain" {
		t.Errorf("ожидался Content-Type text/plain, получен %s", ct)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ошибка чтения тела ответа: %v", err)
	}
	body := string(bodyBytes)

	id := getIDFromResponse(t, body)
	saved, ok := store.Get(id)
	if !ok {
		t.Fatalf("короткий ID %s не найден в хранилище", id)
	}
	if saved != originalURL {
		t.Errorf("сохранённый URL = %s, ожидался %s", saved, originalURL)
	}
}

func TestCreateShortLink_EmptyBody(t *testing.T) {
	store := NewStore()
	handler := NewShortenerHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("ожидался статус %d, получен %d", http.StatusBadRequest, resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := strings.TrimSpace(string(bodyBytes))
	if body != "URL не может быть пустым" {
		t.Errorf("сообщение об ошибке = %q, ожидалось \"URL не может быть пустым\"", body)
	}
}

func TestCreateShortLink_InvalidURL(t *testing.T) {
	store := NewStore()
	handler := NewShortenerHandler(store)

	invalidURL := "not-a-valid-url"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(invalidURL))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("ожидался статус %d, получен %d", http.StatusBadRequest, resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := strings.TrimSpace(string(bodyBytes))
	if body != "Некорректный URL" {
		t.Errorf("сообщение об ошибке = %q, ожидалось \"Некорректный URL\"", body)
	}
}

func TestCreateShortLink_BodyTruncation(t *testing.T) {
	store := NewStore()
	handler := NewShortenerHandler(store)

	longPath := strings.Repeat("a", 3000)
	longURL := "http://example.com/" + longPath
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(longURL))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("ожидался статус %d, получен %d", http.StatusCreated, resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	id := getIDFromResponse(t, string(bodyBytes))

	saved, ok := store.Get(id)
	if !ok {
		t.Fatal("ID не найден в хранилище после усечения")
	}

	if len(saved) != maxBodySize {
		t.Errorf("длина сохранённого URL = %d, ожидалось %d (усечение)", len(saved), maxBodySize)
	}

	if !strings.HasPrefix(longURL, saved) {
		t.Error("сохранённый URL не является префиксом исходного")
	}
}

// ---------- redirect ----------

func TestRedirect_ExistingID(t *testing.T) {
	store := NewStore()
	handler := NewShortenerHandler(store)

	id := "test1234"
	originalURL := "https://example.com/redirect-target"
	store.Save(id, originalURL)

	req := httptest.NewRequest(http.MethodGet, "/"+id, nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("ожидался статус %d, получен %d", http.StatusTemporaryRedirect, resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location != originalURL {
		t.Errorf("заголовок Location = %s, ожидался %s", location, originalURL)
	}
}

func TestRedirect_NonExistentID(t *testing.T) {
	store := NewStore()
	handler := NewShortenerHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("ожидался статус %d для несуществующего ID, получен %d", http.StatusBadRequest, resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := strings.TrimSpace(string(bodyBytes))
	if body != "Некорректный запрос" {
		t.Errorf("сообщение = %q, ожидалось \"Некорректный запрос\"", body)
	}
}

// ---------- ServeHTTP (маршрутизация) ----------

func TestServeHTTP_InvalidMethodOnRoot(t *testing.T) {
	store := NewStore()
	handler := NewShortenerHandler(store)

	req := httptest.NewRequest(http.MethodPut, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("ожидался 400, получен %d", resp.StatusCode)
	}
}

func TestServeHTTP_GetRootWithoutID(t *testing.T) {
	store := NewStore()
	handler := NewShortenerHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("ожидался 400, получен %d", resp.StatusCode)
	}
}

func TestServeHTTP_PostToInvalidPath(t *testing.T) {
	store := NewStore()
	handler := NewShortenerHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/somepath", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("ожидался 400, получен %d", resp.StatusCode)
	}
}
