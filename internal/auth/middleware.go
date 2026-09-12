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
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(userID))
	sig := hex.EncodeToString(mac.Sum(nil))
	return userID + ":" + sig
}

// validSignature проверяет подпись.
func validSignature(userID, signature, secret string) bool {
	expectedMAC := hmac.New(sha256.New, []byte(secret))
	expectedMAC.Write([]byte(userID))
	expectedSig := hex.EncodeToString(expectedMAC.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expectedSig))
}
