package router

import (
	"net/http"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/handlers"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/middleware"
	"github.com/go-chi/chi/v5"
)

func Events(r chi.Router, h *handlers.Events, jwt func(http.Handler) http.Handler) {
	r.Get("/api/events", h.PublicEvents)
	r.Get("/api/events/{id}", h.PublicEvent)
	r.Group(func(organizer chi.Router) {
		organizer.Use(jwt, middleware.RequireRoles(domain.Organizer))
		organizer.Get("/api/organizer/events", h.OwnedEvents)
		organizer.Post("/api/organizer/events", h.Create)
		organizer.Get("/api/organizer/events/{id}", h.OwnedEvent)
		organizer.Patch("/api/organizer/events/{id}", h.Edit)
		organizer.Put("/api/organizer/events/{id}/cover", h.ReplaceCover)
		organizer.Post("/api/organizer/events/{id}/publish", h.Publish)
		organizer.Post("/api/organizer/events/{id}/ticket-types", h.CreateTicketType)
		organizer.Patch("/api/organizer/events/{id}/ticket-types/{ticketTypeId}", h.EditTicketType)
	})
	r.Group(func(buyer chi.Router) {
		buyer.Use(jwt, middleware.RequireRoles(domain.Buyer))
		buyer.Post("/api/orders", h.Buy)
		buyer.Get("/api/orders", h.Orders)
		buyer.Get("/api/tickets", h.Tickets)
		buyer.Get("/api/tickets/{id}", h.Ticket)
	})
}
