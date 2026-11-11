package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/health"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/handlers"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/router"
)

type pingFunc func(context.Context) error

func (ping pingFunc) Ping(ctx context.Context) error {
	return ping(ctx)
}

func TestHealth(t *testing.T) {
	var pingErr error
	var calls int
	check := health.NewCheck(pingFunc(func(context.Context) error {
		calls++
		return pingErr
	}), time.Second)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := router.New(handlers.NewHealth(check, logger))

	assertHealth(t, api, http.StatusOK, "healthy", "connected")
	pingErr = errors.New("private database details")
	assertHealth(t, api, http.StatusServiceUnavailable, "unhealthy", "disconnected")
	pingErr = nil
	assertHealth(t, api, http.StatusOK, "healthy", "connected")
	if calls != 3 {
		t.Errorf("Ping called %d times, want 3", calls)
	}
}

func TestHealthPingTimeout(t *testing.T) {
	check := health.NewCheck(pingFunc(func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("Ping context has no deadline")
		}
		<-ctx.Done()
		if err := ctx.Err(); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Ping context error = %v, want DeadlineExceeded", err)
		}
		return ctx.Err()
	}), 10*time.Millisecond)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := router.New(handlers.NewHealth(check, logger))

	assertHealth(t, api, http.StatusServiceUnavailable, "unhealthy", "disconnected")
}

func assertHealth(t *testing.T, api http.Handler, statusCode int, status, database string) {
	t.Helper()
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if response.Code != statusCode {
		t.Errorf("HTTP status = %d, want %d", response.Code, statusCode)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("could not decode health response: %v", err)
	}
	want := map[string]string{"status": status, "service": "TicketHN API", "database": database}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("response = %v, want %v", body, want)
	}
}
