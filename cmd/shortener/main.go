package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/MartsinovichDanya/pgc_shortener/internal/config"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/server"
	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
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

	server.Run(cfg)
}
