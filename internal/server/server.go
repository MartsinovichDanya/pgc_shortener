package server

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/MartsinovichDanya/pgc_shortener/internal/config"
	"github.com/MartsinovichDanya/pgc_shortener/internal/handler"
	"github.com/MartsinovichDanya/pgc_shortener/internal/storage"
)

// Run инициализирует все зависимости, настраивает роутер и запускает HTTP-сервер.
func Run() {
	cfg := config.ParseFlags()

	store := storage.NewStore()
	handler := handler.NewShortenerHandler(store, cfg.BaseURL, cfg.MaxBodySize, cfg.IdLength)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/", handler.CreateShortLink)
	r.Get("/{id}", handler.Redirect)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
	})

	log.Printf("Сервер запущен на %s", cfg.ServerAddr)
	log.Fatal(http.ListenAndServe(cfg.ServerAddr, r))
}
