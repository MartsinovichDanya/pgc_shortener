package config

import "flag"

// Config содержит настройки приложения.
type Config struct {
	ServerAddr  string // адрес запуска HTTP-сервера (флаг -a)
	BaseURL     string // базовый адрес для сокращённого URL (флаг -b)
	MaxBodySize int    // максимальный размер тела запроса (пока не задаётся флагом)
	IdLength    int
}

// ParseFlags обрабатывает аргументы командной строки и возвращает заполненную конфигурацию.
func ParseFlags() *Config {
	cfg := &Config{
		MaxBodySize: 2048,
		IdLength:    8,
	}
	flag.StringVar(&cfg.ServerAddr, "a", "localhost:8080", "адрес запуска HTTP-сервера")
	flag.StringVar(&cfg.BaseURL, "b", "http://localhost:8080", "базовый адрес результирующего сокращённого URL")
	flag.Parse()
	return cfg
}
