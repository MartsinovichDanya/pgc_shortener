package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"dario.cat/mergo"
	"github.com/caarlos0/env/v6"
)

// Config содержит настройки приложения.
type Config struct {
	ServerAddr      string `env:"SERVER_ADDRESS"`
	GRPCServerAddr  string `env:"GRPC_SERVER_ADDRESS"`
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
	TrustedSubnet   string `env:"TRUSTED_SUBNET"`
	ConfigFile      string `env:"CONFIG"`
	EnableAudit     bool
	UseDB           bool
}

// FileConfig — структура для чтения JSON-файла конфигурации.
type FileConfig struct {
	ServerAddr      string `json:"server_address"`
	GRPCServerAddr  string `json:"grpc_server_address"`
	BaseURL         string `json:"base_url"`
	FileStoragePath string `json:"file_storage_path"`
	DatabaseDSN     string `json:"database_dsn"`
	AuditFile       string `json:"audit_file"`
	AuditURL        string `json:"audit_url"`
	EnableHTTPS     bool   `json:"enable_https"`
	TrustedSubnet   string `json:"trusted_subnet"`
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

// GetConfig собирает конфигурацию из трёх источников.
//
// Приоритет (от высшего к низшему):
//  1. переменные окружения
//  2. явно заданные флаги командной строки
//  3. файл конфигурации
//  4. значения по умолчанию флагов
//
// Каждый слой передаётся в mergo.Merge по порядку; mergo заполняет только
// пустые поля, поэтому ранее слитые (более приоритетные) значения сохраняются.
// Логика одинакова для всех полей — добавление нового параметра сводится
// к добавлению одной строки в нужный слой, а не к новому блоку if.
func GetConfig() *Config {
	// --- Слой 1: переменные окружения (наивысший приоритет) ---
	envCfg := &Config{}
	if err := env.Parse(envCfg); err != nil {
		log.Fatal(err)
	}

	// ENABLE_HTTPS — единственное bool-поле, для которого «false» может быть
	// осознанным значением из env. mergo не отличает false от «не задано»,
	// поэтому вынесем его из общего слияния и посчитаем отдельно.
	envHTTPS := envCfg.EnableHTTPS
	_, envHTTPSSet := os.LookupEnv("ENABLE_HTTPS")

	// --- Слой 2: флаги командной строки ---
	var (
		flagServerAddr      string
		flagGRPCServerAddr  string
		flagBaseURL         string
		flagFileStoragePath string
		flagDatabaseDSN     string
		flagAuditFile       string
		flagAuditURL        string
		flagConfigFile      string
		flagTrustedSubnet   string
		flagEnableHTTPS     bool
	)

	flag.StringVar(&flagServerAddr, "a", "", "адрес запуска HTTP-сервера")
	flag.StringVar(&flagGRPCServerAddr, "g", "", "адрес запуска gRPC-сервера")
	flag.StringVar(&flagBaseURL, "b", "", "базовый адрес результирующего сокращённого URL")
	flag.StringVar(&flagFileStoragePath, "f", "", "путь к файлу-хранилищу")
	flag.StringVar(&flagDatabaseDSN, "d", "", "строка подключения в БД")
	flag.StringVar(&flagAuditFile, "audit-file", "", "путь к файлу-приемнику событий аудита")
	flag.StringVar(&flagAuditURL, "audit-url", "", "URL сервиса-приемника событий аудита")
	flag.BoolVar(&flagEnableHTTPS, "s", false, "использование защищенного протокола")
	flag.StringVar(&flagConfigFile, "config", "", "путь к файлу конфигурации")
	flag.StringVar(&flagConfigFile, "c", "", "путь к файлу конфигурации")
	flag.StringVar(&flagTrustedSubnet, "t", "", "доверенная подсеть")
	flag.Parse()

	setFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	// Явно заданные флаги — отдельный слой. Заполняем только реально
	// переданные значения, чтобы mergo не затирал более приоритетный env.
	explicitFlags := &Config{}
	if setFlags["a"] {
		explicitFlags.ServerAddr = flagServerAddr
	}
	if setFlags["g"] {
		explicitFlags.GRPCServerAddr = flagGRPCServerAddr
	}
	if setFlags["b"] {
		explicitFlags.BaseURL = flagBaseURL
	}
	if setFlags["f"] {
		explicitFlags.FileStoragePath = flagFileStoragePath
	}
	if setFlags["d"] {
		explicitFlags.DatabaseDSN = flagDatabaseDSN
	}
	if setFlags["t"] {
		explicitFlags.TrustedSubnet = flagTrustedSubnet
	}
	if setFlags["audit-file"] {
		explicitFlags.AuditFile = flagAuditFile
	}
	if setFlags["audit-url"] {
		explicitFlags.AuditURL = flagAuditURL
	}
	// EnableHTTPS — см. специальную обработку ниже.

	// --- Слой 3: файл конфигурации ---
	configFile := envCfg.ConfigFile
	if configFile == "" {
		configFile = flagConfigFile
	}

	fileCfg := &Config{}
	var fileEnableHTTPS bool
	if configFile != "" {
		fc, err := LoadFromJSON(configFile)
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		// FileConfig и Config — разные типы, поэтому маппим вручную.
		// Это единственный участок «клея» между структурами.
		fileCfg.ServerAddr = fc.ServerAddr
		fileCfg.GRPCServerAddr = fc.GRPCServerAddr
		fileCfg.BaseURL = fc.BaseURL
		fileCfg.FileStoragePath = fc.FileStoragePath
		fileCfg.DatabaseDSN = fc.DatabaseDSN
		fileCfg.AuditFile = fc.AuditFile
		fileCfg.AuditURL = fc.AuditURL
		fileCfg.TrustedSubnet = fc.TrustedSubnet
		fileEnableHTTPS = fc.EnableHTTPS
	}

	// --- Слой 4: значения по умолчанию флагов (низший приоритет) ---
	flagDefaults := &Config{
		ServerAddr:      "localhost:8080",
		GRPCServerAddr:  "localhost:9090",
		BaseURL:         "http://localhost:8080",
		FileStoragePath: "storage.json",
	}

	// --- Слияние: env → explicitFlags → fileCfg → flagDefaults ---
	// Каждый вызов mergo.Merge заполняет только пустые поля в result,
	// поэтому источник, слитый раньше, выигрывает без дополнительных проверок.
	result := envCfg
	for _, layer := range []*Config{explicitFlags, fileCfg, flagDefaults} {
		if err := mergo.Merge(result, layer); err != nil {
			log.Fatalf("merge config layer: %v", err)
		}
	}

	// --- Tri-state для EnableHTTPS ---
	// env (даже false) > явный флаг > файл > дефолт.
	switch {
	case envHTTPSSet:
		result.EnableHTTPS = envHTTPS
	case setFlags["s"]:
		result.EnableHTTPS = flagEnableHTTPS
	case fileEnableHTTPS:
		result.EnableHTTPS = true
	default:
		result.EnableHTTPS = false
	}

	result.ConfigFile = configFile
	result.EnableAudit = result.AuditFile != "" || result.AuditURL != ""
	result.UseDB = result.DatabaseDSN != ""

	return result
}
