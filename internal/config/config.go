package config

import (
	"flag"
	"log"

	"github.com/caarlos0/env/v6"
)

// Config содержит настройки приложения.
type Config struct {
	ServerAddr      string `env:"SERVER_ADDRESS"`                  // адрес запуска HTTP-сервера (флаг -a)
	BaseURL         string `env:"BASE_URL"`                        // базовый адрес для сокращённого URL (флаг -b)
	MaxBodySize     int    `env:"MAX_BODY_SIZE" envDefault:"2048"` // максимальный размер тела запроса (пока не задаётся флагом)
	IDLength        int    `env:"ID_LENGTH" envDefault:"8"`
	LogLevel        string `env:"LOG_LEVEL" envDefault:"DEBUG"`
	FileStoragePath string `env:"FILE_STORAGE_PATH"`
	DatabaseDSN     string `env:"DATABASE_DSN"`
	UseDB           bool
}

// GetConfig обрабатывает аргументы командной строки и переменные окружения, возвращает заполненную конфигурацию.
func GetConfig() *Config {
	var cfg Config
	var flagServerAddr, flagBaseURL, flagFileStoragePath,
		flagDatabaseDSN string

	err := env.Parse(&cfg)
	if err != nil {
		log.Fatal(err)
	}

	flag.StringVar(&flagServerAddr, "a", "localhost:8080", "адрес запуска HTTP-сервера")
	flag.StringVar(&flagBaseURL, "b", "http://localhost:8080", "базовый адрес результирующего сокращённого URL")
	flag.StringVar(&flagFileStoragePath, "f", "storage.json", "путь к файлу-хранилищу")
	flag.StringVar(&flagDatabaseDSN, "d", "", "Строка подключения в БД")

	flag.Parse()

	if cfg.ServerAddr == "" {
		cfg.ServerAddr = flagServerAddr
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = flagBaseURL
	}
	if cfg.FileStoragePath == "" {
		cfg.FileStoragePath = flagFileStoragePath
	}
	if cfg.DatabaseDSN == "" {
		cfg.DatabaseDSN = flagDatabaseDSN
	}

	cfg.UseDB = cfg.DatabaseDSN != ""

	return &cfg
}
