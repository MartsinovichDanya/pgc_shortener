package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/MartsinovichDanya/pgc_shortener/api/shortener/proto"

	"github.com/MartsinovichDanya/pgc_shortener/internal/audit"
	"github.com/MartsinovichDanya/pgc_shortener/internal/auth"
	"github.com/MartsinovichDanya/pgc_shortener/internal/config"
	grpcsrv "github.com/MartsinovichDanya/pgc_shortener/internal/grpc"
	"github.com/MartsinovichDanya/pgc_shortener/internal/handler"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/middleware"
	"github.com/MartsinovichDanya/pgc_shortener/internal/service"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	_ "github.com/MartsinovichDanya/pgc_shortener/swagger"
)

const (
	certFile = "cert/cert.pem"
	keyFile  = "cert/private.pem"

	shutdownTimeout  = 10 * time.Second
	auditShutdownDur = 2 * time.Second
)

// Run — единая точка запуска: инициализирует общий storage, общий audit,
// общий service, поднимает HTTP- и gRPC-серверы и выполняет graceful shutdown
// всех компонентов при отмене ctx.
func Run(ctx context.Context, cfg *config.Config) error {
	logger.Log.Debug("Running config", zap.Any("config", cfg))

	// === 1. Storage (общий) ===
	var store storage.Store
	var err error
	if cfg.UseDB {
		store, err = storage.NewPostgresStore(cfg.DatabaseDSN)
	} else {
		store, err = storage.NewFileStore(cfg.FileStoragePath)
	}
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	// ВАЖНО: у storage.Store метод Close() не возвращает ошибку.
	defer store.Close()
	logger.Log.Debug("Storage created")

	// === 2. Audit (общий) ===
	var auditObserver audit.Observer
	if cfg.EnableAudit {
		subject := audit.NewSubject(4096)

		if cfg.AuditFile != "" {
			fo, err := audit.NewFileObserver(cfg.AuditFile)
			if err != nil {
				return fmt.Errorf("open audit file: %w", err)
			}
			// Если у вашего FileObserver Close() возвращает error — верните
			// проверку: defer func() { _ = fo.Close() }() и залогируйте.
			defer fo.Close()
			subject.Attach(fo)
		}
		if cfg.AuditURL != "" {
			subject.Attach(audit.NewHTTPObserver(cfg.AuditURL))
		}

		auditObserver = subject
		defer subject.Shutdown(auditShutdownDur)
		logger.Log.Debug("Audit initialized")
	}

	// === 3. Service (общий для HTTP и gRPC) ===
	svc := service.New(store, cfg.BaseURL, cfg.IDLength, auditObserver)
	logger.Log.Debug("Service created")

	// === 4. HTTP-обработчик и сервер ===
	h := handler.NewShortenerHandler(
		store, svc,
		cfg.BaseURL, cfg.MaxBodySize, cfg.IDLength,
		cfg.UseDB,
	)
	httpSrv := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: buildHTTPRouter(cfg, h),
	}

	// === 5. gRPC-сервер ===
	grpcSrv, err := buildGRPCServer(cfg, svc)
	if err != nil {
		return fmt.Errorf("init grpc: %w", err)
	}
	grpcAddr := cfg.GRPCServerAddr

	// === 6. Запуск обоих серверов ===
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		logger.Log.Info("HTTP server starting",
			zap.String("addr", cfg.ServerAddr),
			zap.Bool("https", cfg.EnableHTTPS),
		)
		var serveErr error
		if cfg.EnableHTTPS {
			serveErr = httpSrv.ListenAndServeTLS(certFile, keyFile)
		} else {
			serveErr = httpSrv.ListenAndServe()
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("http serve: %w", serveErr)
		}
		return nil
	})

	g.Go(func() error {
		lis, err := net.Listen("tcp", grpcAddr)
		if err != nil {
			return fmt.Errorf("grpc listen %q: %w", grpcAddr, err)
		}
		logger.Log.Info("gRPC server starting",
			zap.String("addr", grpcAddr),
			zap.Bool("https", cfg.EnableHTTPS),
		)
		if serveErr := grpcSrv.Serve(lis); serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
			return fmt.Errorf("grpc serve: %w", serveErr)
		}
		return nil
	})

	// === 7. Ждём сигнал ОС или ошибку любого из серверов ===
	<-gCtx.Done()
	logger.Log.Info("shutdown initiated")

	// === 8. Graceful shutdown HTTP ===
	httpCtx, cancelHTTP := context.WithTimeout(context.Background(), shutdownTimeout)
	if err := httpSrv.Shutdown(httpCtx); err != nil {
		logger.Log.Error("http shutdown failed", zap.Error(err))
	}
	cancelHTTP()

	// === 9. Graceful shutdown gRPC (GracefulStop без ctx — таймаут через горутину) ===
	grpcDone := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop()
		close(grpcDone)
	}()
	select {
	case <-grpcDone:
	case <-time.After(shutdownTimeout):
		logger.Log.Warn("grpc graceful stop timed out, forcing")
		grpcSrv.Stop()
		<-grpcDone
	}

	// === 10. Фоновый воркер batch-delete ===
	handlerCtx, cancelHandler := context.WithTimeout(context.Background(), shutdownTimeout)
	h.Shutdown(handlerCtx)
	cancelHandler()

	// === 11. Дожидаемся serve-горутин ===
	if err := g.Wait(); err != nil {
		logger.Log.Error("servers stopped with error", zap.Error(err))
		return err
	}

	// store.Close() и subject.Shutdown() выполнятся через defer в порядке,
	// обратном регистрации: audit → file observer → store.

	logger.Log.Info("servers stopped gracefully")
	return nil
}

// buildHTTPRouter собирает chi-роутер поверх готового handler.
func buildHTTPRouter(cfg *config.Config, h *handler.ShortenerHandler) http.Handler {
	r := chi.NewRouter()

	r.Use(chiMiddleware.RequestID)
	// r.Use(middleware.RealIP)
	r.Use(logger.GetLogger())
	r.Use(middleware.GzipMiddleware)
	r.Use(chiMiddleware.Recoverer)

	// Публичные роуты
	r.Get("/swagger/*", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))

	// /stats защищён проверкой доверенной подсети.
	// Пустой/невалидный trusted_subnet => middleware отклоняет все запросы.
	r.With(middleware.TrustedSubnet(cfg.TrustedSubnet)).
		Get("/api/internal/stats", h.Stats)

	// Защищённые роуты
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware(cfg.CookieSecret))
		r.Post("/", h.CreateShortLink)
		r.Post("/api/shorten", h.ShortenAPI)
		r.Post("/api/shorten/batch", h.ShortenBatch)
		r.Get("/api/user/urls", h.UserURLs)
		r.Delete("/api/user/urls", h.DeleteUserURLs)
		r.Get("/ping", h.PingHandler)
		r.Get("/{id}", h.Redirect)
	})

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})
	return r
}

// buildGRPCServer собирает grpc.Server с интерсептором auth и TLS (если включён).
func buildGRPCServer(cfg *config.Config, svc *service.Service) (*grpc.Server, error) {
	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			grpcsrv.AuthInterceptor(cfg.CookieSecret),
		),
	}
	if cfg.EnableHTTPS {
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load grpc tls: %w", err)
		}
		opts = append(opts, grpc.Creds(creds))
	}

	srv := grpc.NewServer(opts...)
	pb.RegisterShortenerServiceServer(srv, grpcsrv.New(svc))
	return srv, nil
}
