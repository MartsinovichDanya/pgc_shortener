package grpc

import (
	"context"
	"errors"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/MartsinovichDanya/pgc_shortener/api/shortener/proto"

	"github.com/MartsinovichDanya/pgc_shortener/internal/auth"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
	"github.com/MartsinovichDanya/pgc_shortener/internal/service"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
)

// Server — gRPC-фасад к общему service. Вся бизнес-логика — в *service.Service,
// аудит и работа с хранилищем тоже там. Здесь только маппинг proto ↔ service.
type Server struct {
	pb.UnimplementedShortenerServiceServer
	svc *service.Service
}

// New создаёт gRPC-сервер поверх готового service (он же используется HTTP-хендлером).
func New(svc *service.Service) *Server {
	return &Server{svc: svc}
}

// userIDFromContext единообразно извлекает userID из контекста.
// Возвращает Unauthenticated, если userID отсутствует или пуст —
// одинаково для всех RPC, чтобы нельзя было сохранить ссылку без владельца
// или выполнить действие от анонима.
func userIDFromContext(ctx context.Context) (string, error) {
	userID, ok := ctx.Value(auth.UserIDKey).(string)
	if !ok || userID == "" {
		return "", status.Error(codes.Unauthenticated, "unauthorized")
	}
	return userID, nil
}

// ShortenURL создаёт короткую ссылку.
func (s *Server) ShortenURL(ctx context.Context, req *pb.URLShortenRequest) (*pb.URLShortenResponse, error) {
	originalURL := req.GetUrl()
	if originalURL == "" {
		return nil, status.Error(codes.InvalidArgument, "url is required")
	}

	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	shortURL, _, err := s.svc.Shorten(ctx, originalURL, userID)
	if err != nil {
		logger.Log.Error("gRPC Shorten error", zap.Error(err))
		return nil, status.Error(codes.Internal, "internal error")
	}
	return &pb.URLShortenResponse{Result: shortURL}, nil
}

// ExpandURL возвращает оригинальный URL по короткому ID.
func (s *Server) ExpandURL(ctx context.Context, req *pb.URLExpandRequest) (*pb.URLExpandResponse, error) {
	id := req.GetId()
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	originalURL, err := s.svc.Expand(ctx, id, userID)
	if err != nil {
		if errors.Is(err, storage.ErrURLDeleted) {
			return nil, status.Error(codes.NotFound, "url deleted")
		}
		logger.Log.Debug("gRPC Expand: not found",
			zap.String("id", id), zap.Error(err))
		return nil, status.Error(codes.NotFound, "url not found")
	}
	return &pb.URLExpandResponse{Result: originalURL}, nil
}

// ListUserURLs возвращает ссылки текущего пользователя.
func (s *Server) ListUserURLs(ctx context.Context, _ *emptypb.Empty) (*pb.UserURLsResponse, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	urls, err := s.svc.ListByUser(ctx, userID)
	if err != nil {
		logger.Log.Error("gRPC ListUserURLs error", zap.Error(err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := &pb.UserURLsResponse{
		Url: make([]*pb.URLData, 0, len(urls)),
	}
	for _, u := range urls {
		resp.Url = append(resp.Url, &pb.URLData{
			ShortUrl:    u.ShortURL,
			OriginalUrl: u.OriginalURL,
		})
	}
	return resp, nil
}
