package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/response"
)

type identityKey struct{}

type Authenticator interface {
	Authenticate(context.Context, string) (domain.User, error)
}

func Identity(ctx context.Context) (domain.User, bool) {
	user, ok := ctx.Value(identityKey{}).(domain.User)
	return user, ok
}

func RequireJWT(service Authenticator, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headers := r.Header.Values("Authorization")
			if len(headers) != 1 {
				response.Unauthorized(w)
				return
			}
			parts := strings.Fields(headers[0])
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 4096 {
				response.Unauthorized(w)
				return
			}
			user, err := service.Authenticate(r.Context(), parts[1])
			if errors.Is(err, auth.ErrUnauthorized) {
				response.Unauthorized(w)
				return
			}
			if err != nil {
				logger.ErrorContext(r.Context(), "Could not load authenticated identity")
				if errors.Is(err, context.DeadlineExceeded) {
					response.Error(w, http.StatusServiceUnavailable, "UNAVAILABLE", "Servicio temporalmente no disponible")
					return
				}
				response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Error interno del servidor")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, user)))
		})
	}
}

// RequireRoles allows an authenticated user with any of the requested roles.
func RequireRoles(roles ...domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := Identity(r.Context())
			if !ok {
				response.Unauthorized(w)
				return
			}
			for _, role := range roles {
				if slices.Contains(user.Roles, role) {
					next.ServeHTTP(w, r)
					return
				}
			}
			response.Error(w, http.StatusForbidden, "FORBIDDEN", "Acceso denegado")
		})
	}
}
