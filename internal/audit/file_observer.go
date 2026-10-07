package audit

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"go.uber.org/zap"
)

type FileObserver struct {
	filepath string
	mu       sync.Mutex // Защищает файл от одновременной записи из разных горутин
}

func NewFileObserver(filepath string) *FileObserver {
	return &FileObserver{filepath: filepath}
}

func (f *FileObserver) Notify(event Event) {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		logger.Log.Debug("audit: marshal error", zap.Error(err))
		return
	}

	// Открываем файл в режиме дозаписи, создаем его при необходимости
	file, err := os.OpenFile(f.filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Log.Debug("audit: open file error",
			zap.Error(err),
			zap.String("filepath", f.filepath),
		)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			logger.Log.Debug("audit: close file error",
				zap.Error(err),
				zap.String("filepath", f.filepath),
			)
		}
	}()

	// Добавляем перенос строки
	data = append(data, '\n')
	if _, err := file.Write(data); err != nil {
		logger.Log.Debug("audit: write file error",
			zap.Error(err),
			zap.String("filepath", f.filepath),
		)
	}
}
