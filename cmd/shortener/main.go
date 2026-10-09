package main

import (
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
func main() {
	//err := godotenv.Load()
	//if err != nil {
	//	log.Fatal("Error loading .env file")
	//}
	server.Run()
}
