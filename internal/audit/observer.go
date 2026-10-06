package audit

import (
    "bytes"
    "encoding/json"
    "net/http"
    "os"
    "sync"
    "time"
)

// Action определяет тип действия
type Action string

const (
    ActionShorten Action = "shorten"
    ActionFollow  Action = "follow"
)

// Event представляет собой событие аудита
type Event struct {
    TS     int64  `json:"ts"`
    Action Action `json:"action"`
    UserID string `json:"user_id,omitempty"` // omitempty уберет поле, если оно пустое
    URL    string `json:"url"`
}

// NewEvent создает новое событие с текущим timestamp
func NewEvent(action Action, userID, url string) Event {
    return Event{
        TS:     time.Now().Unix(),
        Action: action,
        UserID: userID,
        URL:    url,
    }
}

// Observer описывает интерфейс наблюдателя
type Observer interface {
    Notify(event Event)
}

// Subject (или AuditLogger) управляет наблюдателями
type Subject struct {
    observers []Observer
    mu        sync.RWMutex
}

func NewSubject() *Subject {
    return &Subject{
        observers: make([]Observer, 0),
    }
}

// Attach добавляет нового наблюдателя
func (s *Subject) Attach(o Observer) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.observers = append(s.observers, o)
}

// Notify рассылает событие всем наблюдателям.
func (s *Subject) Notify(event Event) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    for _, obs := range s.observers {
        go obs.Notify(event)
    }
}
