package audit

import (
	"encoding/json"
	"os"

	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"go.uber.org/zap"
)

type FileObserver struct {
	file *os.File
}

func NewFileObserver(filepath string) (*FileObserver, error) {
	file, err := os.OpenFile(filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &FileObserver{file: file}, nil
}

func (f *FileObserver) Notify(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		logger.Log.Debug("audit: marshal error", zap.Error(err))
		return
	}

	// Добавляем перенос строки
	data = append(data, '\n')
	if _, err := f.file.Write(data); err != nil {
		logger.Log.Debug("audit: write file error",
			zap.Error(err),
			zap.String("filepath", f.file.Name()),
		)
	}
}

func (f *FileObserver) Close() error {
	return f.file.Close()
}
