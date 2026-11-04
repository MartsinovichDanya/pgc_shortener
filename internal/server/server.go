package server

import (
	"context"
	"errors"
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
	"golang.org/x/sync/errgroup"
)

const (
	certFile = "cert/cert.pem"
	keyFile  = "cert/private.pem"
)

// Run инициализирует все зависимости, запускает HTTP-сервер и дожидается
// отмены переданного контекста (сигнал ОС из main() или критическая ошибка
// сервера). После этого выполняет graceful shutdown всех созданных
// компонентов в порядке, обратном инициализации.
//
// Управление сигналами ОС и ожидание дочерних горутин выполняет main():
// он является единственной «корневой» горутиной, которая гарантирует,
// что до завершения процесса все остальные горутины остановятся.
func Run(ctx context.Context, cfg *config.Config) error {
	logger.Log.Debug("Running config", zap.Any("config", cfg))

	var store storage.Store
	var err error

	if cfg.UseDB {
		store, err = storage.NewPostgresStore(cfg.DatabaseDSN)
	} else {
		store, err = storage.NewFileStore(cfg.FileStoragePath)
	}
	if err != nil {
		logger.Log.Fatal("Storage creation failed", zap.Error(err))
		return err
	}
	logger.Log.Debug("Storage created")
	defer store.Close()

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

	ServiceHandler := handler.NewShortenerHandler(store, cfg.BaseURL, cfg.MaxBodySize, cfg.IDLength, cfg.UseDB, auditObserver, cfg.TrustedSubnet)
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
	r.Get("/api/internal/stats", ServiceHandler.Stats)

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

	srv := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: r,
	}

	// errgroup дожидается дочерних горутин и собирает их ошибки.
	// gCtx отменяется при отмене родительского ctx (сигнал из main())
	// либо при первой ошибке дочерней горутины.
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		if cfg.EnableHTTPS {
			logger.Log.Info("Starting HTTPS server",
				zap.String("addr", cfg.ServerAddr),
				zap.String("cert", certFile),
				zap.String("key", keyFile),
			)
			err = srv.ListenAndServeTLS(certFile, keyFile)
		} else {
			logger.Log.Info("Starting HTTP server", zap.String("addr", cfg.ServerAddr))
			err = srv.ListenAndServe()
		}
		// ErrServerClosed — нормальное завершение после Shutdown.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})

	// Ждём либо отмены контекста (сигнал ОС), либо критической ошибки сервера.
	<-gCtx.Done()
	logger.Log.Info("Shutdown initiated")

	// 1. Останавливаем приём новых соединений, дожидаемся активных хендлеров.
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Log.Error("HTTP server shutdown failed", zap.Error(err))
	}
	cancelShutdown()

	// 2. Дожидаемся завершения горутины сервера и получаем её ошибку.
	serverErr := g.Wait()

	// 3. Останавливаем фоновые воркеры хендлера (batch delete и т.п.).
	handlerCtx, cancelHandler := context.WithTimeout(context.Background(), 10*time.Second)
	ServiceHandler.Shutdown(handlerCtx)
	cancelHandler()

	// store.Close() и auditSubject.Shutdown() выполнятся через defer
	// в порядке, обратном регистрации.

	if serverErr != nil {
		logger.Log.Error("Server failed", zap.Error(serverErr))
		return serverErr
	}
	logger.Log.Info("Server stopped gracefully")
	return nil
}
