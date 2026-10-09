package model

//go:generate easyjson -all $GOFILE

// Request — запрос на сокращение ссылки (POST /api/shorten).
//
//easyjson:json
type Request struct {
	URL string `json:"url" example:"https://example.com"`
}

// Response — ответ с короткой ссылкой.
//
//easyjson:json
type Response struct {
	Result string `json:"result" example:"http://localhost:8080/abc123"`
}

// BatchRequestItem — элемент пакетного запроса (POST /api/shorten/batch).
//
//easyjson:json
type BatchRequestItem struct {
	CorrelationID string `json:"correlation_id" example:"1"`
	OriginalURL   string `json:"original_url" example:"https://example.com"`
}

// BatchResponseItem — элемент пакетного ответа.
//
//easyjson:json
type BatchResponseItem struct {
	CorrelationID string `json:"correlation_id" example:"1"`
	ShortURL      string `json:"short_url" example:"http://localhost:8080/abc123"`
}

// UserURL — ссылка пользователя (GET /api/user/urls).
//
//easyjson:json
type UserURL struct {
	ShortURL    string `json:"short_url" example:"http://localhost:8080/abc123"`
	OriginalURL string `json:"original_url" example:"https://example.com"`
}

type ErrorResponse struct {
	Error string `json:"error" example:"Некорректный URL"`
}
