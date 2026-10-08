package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// AuthMiddleware возвращает middleware для аутентификации через подписанную куку.
func AuthMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var userID string
			var tokenInvalid bool

			cookie, err := r.Cookie(cookieName)
			if err != nil {
				// Кука отсутствует – создаём нового пользователя.
				userID = uuid.NewString()
			} else {
				// Кука есть – проверяем подпись.
				parts := strings.SplitN(cookie.Value, ":", 2)
				if len(parts) != 2 {
					tokenInvalid = true
				} else {
					uid, signature := parts[0], parts[1]
					if !validSignature(uid, signature, secret) {
						tokenInvalid = true
					} else {
						userID = uid
					}
				}
				// Если кука невалидна, генерируем нового пользователя для остальных эндпоинтов,
				// но помечаем контекст для возможной обработки 401.
				if tokenInvalid {
					userID = uuid.NewString()
				}
			}

			// Устанавливаем или обновляем куку с новым userID.
			signedValue := signUserID(userID, secret)
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    signedValue,
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

// signUserID возвращает строку вида "userID:HMAC-SHA256(userID, secret)".
func signUserID(userID, secret string) string {
	secretBytes := []byte(secret)
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(userID))
	sum := mac.Sum(nil) // 32 байта в буфере hash — одна аллокация
	sig := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(sig, sum)
	return userID + ":" + string(sig) // одна конкатенация вместо промежуточных строк
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
