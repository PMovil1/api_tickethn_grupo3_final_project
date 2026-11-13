package router

import (
	"context"
	_ "embed"
	"net/http"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/handlers"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/middleware"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/response"
	"github.com/go-chi/chi/v5"
)

//go:embed openapi.yaml
var openAPI []byte

func Auth(routes chi.Router, h *handlers.Auth, requireJWT func(http.Handler) http.Handler) {
	routes.Get("/api/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openAPI)
	})
	routes.Route("/api/auth", func(r chi.Router) {
		r.Use(authTimeout)
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			response.Error(w, http.StatusNotFound, "NOT_FOUND", "Ruta no encontrada")
		})
		r.Post("/register", h.Register)
		r.Post("/verify-email", h.VerifyEmail)
		r.Post("/resend-verification", h.ResendVerification)
		r.Post("/login", h.Login)
		r.Post("/refresh", h.Refresh)
		r.Post("/forgot-password", h.ForgotPassword)
		r.Post("/reset-password", h.ResetPassword)
		r.Group(func(protected chi.Router) {
			protected.Use(requireJWT)
			protected.Post("/logout", h.Logout)
			protected.Get("/me", h.Me)
			protected.Get("/sessions", h.Sessions)
			protected.Delete("/sessions/{id}", h.RevokeSession)
		})
	})
	routes.Route("/api/admin/users", func(admin chi.Router) {
		admin.Use(authTimeout, requireJWT, middleware.RequireRoles(domain.Admin))
		admin.Get("/", h.Users)
		admin.Put("/{id}/roles/{role}", h.AssignUserRole)
		admin.Delete("/{id}/roles/{role}", h.RemoveUserRole)
	})
}

func authTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
