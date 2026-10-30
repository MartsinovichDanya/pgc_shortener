package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/caarlos0/env/v6"
)

// Config содержит настройки приложения.
type Config struct {
	ServerAddr      string `env:"SERVER_ADDRESS"`
	BaseURL         string `env:"BASE_URL"`
	LogLevel        string `env:"LOG_LEVEL"        envDefault:"DEBUG"`
	FileStoragePath string `env:"FILE_STORAGE_PATH"`
	DatabaseDSN     string `env:"DATABASE_DSN"`
	CookieSecret    string `env:"COOKIE_SECRET"    envDefault:"secret123"`
	AuditFile       string `env:"AUDIT_FILE"`
	AuditURL        string `env:"AUDIT_URL"`
	MaxBodySize     int    `env:"MAX_BODY_SIZE"    envDefault:"2048"`
	IDLength        int    `env:"ID_LENGTH"        envDefault:"8"`
	EnableHTTPS     bool   `env:"ENABLE_HTTPS"`
	ConfigFile      string `env:"CONFIG"`
	EnableAudit     bool
	UseDB           bool
}

// FileConfig — структура для чтения JSON-файла конфигурации.
// Содержит только те поля, которые разрешено задавать в файле.
type FileConfig struct {
	ServerAddr      string `json:"server_address"`
	BaseURL         string `json:"base_url"`
	FileStoragePath string `json:"file_storage_path"`
	DatabaseDSN     string `json:"database_dsn"`
	AuditFile       string `json:"audit_file"`
	AuditURL        string `json:"audit_url"`
	EnableHTTPS     bool   `json:"enable_https"`
}

// LoadFromJSON читает конфигурацию из JSON-файла.
func LoadFromJSON(path string) (*FileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}
	cfg := &FileConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, err)
	}
	return cfg, nil
}

// GetConfig обрабатывает аргументы командной строки и переменные окружения, возвращает заполненную конфигурацию.
func GetConfig() *Config {
	var cfg Config
	var flagServerAddr, flagBaseURL, flagFileStoragePath,
		flagDatabaseDSN, flagAuditFile, flagAuditURL,
		flagConfigFile string
	var flagEnableHTTPS bool

	err := env.Parse(&cfg)
	if err != nil {
		log.Fatal(err)
	}

	flag.StringVar(&flagServerAddr, "a", "localhost:8080", "адрес запуска HTTP-сервера")
	flag.StringVar(&flagBaseURL, "b", "http://localhost:8080", "базовый адрес результирующего сокращённого URL")
	flag.StringVar(&flagFileStoragePath, "f", "storage.json", "путь к файлу-хранилищу")
	flag.StringVar(&flagDatabaseDSN, "d", "", "строка подключения в БД")
	flag.StringVar(&flagAuditFile, "audit-file", "", "путь к файлу-приемнику событий аудита")
	flag.StringVar(&flagAuditURL, "audit-url", "", "URL сервиса-приемника событий аудита")
	flag.BoolVar(&flagEnableHTTPS, "s", false, "Использование защищенного протокола")

	flag.StringVar(&flagConfigFile, "config", "", "путь к файлу конфигурации")
	flag.StringVar(&flagConfigFile, "c", "", "путь к файлу конфигурации")

	flag.Parse()

	setFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	if cfg.ConfigFile == "" {
		cfg.ConfigFile = flagConfigFile
	}

	var fileCfg FileConfig
	if cfg.ConfigFile != "" {
		fc, err := LoadFromJSON(cfg.ConfigFile)
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		fileCfg = *fc
	}

	if cfg.ServerAddr == "" {
		if !setFlags["a"] && fileCfg.ServerAddr != "" {
			cfg.ServerAddr = fileCfg.ServerAddr
		} else {
			cfg.ServerAddr = flagServerAddr
		}
	}

	if cfg.BaseURL == "" {
		if !setFlags["b"] && fileCfg.BaseURL != "" {
			cfg.BaseURL = fileCfg.BaseURL
		} else {
			cfg.BaseURL = flagBaseURL
		}
	}

	if cfg.FileStoragePath == "" {
		if !setFlags["f"] && fileCfg.FileStoragePath != "" {
			cfg.FileStoragePath = fileCfg.FileStoragePath
		} else {
			cfg.FileStoragePath = flagFileStoragePath
		}
	}

	if cfg.DatabaseDSN == "" {
		if !setFlags["d"] && fileCfg.DatabaseDSN != "" {
			cfg.DatabaseDSN = fileCfg.DatabaseDSN
		} else {
			cfg.DatabaseDSN = flagDatabaseDSN
		}
	}

	if cfg.AuditFile == "" {
		if !setFlags["audit-file"] && fileCfg.AuditFile != "" {
			cfg.AuditFile = fileCfg.AuditFile
		} else {
			cfg.AuditFile = flagAuditFile
		}
	}

	if cfg.AuditURL == "" {
		if !setFlags["audit-url"] && fileCfg.AuditURL != "" {
			cfg.AuditURL = fileCfg.AuditURL
		} else {
			cfg.AuditURL = flagAuditURL
		}
	}

	if _, envHTTPSSet := os.LookupEnv("ENABLE_HTTPS"); !envHTTPSSet {
		if !setFlags["s"] && fileCfg.EnableHTTPS {
			cfg.EnableHTTPS = true
		} else {
			cfg.EnableHTTPS = flagEnableHTTPS
		}
	}

	cfg.EnableAudit = cfg.AuditFile != "" || cfg.AuditURL != ""

	cfg.UseDB = cfg.DatabaseDSN != ""

	return &cfg
}
