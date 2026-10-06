package audit

import (
    "encoding/json"
    "os"
    "sync"
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
        // Логирование ошибки маршалинга (опционально)
        return
    }

    // Открываем файл в режиме дозаписи, создаем его при необходимости
    file, err := os.OpenFile(f.filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        // Логирование ошибки открытия файла (опционально)
        return
    }
    defer file.Close()

    // Добавляем перенос строки
    data = append(data, '\n')
    _, _ = file.Write(data)
}
