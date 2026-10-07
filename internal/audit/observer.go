package audit

import (
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
	mu        sync.RWMutex
	observers []Observer
	ch        chan Event
	wg        sync.WaitGroup
	once      sync.Once
}

func NewSubject(buffer int) *Subject {
	s := &Subject{
		observers: make([]Observer, 0),
		ch:        make(chan Event, buffer),
	}
	s.wg.Add(1)
	go s.worker()
	return s
}

func (s *Subject) Attach(o Observer) {
	s.mu.Lock()
	s.observers = append(s.observers, o)
	s.mu.Unlock()
}

func (s *Subject) Notify(e Event) {
	select {
	case s.ch <- e:
	default:
	}
}

func (s *Subject) worker() {
	defer s.wg.Done()
	for e := range s.ch {
		s.mu.RLock()
		obs := s.observers
		s.mu.RUnlock()
		for _, o := range obs {
			o.Notify(e)
		}
	}
}

func (s *Subject) Shutdown(timeout time.Duration) {
	s.once.Do(func() { close(s.ch) })
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

type NoopNotifier struct{}

func (NoopNotifier) Notify(Event) {}
