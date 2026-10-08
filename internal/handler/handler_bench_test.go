package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/MartsinovichDanya/pgc_shortener/internal/auth"
	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

const (
	benchSecret  = "bench-secret"
	benchBaseURL = "http://localhost:8080"
)

// newBenchServer создаёт обработчик с in-memory FileStore (без файла на диске)
// и httptest-сервер с тем же роутером/middleware, что и боевой сервер.
func newBenchServer(tb testing.TB) *httptest.Server {
	tb.Helper()

	store, err := storage.NewFileStore() // только память, без записи на диск
	if err != nil {
		tb.Fatalf("NewFileStore: %v", err)
	}

	h := NewShortenerHandler(store, benchBaseURL, 1<<20, 16, false, nil)

	r := chi.NewRouter()
	r.Use(auth.AuthMiddleware(benchSecret))
	r.Post("/", h.CreateShortLink)
	r.Post("/api/shorten", h.ShortenAPI)
	r.Post("/api/shorten/batch", h.ShortenBatch)
	r.Get("/api/user/urls", h.UserURLs)
	r.Delete("/api/user/urls", h.DeleteUserURLs)
	r.Get("/{id}", h.Redirect)

	srv := httptest.NewServer(r)
	tb.Cleanup(func() {
		srv.Close()
		h.Shutdown()
	})
	return srv
}

// doRequest выполняет запрос через клиент httptest-сервера.
// Возвращает код ответа, тело и куки ответа (user_token).
func doRequest(t testing.TB, srv *httptest.Server, method, path string, body []byte, cookies ...*http.Cookie) (int, []byte, []*http.Cookie) {
	t.Helper()

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, data, resp.Cookies()
}

// seedURLs заранее наполняет хранилище n уникальными ссылками через POST /api/shorten.
// Возвращает список коротких ID и куку пользователя. Выполняется ДО ResetTimer.
func seedURLs(b *testing.B, srv *httptest.Server, n int) ([]string, []*http.Cookie) {
	b.Helper()

	ids := make([]string, 0, n)
	var userCookie []*http.Cookie

	for i := 0; i < n; i++ {
		payload, err := json.Marshal(model.Request{
			URL: "https://seed.example.com/" + strconv.Itoa(i),
		})
		if err != nil {
			b.Fatalf("marshal seed: %v", err)
		}

		status, body, cookies := doRequest(b, srv, http.MethodPost, "/api/shorten", payload, userCookie...)
		if status != http.StatusCreated && status != http.StatusConflict {
			b.Fatalf("seed: status = %d body=%s", status, string(body))
		}
		if len(userCookie) == 0 {
			userCookie = cookies
		}

		var resp model.Response
		if err := json.Unmarshal(body, &resp); err != nil {
			b.Fatalf("seed unmarshal: %v body=%s", err, string(body))
		}
		ids = append(ids, strings.TrimPrefix(resp.Result, benchBaseURL+"/"))
	}
	return ids, userCookie
}

// BenchmarkCreateShortLink измеряет POST / (текстовый протокол).
// URL уникален на каждую итерацию, чтобы не получать 409 (дубликат отклоняется
// хранилищем как ошибка, а не как измеряемая работа по созданию ссылки).
func BenchmarkCreateShortLink(b *testing.B) {
	srv := newBenchServer(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := []byte("https://bench.example.com/" + strconv.Itoa(i) + "/text")
		status, respBody, _ := doRequest(b, srv, http.MethodPost, "/", body)
		if status != http.StatusCreated {
			b.Fatalf("status = %d body=%s", status, string(respBody))
		}
	}
}

// BenchmarkShortenAPI измеряет POST /api/shorten (JSON-протокол).
// URL уникален на каждую итерацию (см. комментарий выше про 409).
func BenchmarkShortenAPI(b *testing.B) {
	srv := newBenchServer(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload, err := json.Marshal(model.Request{
			URL: "https://bench.example.com/" + strconv.Itoa(i) + "/api",
		})
		if err != nil {
			b.Fatalf("marshal: %v", err)
		}
		status, body, _ := doRequest(b, srv, http.MethodPost, "/api/shorten", payload)
		if status != http.StatusCreated {
			b.Fatalf("status = %d body=%s", status, string(body))
		}
	}
}

// BenchmarkRedirect измеряет GET /{id} – горячий read-path.
func BenchmarkRedirect(b *testing.B) {
	srv := newBenchServer(b)

	// Готовим один валидный короткий ID вне цикла измерений.
	payload, err := json.Marshal(model.Request{URL: "https://example.com/redirect-target"})
	if err != nil {
		b.Fatalf("marshal: %v", err)
	}
	status, body, _ := doRequest(b, srv, http.MethodPost, "/api/shorten", payload)
	if status != http.StatusCreated {
		b.Fatalf("prepare: status = %d body=%s", status, string(body))
	}
	var resp model.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		b.Fatalf("unmarshal: %v", err)
	}
	shortID := strings.TrimPrefix(resp.Result, benchBaseURL+"/")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status, body, _ := doRequest(b, srv, http.MethodGet, "/"+shortID, nil)
		// Клиент может проследовать 307 на внешний хост – тогда статус его;
		// принимаем и 307 напрямую.
		if status != http.StatusTemporaryRedirect && status >= 400 && status != http.StatusNotFound {
			b.Fatalf("redirect: status = %d body=%s", status, string(body))
		}
	}
}

