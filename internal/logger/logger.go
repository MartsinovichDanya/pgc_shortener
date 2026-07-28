package logger

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

var Log *zap.Logger = zap.NewNop()

func Initialize(level string) error {
	// преобразуем текстовый уровень логирования в zap.AtomicLevel
	lvl, err := zap.ParseAtomicLevel(level)
	if err != nil {
		return err
	}
	// создаём новую конфигурацию логера
	cfg := zap.NewProductionConfig()
	// устанавливаем уровень
	cfg.Level = lvl
	// создаём логер на основе конфигурации
	zl, err := cfg.Build()
	if err != nil {
		return err
	}
	// устанавливаем синглтон
	Log = zl
	defer Log.Sync()
	return nil
}

func GetLogger() func(next http.Handler) http.Handler {
	return middleware.RequestLogger(&Formatter{Log})
}

type Formatter struct {
	logger *zap.Logger
}

type LogEntry struct {
	logger *zap.Logger
	method string
	path   string
}

func (f *Formatter) NewLogEntry(r *http.Request) middleware.LogEntry {
	return &LogEntry{
		logger: f.logger,
		method: r.Method,
		path:   r.URL.Path,
	}
}

func (e *LogEntry) Write(status, bytes int, header http.Header, elapsed time.Duration, extra interface{}) {
	e.logger.Info("request",
		zap.String("method", e.method),
		zap.String("path", e.path),
		zap.Int("status", status),
		zap.Int("bytes", bytes),
		zap.Duration("elapsed", elapsed),
	)
}

func (e *LogEntry) Panic(v interface{}, stack []byte) {
	e.logger.Error("panic",
		zap.Any("panic", v),
		zap.String("stack", string(stack)),
	)
}
