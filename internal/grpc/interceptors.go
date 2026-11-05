package grpc

import (
	"context"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/MartsinovichDanya/pgc_shortener/internal/auth"
	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
)

// AuthInterceptor читает токен из metadata["authorization"], проверяет его
// через auth.ValidateToken и кладёт userID в context — тот же ключ,
// что использует HTTP-middleware. Дальше сервис/хендлер работают одинаково.
func AuthInterceptor(secret string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		values := md.Get("authorization")
		if len(values) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}

		token := strings.TrimSpace(values[0])
		token = strings.TrimPrefix(token, "Bearer ")

		userID, err := auth.ValidateToken(token, secret)
		if err != nil {
			logger.Log.Debug("gRPC auth failed", zap.Error(err))
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		ctx = context.WithValue(ctx, auth.UserIDKey, userID)
		return handler(ctx, req)
	}
}
