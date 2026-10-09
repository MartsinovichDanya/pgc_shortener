package audit

import (
	"fmt"
	"sync"
	"time"

	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"go.uber.org/zap"
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
	UserID string `json:"user_id,omitempty"`
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
	closed    bool
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
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	s.observers = append(s.observers, o)
}

func (s *Subject) Notify(e Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return
	}

	select {
	case s.ch <- e:
	default:
		logger.Log.Debug("audit: event dropped, queue full",
			zap.String("action", string(e.Action)),
			zap.String("url", e.URL),
		)
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

func (s *Subject) Shutdown(timeout time.Duration) error {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		close(s.ch)
		s.mu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("audit: shutdown timeout after %s", timeout)
	}
}

type NoopNotifier struct{}

func (NoopNotifier) Notify(Event) {}
