package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/health"
)

type Health struct {
	check  *health.Check
	logger *slog.Logger
}

type healthResponse struct {
	Status   string `json:"status"`
	Service  string `json:"service"`
	Database string `json:"database"`
}

func NewHealth(check *health.Check, logger *slog.Logger) *Health {
	return &Health{check: check, logger: logger}
}

func (h *Health) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	response := healthResponse{Status: "healthy", Service: "TicketHN API", Database: "connected"}
	if err := h.check.Execute(r.Context()); err != nil {
		h.logger.WarnContext(r.Context(), "PostgreSQL health check failed", "error_type", fmt.Sprintf("%T", err))
		status = http.StatusServiceUnavailable
		response.Status = "unhealthy"
		response.Database = "disconnected"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.ErrorContext(r.Context(), "Could not write health check response", "error_type", fmt.Sprintf("%T", err))
	}
}
