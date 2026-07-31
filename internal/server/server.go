package server

import (
	"net/http"

	"github.com/MartsinovichDanya/pgc_shortener/internal/config"
	"github.com/MartsinovichDanya/pgc_shortener/internal/handler"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/middleware"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// Run инициализирует все зависимости, настраивает роутер и запускает HTTP-сервер.
func Run() error {
	cfg := config.GetConfig()

	if err := logger.Initialize(cfg.LogLevel); err != nil {
		return err
	}
	logger.Log.Debug("Running config", zap.Any("config", cfg))

	var store storage.Store
	var err error

	if cfg.UseDB {
		// Создаём хранилище в PostgreSQL (DSN берётся из конфига).
		store, err = storage.NewPostgresStore(cfg.DatabaseDSN)
	} else {
		// Файловое/локальное хранилище.
		store, err = storage.NewFileStore(cfg.FileStoragePath)
	}
	if err != nil {
		logger.Log.Fatal("Storage creation failed", zap.Error(err))
		return err
	}
	logger.Log.Debug("Storage created")

	ServiceHandler := handler.NewShortenerHandler(store, cfg.BaseURL, cfg.MaxBodySize, cfg.IDLength, cfg.UseDB)

	logger.Log.Debug("Handler created")

	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID)
	//r.Use(middleware.RealIP)
	r.Use(logger.GetLogger())
	r.Use(middleware.GzipMiddleware)
	r.Use(chiMiddleware.Recoverer)

	r.Post("/", ServiceHandler.CreateShortLink)
	r.Post("/api/shorten", ServiceHandler.ShortenAPI)
	r.Post("/ping", ServiceHandler.PingHandler)
	r.Get("/{id}", ServiceHandler.Redirect)

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
