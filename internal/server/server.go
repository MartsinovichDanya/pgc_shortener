package server

import (
	"net/http"

	"github.com/MartsinovichDanya/pgc_shortener/internal/config"
	"github.com/MartsinovichDanya/pgc_shortener/internal/handler"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// Run инициализирует все зависимости, настраивает роутер и запускает HTTP-сервер.
func Run() error {
	cfg := config.GetConfig()

	if err := logger.Initialize(cfg.LogLevel); err != nil {
		return err
	}
	logger.Log.Debug("Running config", zap.Any("config", cfg))

	store := storage.NewStore()
	handler := handler.NewShortenerHandler(store, cfg.BaseURL, cfg.MaxBodySize, cfg.IDLength)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	//r.Use(middleware.RealIP)
	r.Use(logger.GetLogger())
	r.Use(middleware.Recoverer)

	r.Post("/", handler.CreateShortLink)
	r.Post("/api/shorten", handler.ShortenAPI)
	r.Get("/{id}", handler.Redirect)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})

	if err := http.ListenAndServe(cfg.ServerAddr, r); err != nil {
		return err
	}

	return nil
}
