package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type contextKey string

const (
	UserIDKey       contextKey = "userID"
	TokenInvalidKey contextKey = "tokenInvalid"
)

const cookieName = "user_token"

// ErrInvalidToken возвращается ValidateToken при некорректной подписи/формате.
var ErrInvalidToken = errors.New("invalid token")

// AuthMiddleware возвращает middleware для аутентификации через подписанную куку.
func AuthMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var userID string
			var tokenInvalid bool

			cookie, err := r.Cookie(cookieName)
			switch {
			case err != nil:
				// Кука отсутствует – создаём нового пользователя.
				userID = uuid.NewString()
			default:
				uid, verr := ValidateToken(cookie.Value, secret)
				if verr != nil {
					tokenInvalid = true
					userID = uuid.NewString()
				} else {
					userID = uid
				}
			}

			// Устанавливаем или обновляем куку с новым userID.
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    SignUserID(userID, secret),
				Path:     "/",
				HttpOnly: true,
				Secure:   false,
				SameSite: http.SameSiteLaxMode,
			})

			// Помещаем userID и флаг ошибки в контекст.
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			if tokenInvalid {
				ctx = context.WithValue(ctx, TokenInvalidKey, true)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ValidateToken проверяет токен формата "userID:hex(HMAC-SHA256(userID, secret))"
// и возвращает userID. Используется и HTTP-middleware (косвенно), и gRPC-interceptor.
func ValidateToken(token, secret string) (string, error) {
	token = strings.TrimSpace(token)
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return "", ErrInvalidToken
	}
	userID, signature := parts[0], parts[1]
	if userID == "" || !validSignature(userID, signature, secret) {
		return "", ErrInvalidToken
	}
	return userID, nil
}

// SignUserID возвращает подписанный токен "userID:signature".
// Парная к ValidateToken; нужна HTTP-middleware и gRPC-клиентам.
func SignUserID(userID, secret string) string {
	return signUserID(userID, secret)
}

// signUserID возвращает строку вида "userID:HMAC-SHA256(userID, secret)".
func signUserID(userID, secret string) string {
	secretBytes := []byte(secret)
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(userID))
	sum := mac.Sum(nil)
	sig := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(sig, sum)
	return userID + ":" + string(sig)
}

// validSignature проверяет подпись без hex-строки expected.
func validSignature(userID, signature, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(userID))
	sum := mac.Sum(nil)
	sigBytes, err := hex.DecodeString(signature)
	if err != nil || len(sigBytes) != len(sum) {
		return false
	}
	return hmac.Equal(sigBytes, sum)
}
