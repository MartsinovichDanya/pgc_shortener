package main

import (
	"fmt"

	"github.com/MartsinovichDanya/pgc_shortener/internal/server"
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
	//err := godotenv.Load()
	//if err != nil {
	//	log.Fatal("Error loading .env file")
	//}

	fmt.Printf("version: %s\ndate: %s\ncommit: %s\n", buildVersion, buildDate, buildCommit)
	server.Run()
}
