package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func New(health http.Handler) *chi.Mux {
	routes := chi.NewRouter()
	routes.Get("/api/health", health.ServeHTTP)
	return routes
}
