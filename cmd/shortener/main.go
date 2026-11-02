package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/MartsinovichDanya/pgc_shortener/internal/config"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/server"
	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// @title           PGC Shortener API
// @version         1.0
// @description     API сервиса сокращения ссылок.
// @BasePath        /
// @schemes         http https

// @securityDefinitions.apikey CookieAuth
// @in              cookie
// @name            user_token

var buildVersion = "N/A"
var buildDate = "N/A"
var buildCommit = "N/A"

func main() {
	if err := godotenv.Load(); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Printf("Warning: error loading .env file: %v", err)
		}
	}
	cfg := config.GetConfig()

	if err := logger.Initialize(cfg.LogLevel); err != nil {
		utils.Exit(1)
	}

	logger.Log.Info("Build info",
		zap.String("Build version", buildVersion),
		zap.String("Build date", buildDate),
		zap.String("Build commit", buildCommit),
	)

	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT,
	)
	defer stop()

	// Корневая горутина: errgroup дожидается завершения всех дочерних
	// горутин и собирает критические ошибки. К моменту возврата из
	// g.Wait() гарантировано, что все горутины сервиса остановлены.
	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return server.Run(gCtx, cfg)
	})

	if err := g.Wait(); err != nil {
		logger.Log.Error("Application stopped with error", zap.Error(err))
		_ = logger.Log.Sync()
		utils.Exit(1)
	}

	logger.Log.Info("Application stopped gracefully")
	_ = logger.Log.Sync()
}
