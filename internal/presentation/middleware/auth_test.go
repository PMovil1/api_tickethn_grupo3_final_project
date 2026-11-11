package middleware

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
)

type authenticatorFunc func(context.Context, string) (domain.User, error)

func (f authenticatorFunc) Authenticate(ctx context.Context, token string) (domain.User, error) {
	return f(ctx, token)
}

func TestRequireRoles(t *testing.T) {
	service := authenticatorFunc(func(_ context.Context, token string) (domain.User, error) {
		switch token {
		case "buyer":
			return domain.User{Roles: []domain.Role{domain.Buyer}}, nil
		case "organizer":
			return domain.User{Roles: []domain.Role{domain.Buyer, domain.Organizer}}, nil
		default:
			return domain.User{}, auth.ErrUnauthorized
		}
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	restricted := RequireRoles(domain.Admin, domain.Organizer)(next)
	handler := RequireJWT(service, logger)(restricted)
	for _, tc := range []struct {
		header string
		want   int
	}{
		{"", http.StatusUnauthorized},
		{"Bearer invalid", http.StatusUnauthorized},
		{"Bearer buyer", http.StatusForbidden},
		{"Bearer organizer", http.StatusOK},
	} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.header != "" {
			request.Header.Set("Authorization", tc.header)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != tc.want {
			t.Errorf("authorization %q returned %d, want %d", tc.header, recorder.Code, tc.want)
		}
	}
}
