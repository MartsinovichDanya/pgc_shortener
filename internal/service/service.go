package service

import (
	"context"
	"errors"
	"strings"

	"go.uber.org/zap"

	"github.com/MartsinovichDanya/pgc_shortener/internal/audit"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/model"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

// Service — общая бизнес-логика для HTTP- и gRPC-транспортов.
// Транспорты (handler / grpc) — тонкие фасады: они отвечают только за
// парсинг запроса, форматирование ответа и статусы.
type Service struct {
	store         storage.Store
	baseURL       string
	idLength      int
	auditObserver audit.Observer
}

func New(store storage.Store, baseURL string, idLength int, obs audit.Observer) *Service {
	if obs == nil {
		obs = audit.NoopNotifier{}
	}
	return &Service{
		store:         store,
		baseURL:       baseURL,
		idLength:      idLength,
		auditObserver: obs,
	}
}

// Shorten создаёт короткую ссылку.
// Если URL уже сохранён — возвращает существующий короткий URL и
// alreadyExists=true (HTTP: 409, gRPC: успешный ответ с тем же URL).
// Событие аудита ActionShorten отправляется только при реальном сохранении.
func (s *Service) Shorten(ctx context.Context, originalURL, userID string) (shortURL string, alreadyExists bool, err error) {
	id, err := utils.GenerateID(s.idLength)
	if err != nil {
		logger.Log.Debug("Ошибка генерации ID", zap.Error(err))
		return "", false, err
	}

	if err := s.store.Save(ctx, id, originalURL, userID); err != nil {
		if errors.Is(err, storage.ErrURLExists) {
			existingID, errGet := s.store.GetByOriginalURL(ctx, originalURL)
			if errGet != nil {
				logger.Log.Error("Ошибка получения существующего URL", zap.Error(errGet))
				return "", false, errGet
			}
			return s.joinShortURL(existingID), true, nil
		}
		logger.Log.Error("Ошибка сохранения URL", zap.Error(err))
		return "", false, err
	}

	s.auditObserver.Notify(audit.NewEvent(audit.ActionShorten, userID, originalURL))
	return s.joinShortURL(id), false, nil
}

// Expand возвращает оригинальный URL по короткому ID.
// Отправляет событие аудита ActionFollow (как в HTTP-редиректе).
// Ошибки хранилища возвращаются без обёртки — фасад сам решает,
// каким статусом/кодом их отдать (410 vs 400 vs NotFound).
func (s *Service) Expand(ctx context.Context, id, userID string) (string, error) {
	originalURL, err := s.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	s.auditObserver.Notify(audit.NewEvent(audit.ActionFollow, userID, originalURL))
	return originalURL, nil
}

// ListByUser возвращает ссылки пользователя с полными короткими URL.
func (s *Service) ListByUser(ctx context.Context, userID string) ([]model.UserURL, error) {
	urls, err := s.store.GetUserURLs(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range urls {
		urls[i].ShortURL = s.joinShortURL(urls[i].ShortURL)
	}
	return urls, nil
}

// BaseURL и IDLength — для HTTP-хендлера, который использует их в batch.
func (s *Service) BaseURL() string { return s.baseURL }
func (s *Service) IDLength() int   { return s.idLength }

func (s *Service) joinShortURL(id string) string {
	var sb strings.Builder
	sb.Grow(len(s.baseURL) + 1 + len(id))
	sb.WriteString(s.baseURL)
	sb.WriteByte('/')
	sb.WriteString(id)
	return sb.String()
}
