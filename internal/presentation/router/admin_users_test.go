package router

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/handlers"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/middleware"
	"github.com/go-chi/chi/v5"
)

const targetUser = "73428bdd-e13e-44bc-92b3-66c43a622824"

type adminUserRepo struct {
	auth.Repository
	changes []bool
}

func (*adminUserRepo) Users(context.Context) ([]domain.User, error) {
	return []domain.User{{ID: targetUser, PasswordHash: "private"}}, nil
}

func (r *adminUserRepo) SetUserRole(_ context.Context, id string, role domain.Role, assigned bool) error {
	if id != targetUser || role != domain.Admin {
		return auth.ErrNotFound
	}
	r.changes = append(r.changes, assigned)
	return nil
}

type adminIdentity struct{}

func (adminIdentity) Authenticate(_ context.Context, token string) (domain.User, error) {
	switch token {
	case "admin":
		return domain.User{ID: "6b34edec-e72f-4c58-b545-7589ce3da931", Roles: []domain.Role{domain.Admin}}, nil
	case "organizer":
		return domain.User{Roles: []domain.Role{domain.Organizer}}, nil
	default:
		return domain.User{}, auth.ErrUnauthorized
	}
}

func TestAdminUserRoutes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := &adminUserRepo{}
	routes := chi.NewRouter()
	Auth(routes, handlers.NewAuth(auth.New(repo, nil, nil, nil, logger, nil, 0, 0), logger), middleware.RequireJWT(adminIdentity{}, logger))

	request := func(method, path, token string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res := httptest.NewRecorder()
		routes.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s as %q returned %d, want %d", method, path, token, res.Code, want)
		}
		return res
	}

	users := "/api/admin/users"
	role := users + "/" + targetUser + "/roles/ADMIN"
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, users}, {http.MethodPut, role}, {http.MethodDelete, role},
	} {
		request(route.method, route.path, "", http.StatusUnauthorized)
		request(route.method, route.path, "organizer", http.StatusForbidden)
	}
	res := request(http.MethodGet, users, "admin", http.StatusOK)
	var body []map[string]json.RawMessage
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || len(body) != 1 ||
		len(body[0]) != 4 || string(body[0]["id"]) != `"`+targetUser+`"` || string(body[0]["roles"]) != "[]" {
		t.Fatalf("unexpected user list: %s", res.Body)
	}
	request(http.MethodPut, users+"/invalid/roles/ADMIN", "admin", http.StatusBadRequest)
	request(http.MethodDelete, users+"/"+targetUser+"/roles/UNKNOWN", "admin", http.StatusBadRequest)
	request(http.MethodPut, role, "admin", http.StatusNoContent)
	request(http.MethodDelete, role, "admin", http.StatusNoContent)
	if len(repo.changes) != 2 || !repo.changes[0] || repo.changes[1] {
		t.Fatalf("unexpected role changes: %v", repo.changes)
	}
}