// BenchmarkShortenBatch измеряет POST /api/shorten/batch – батч из 10 URL.
// Формат тела строго соответствует handler.ShortenBatch:
// массив объектов [{"correlation_id":"...","original_url":"..."}].
func BenchmarkShortenBatch(b *testing.B) {
	srv := newBenchServer(b)

	const batchSize = 10

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Каждой итерации нужны УНИКАЛЬНЫЕ original_url:
		// дубликат => ErrURLExists => 500, а пустой correlation_id/битый JSON => 400.
		items := make([]model.BatchRequestItem, batchSize)
		for j := 0; j < batchSize; j++ {
			items[j] = model.BatchRequestItem{
				CorrelationID: "corr-" + strconv.Itoa(i) + "-" + strconv.Itoa(j),
				OriginalURL:   "https://batch.example.com/" + strconv.Itoa(i) + "/" + strconv.Itoa(j),
			}
		}
		payload, err := json.Marshal(items)
		if err != nil {
			b.Fatalf("marshal batch: %v", err)
		}

		status, body, _ := doRequest(b, srv, http.MethodPost, "/api/shorten/batch", payload)
		if status != http.StatusCreated {
			b.Fatalf("status = %d body=%s", status, string(body))
		}
	}
}

// BenchmarkUserURLs измеряет GET /api/user/urls на пользователе со 100 ссылками.
func BenchmarkUserURLs(b *testing.B) {
	srv := newBenchServer(b)

	// Наполнение выполняется ДО ResetTimer – оно не входит в замер.
	_, userCookie := seedURLs(b, srv, 100)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status, body, _ := doRequest(b, srv, http.MethodGet, "/api/user/urls", nil, userCookie...)
		if status != http.StatusOK {
			b.Fatalf("status = %d body=%s", status, string(body))
		}
	}
}

// BenchmarkDeleteUserURLs измеряет синхронную часть DELETE /api/user/urls
// (постановка идентификаторов в очередь фонового воркера, ответ 202 Accepted).
// Формат тела строго соответствует handler.DeleteUserURLs:
// массив СТРОК ["<shortID>", ...]; пустой массив или битый JSON => 400.
func BenchmarkDeleteUserURLs(b *testing.B) {
	srv := newBenchServer(b)

	ids, userCookie := seedURLs(b, srv, 1000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chunk := ids[i%len(ids):]
		if len(chunk) > 20 {
			chunk = chunk[:20]
		}
		payload, err := json.Marshal(chunk)
		if err != nil {
			b.Fatalf("marshal delete: %v", err)
		}

		status, body, _ := doRequest(b, srv, http.MethodDelete, "/api/user/urls", payload, userCookie...)
		if status != http.StatusAccepted {
			b.Fatalf("status = %d body=%s", status, string(body))
		}
	}
}

// BenchmarkGenerateID меряет чистую генерацию ID (crypto/rand + big.Int) –
// типичную «тяжёлую» точку, видимую в pprof.
func BenchmarkGenerateID(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := utils.GenerateID(16); err != nil {
			b.Fatal(err)
		}
	}
}

// TestBatchPayloadShape – smoke-тест формата batch-запроса под текущий handler.
func TestBatchPayloadShape(t *testing.T) {
	srv := newBenchServer(t)

	items := []model.BatchRequestItem{{CorrelationID: "c1", OriginalURL: "https://example.com/1"}}
	payload, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	status, body, _ := doRequest(t, srv, http.MethodPost, "/api/shorten/batch", payload)
	if status != http.StatusCreated {
		t.Fatalf("status = %d want 201 body=%s", status, string(body))
	}

	var out []model.BatchResponseItem
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("bad response: %v", err)
	}
	if len(out) != 1 || out[0].CorrelationID != "c1" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

// TestDeletePayloadShape – smoke-тест формата DELETE /api/user/urls под текущий handler.
func TestDeletePayloadShape(t *testing.T) {
	srv := newBenchServer(t)

	p, err := json.Marshal(model.Request{URL: "https://example.com/del"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	status, body, cookies := doRequest(t, srv, http.MethodPost, "/api/shorten", p)
	if status != http.StatusCreated {
		t.Fatalf("status = %d want 201 body=%s", status, string(body))
	}

	var resp model.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	shortID := strings.TrimPrefix(resp.Result, benchBaseURL+"/")

	del, err := json.Marshal([]string{shortID})
	if err != nil {
		t.Fatalf("marshal delete: %v", err)
	}
	status, body, _ = doRequest(t, srv, http.MethodDelete, "/api/user/urls", del, cookies...)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d want 202 body=%s", status, string(body))
	}
}
