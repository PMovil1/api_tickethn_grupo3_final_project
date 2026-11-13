package storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
)

func TestCoverStorageRequests(t *testing.T) {
	const object = "80000000-0000-4000-8000-000000000001.png"
	seen := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer backend-key" || r.Header.Get("apikey") != "backend-key" {
			t.Error("missing server credentials")
		}
		switch r.Method {
		case http.MethodPost:
			if r.URL.Path != "/storage/v1/object/event-covers/"+object ||
				r.Header.Get("x-upsert") != "false" || r.Header.Get("Content-Type") != "image/png" {
				t.Errorf("unexpected upload: %s %v", r.URL, r.Header)
			}
		case http.MethodDelete:
			var body struct{ Prefixes []string }
			err := json.NewDecoder(r.Body).Decode(&body)
			if err != nil || len(body.Prefixes) != 1 || body.Prefixes[0] != object ||
				r.URL.Path != "/storage/v1/object/event-covers" {
				t.Errorf("unexpected delete: %s %+v %v", r.URL, body, err)
			}
		default:
			t.Errorf("unexpected method: %s", r.Method)
		}
		seen <- r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	store := NewCovers(server.URL, "event-covers", "backend-key")
	ctx := context.Background()
	url, err := store.Upload(ctx, object, events.Cover{MediaType: "image/png", Bytes: []byte("validated image")})
	if err != nil || url != server.URL+"/storage/v1/object/public/event-covers/"+object {
		t.Fatalf("upload: %q %v", url, err)
	}
	if err := store.Delete(ctx, url); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("expected one upload and one deletion, got %d requests", len(seen))
	}
}
