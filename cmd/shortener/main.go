package main

import (
	//"github.com/joho/godotenv"

	"github.com/MartsinovichDanya/pgc_shortener/internal/server"
)

func main() {
	//err := godotenv.Load()
	//if err != nil {
	//	log.Fatal("Error loading .env file")
	//}

	server.Run()
}
