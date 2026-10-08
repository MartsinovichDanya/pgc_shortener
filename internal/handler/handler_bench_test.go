package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/MartsinovichDanya/pgc_shortener/internal/audit"
	"github.com/MartsinovichDanya/pgc_shortener/internal/auth"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

func TestMain(m *testing.M) {
	// инициализируем logger.Log заглушкой, чтобы Debug-логи не влияли на бенчи
	_ = logger.Initialize("panic")
	m.Run()
}

type benchEnv struct {
	handler *ShortenerHandler
	store   storage.Store
}

func newBenchEnv(b testing.TB) *benchEnv {
	b.Helper()
	// in-memory: filename пустой — без записи на диск
	store, err := storage.NewFileStore("")
	if err != nil {
		b.Fatalf("NewFileStore: %v", err)
	}
	h := NewShortenerHandler(store, "http://localhost:8080", 2048, 8, false, audit.NoopNotifier{})
	b.Cleanup(h.Shutdown)
	return &benchEnv{handler: h, store: store}
}

func withUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, auth.UserIDKey, userID)
}

// POST /api/shorten
func BenchmarkShortenAPI(b *testing.B) {
	env := newBenchEnv(b)
	body, _ := json.Marshal(model.Request{URL: "https://example.com/some/very/long/path"})
	for i := range b.N {
		req := httptest.NewRequest(http.MethodPost, "/api/shorten", bytes.NewReader(body))
		req = req.WithContext(withUser(req.Context(), fmt.Sprintf("user-%d", i%100)))
		rec := httptest.NewRecorder()
		env.handler.ShortenAPI(rec, req)
		if rec.Code != http.StatusCreated {
			b.Fatalf("status = %d", rec.Code)
		}
	}
	b.ReportAllocs()
}

// GET /{id} – редирект (самый горячий путь)
func newBenchRouter(env *benchEnv) http.Handler {
	r := chi.NewRouter()
	r.Get("/{id}", env.handler.Redirect)
	return r
}

func BenchmarkRedirect(b *testing.B) {
	env := newBenchEnv(b)
	const n = 10000
	ids := make([]string, n)
	for i := range ids {
		id, _ := utils.GenerateID(8)
		if err := env.store.Save(context.Background(), id, fmt.Sprintf("https://example.com/%d", i), "bench-user"); err != nil {
			b.Fatal(err)
		}
		ids[i] = id
	}
	router := newBenchRouter(env)
	for i := range b.N {
		req := httptest.NewRequest(http.MethodGet, "/"+ids[i%n], nil)
		req = req.WithContext(withUser(req.Context(), "bench-user"))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req) // chi сам положит URLParam
		if rec.Code != http.StatusTemporaryRedirect {
			b.Fatalf("status = %d", rec.Code)
		}
	}
	b.ReportAllocs()
}

// POST /api/shorten/batch
func BenchmarkShortenBatch(b *testing.B) {
	env := newBenchEnv(b)
	const batchSize = 100
	for i := range b.N {
		items := make([]model.BatchRequestItem, batchSize)
		for j := range items {
			items[j] = model.BatchRequestItem{
				CorrelationID: fmt.Sprintf("corr-%d-%d", i, j),
				OriginalURL:   fmt.Sprintf("https://example.com/batch/%d/%d", i, j),
			}
		}
		body, _ := json.Marshal(items)
		req := httptest.NewRequest(http.MethodPost, "/api/shorten/batch", bytes.NewReader(body))
		req = req.WithContext(withUser(req.Context(), "bench-user"))
		rec := httptest.NewRecorder()
		env.handler.ShortenBatch(rec, req)
		if rec.Code != http.StatusCreated {
			b.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
	}
	b.ReportAllocs()
}

// GET /api/user/urls
func BenchmarkUserURLs(b *testing.B) {
	env := newBenchEnv(b)
	const perUser = 500
	for j := 0; j < perUser; j++ {
		id, _ := utils.GenerateID(8)
		if err := env.store.Save(context.Background(), id, fmt.Sprintf("https://example.com/u/%d", j), "bench-user"); err != nil {
			b.Fatal(err)
		}
	}
	for range b.N {
		req := httptest.NewRequest(http.MethodGet, "/api/user/urls", nil)
		req = req.WithContext(withUser(req.Context(), "bench-user"))
		rec := httptest.NewRecorder()
		env.handler.UserURLs(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d", rec.Code)
		}
	}
	b.ReportAllocs()
}

// DELETE /api/user/urls
func BenchmarkDeleteUserURLs(b *testing.B) {
	env := newBenchEnv(b)
	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = strconv.Itoa(i)
		_ = env.store.Save(context.Background(), ids[i], fmt.Sprintf("https://del.example.com/%d", i), "bench-user")
	}
	body, _ := json.Marshal(ids)
	b.ResetTimer()
	for range b.N {
		req := httptest.NewRequest(http.MethodDelete, "/api/user/urls", bytes.NewReader(body))
		req = req.WithContext(withUser(req.Context(), "bench-user"))
		rec := httptest.NewRecorder()
		env.handler.DeleteUserURLs(rec, req)
		if rec.Code != http.StatusAccepted {
			b.Fatalf("status = %d", rec.Code)
		}
		// дренаж канала, чтобы не разрастался буфер
		for {
			select {
			case <-env.handler.deleteCh:
			default:
				goto drained
			}
		}
	drained:
		time.Sleep(time.Millisecond)
	}
	b.ReportAllocs()
}
