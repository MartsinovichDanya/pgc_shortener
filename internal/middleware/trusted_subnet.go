package middleware

import (
	"net"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/MartsinovichDanya/pgc_shortener/internal/logger"
)

// TrustedSubnet возвращает middleware, ограничивающее доступ клиентами,
// чей IP (из заголовка X-Real-IP) входит в заданную подсеть.
//
// Пустой или некорректный spec => middleware отклоняет все запросы (403):
// это безопасный дефолт, когда trusted_subnet не сконфигурирован.
func TrustedSubnet(spec string) func(http.Handler) http.Handler {
	var subnet *net.IPNet
	if ts := strings.TrimSpace(spec); ts != "" {
		_, parsed, err := net.ParseCIDR(ts)
		if err != nil {
			logger.Log.Error("invalid trusted_subnet",
				zap.String("trusted_subnet", ts),
				zap.Error(err),
			)
		} else {
			subnet = parsed
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if subnet == nil {
				writeForbidden(w)
				return
			}

			ipStr := strings.TrimSpace(r.Header.Get("X-Real-IP"))
			ip := net.ParseIP(ipStr)
			if ip == nil || !subnet.Contains(ip) {
				writeForbidden(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"Forbidden"}`))
}
