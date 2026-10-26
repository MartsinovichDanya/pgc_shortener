package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/MartsinovichDanya/pgc_shortener/internal/server"
	"github.com/joho/godotenv"
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

	fmt.Printf("Build version: %s\nBuild date: %s\nBuild commit: %s\n", buildVersion, buildDate, buildCommit)
	server.Run()
}
