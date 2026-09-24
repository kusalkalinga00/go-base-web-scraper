package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func NewRouter(h Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"*"},
	}))

	r.Post("/api/scrape", h.SubmitScrape)
	r.Get("/api/scrape", h.ListScrapes)
	r.Get("/api/scrape/{taskID}", h.GetScrape)
	r.Delete("/api/scrape/{taskID}", h.DeleteScrape)
	r.Get("/api/pdf-images/{taskID}/{filename}", h.ServePDFImage)
	r.Get("/api/stats", h.GetStats)
	r.Get("/api/queue/status", h.GetQueueStatus)
	r.Get("/api/system", h.GetSystemInfo)
	r.Post("/api/cleanup", h.CleanupStorage)
	r.Get("/api/health", h.HealthCheck)

	return r
}
