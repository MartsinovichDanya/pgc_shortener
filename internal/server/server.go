package server

import (
	"net/http"
	"time"

	"github.com/MartsinovichDanya/pgc_shortener/internal/audit"
	"github.com/MartsinovichDanya/pgc_shortener/internal/auth"
	"github.com/MartsinovichDanya/pgc_shortener/internal/config"
	"github.com/MartsinovichDanya/pgc_shortener/internal/handler"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/middleware"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	_ "github.com/MartsinovichDanya/pgc_shortener/swagger"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
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

	var auditObserver audit.Observer

	if cfg.EnableAudit {
		auditSubject := audit.NewSubject(4096)

		if cfg.AuditFile != "" {
			fo, err := audit.NewFileObserver(cfg.AuditFile)
			if err != nil {
				logger.Log.Fatal("audit: cannot open file", zap.Error(err))
			}
			defer fo.Close()
			auditSubject.Attach(fo)
		}

		if cfg.AuditURL != "" {
			auditSubject.Attach(audit.NewHTTPObserver(cfg.AuditURL))
		}

		auditObserver = auditSubject
		logger.Log.Debug("Audit initialized")

		defer auditSubject.Shutdown(2 * time.Second)
	}

	ServiceHandler := handler.NewShortenerHandler(store, cfg.BaseURL, cfg.MaxBodySize, cfg.IDLength, cfg.UseDB, auditObserver)

	logger.Log.Debug("Handler created")

	r := chi.NewRouter()

	// --- Глобальные middleware (применяются ко всем роутам) ---
	r.Use(chiMiddleware.RequestID)
	//r.Use(middleware.RealIP)
	r.Use(logger.GetLogger())
	r.Use(middleware.GzipMiddleware)
	r.Use(chiMiddleware.Recoverer)

	// --- Публичные роуты (без авторизации) ---
	r.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	// --- Защищённые роуты (требуют валидную auth-cookie) ---
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware(cfg.CookieSecret))

		r.Post("/", ServiceHandler.CreateShortLink)
		r.Post("/api/shorten", ServiceHandler.ShortenAPI)
		r.Post("/api/shorten/batch", ServiceHandler.ShortenBatch)
		r.Get("/api/user/urls", ServiceHandler.UserURLs)
		r.Delete("/api/user/urls", ServiceHandler.DeleteUserURLs)
		r.Get("/ping", ServiceHandler.PingHandler)
		r.Get("/{id}", ServiceHandler.Redirect)
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})

	//go func() {
	//	mux := http.NewServeMux()
	//	mux.HandleFunc("/debug/pprof/", pprof.Index)
	//	mux.HandleFunc("/debug/pprof/heap", pprof.Handler("heap").ServeHTTP)
	//	mux.HandleFunc("/debug/pprof/goroutine", pprof.Handler("goroutine").ServeHTTP)
	//	logger.Log.Debug("pprof listening on :6060")
	//	if err := http.ListenAndServe(":6060", mux); err != nil {
	//		logger.Log.Error("pprof server failed", zap.Error(err))
	//	}
	//}()

	if err := http.ListenAndServe(cfg.ServerAddr, r); err != nil {
		return err
	}

	return nil
}
