package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/infrastructure/storage"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/handlers"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/middleware"
	"github.com/go-chi/chi/v5"
)

type avatarAccounts struct {
	urls    map[string]*string
	saveErr error
}

func (a *avatarAccounts) AvatarURL(_ context.Context, id string) (*string, error) {
	return a.urls[id], nil
}

func (a *avatarAccounts) ReplaceAvatarURL(_ context.Context, id, url string) (*string, error) {
	previous := a.urls[id]
	if a.saveErr == nil {
		a.urls[id] = &url
	}
	return previous, a.saveErr
}

func (a *avatarAccounts) ConfirmAvatarURL(ctx context.Context, id string) (*string, error) {
	return a.AvatarURL(ctx, id)
}

type avatarIdentity struct{}

func (avatarIdentity) Authenticate(_ context.Context, token string) (domain.User, error) {
	switch token {
	case "alice", "bob":
		return domain.User{ID: token}, nil
	default:
		return domain.User{}, auth.ErrUnauthorized
	}
}

func TestAvatarRoutes(t *testing.T) {
	const bucket = "test-avatars"
	const storagePath = "/storage/v1/object/"
	var objectsMu sync.Mutex
	objects := make(map[string][]byte)
	supabase := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		objectsMu.Lock()
		defer objectsMu.Unlock()
		if r.Header.Get("Authorization") != "Bearer server-key" {
			t.Error("storage request did not use the backend credentials")
		}
		switch r.Method {
		case http.MethodPost:
			key, ok := strings.CutPrefix(r.URL.Path, storagePath+bucket+"/alice/")
			if !ok || r.Header.Get("Content-Type") != "image/png" {
				t.Errorf("unexpected upload: %v", r.URL)
			}
			objects["alice/"+key], _ = io.ReadAll(r.Body)
		case http.MethodGet:
			key, ok := strings.CutPrefix(r.URL.Path, storagePath+"authenticated/"+bucket+"/")
			if !ok || objects[key] == nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(objects[key])
		case http.MethodDelete:
			var body struct{ Prefixes []string }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.URL.Path != storagePath+bucket {
				t.Errorf("unexpected delete: %v %v", r.URL, err)
			}
			for _, key := range body.Prefixes {
				delete(objects, key)
			}
		}
	}))
	defer supabase.Close()
	objectCount := func() int {
		objectsMu.Lock()
		defer objectsMu.Unlock()
		return len(objects)
	}

	repo := &avatarAccounts{urls: make(map[string]*string)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	avatars := auth.NewAvatarService(repo, storage.NewAvatars(supabase.URL, bucket, "server-key"), logger)
	routes := chi.NewRouter()
	Auth(routes, handlers.NewAuth(nil, avatars, logger), middleware.RequireJWT(avatarIdentity{}, logger))
	request := func(method, token, mediaType string, body []byte, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/auth/me/avatar", bytes.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", mediaType)
		res := httptest.NewRecorder()
		routes.ServeHTTP(res, req)
		if res.Code != status {
			t.Fatalf("%s as %q: got %d, want %d. %s", method, token, res.Code, status, res.Body)
		}
		return res
	}

	request(http.MethodPut, "", "image/png", nil, http.StatusUnauthorized)
	request(http.MethodGet, "alice", "", nil, http.StatusNoContent)
	request(http.MethodPut, "alice", "image/png", []byte("invalid"), http.StatusBadRequest)
	request(http.MethodPut, "alice", "application/json", nil, http.StatusUnsupportedMediaType)
	request(http.MethodPut, "alice", "image/png", make([]byte, auth.MaxAvatarBytes+1), http.StatusRequestEntityTooLarge)

	first := avatarPNG(t, color.RGBA{R: 255, A: 255})
	second := avatarPNG(t, color.RGBA{B: 255, A: 255})
	request(http.MethodPut, "alice", "image/png", first, http.StatusNoContent)
	firstURL := *repo.urls["alice"]
	request(http.MethodGet, "bob", "", nil, http.StatusNoContent)

	repo.saveErr = errors.New("database write failed")
	request(http.MethodPut, "alice", "image/png", second, http.StatusInternalServerError)
	if *repo.urls["alice"] != firstURL || objectCount() != 1 {
		t.Fatal("failed write left an unused object or changed the selected avatar")
	}

	repo.saveErr = nil
	request(http.MethodPut, "alice", "image/png", second, http.StatusNoContent)
	result := request(http.MethodGet, "alice", "", nil, http.StatusOK)
	if result.Header().Get("Cache-Control") != "private, no-store" || !bytes.Equal(result.Body.Bytes(), second) {
		t.Fatal("GET did not return the current private photo")
	}
	if *repo.urls["alice"] == firstURL || objectCount() != 1 || !strings.Contains(*repo.urls["alice"], "/authenticated/") {
		t.Fatal("replacement did not store a private URL and remove the previous object")
	}
}

func avatarPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, c)
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
