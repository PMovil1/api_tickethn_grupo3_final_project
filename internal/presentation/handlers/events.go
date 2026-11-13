package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/middleware"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/response"
	"github.com/go-chi/chi/v5"
)

type Events struct {
	service *events.Service
	logger  *slog.Logger
}

func NewEvents(service *events.Service, logger *slog.Logger) *Events {
	return &Events{service: service, logger: logger}
}

type eventResponse struct {
	ID          string              `json:"id"`
	OrganizerID string              `json:"organizer_id,omitempty"`
	Title       string              `json:"title"`
	Description *string             `json:"description"`
	Address     *string             `json:"address"`
	Latitude    *float64            `json:"latitude"`
	Longitude   *float64            `json:"longitude"`
	StartsAt    *time.Time          `json:"starts_at"`
	CoverURL    *string             `json:"cover_image_url"`
	Status      string              `json:"status"`
	CreatedAt   time.Time           `json:"created_at"`
	TicketTypes *[]localityResponse `json:"ticket_types,omitempty"`
}

type localityResponse struct {
	ID          string  `json:"id"`
	EventID     string  `json:"event_id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Price       cents   `json:"price"`
	Capacity    *int    `json:"capacity,omitempty"`
	SoldCount   *int    `json:"sold_count,omitempty"`
	Active      bool    `json:"is_active"`
	Available   int     `json:"available"`
}

type orderResponse struct {
	ID        string           `json:"id"`
	EventID   string           `json:"event_id"`
	Total     cents            `json:"total"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	Tickets   []ticketResponse `json:"tickets,omitempty"`
}

type ticketResponse struct {
	ID           string     `json:"id"`
	EventID      string     `json:"event_id"`
	OrderID      string     `json:"order_id"`
	TicketTypeID string     `json:"ticket_type_id"`
	Code         string     `json:"code"`
	UnitPrice    cents      `json:"unit_price"`
	CreatedAt    time.Time  `json:"created_at"`
	UsedAt       *time.Time `json:"used_at"`
	QR           string     `json:"qr_payload,omitempty"`
}

func eventView(e domain.Event, public, includeTypes bool) eventResponse {
	result := eventResponse{ID: e.ID, Title: e.Title, Description: e.Description, Address: e.Address,
		Latitude: e.Latitude, Longitude: e.Longitude, StartsAt: e.StartsAt, CoverURL: e.CoverURL,
		Status: e.Status, CreatedAt: e.CreatedAt}
	if !public {
		result.OrganizerID = e.OrganizerID
	}
	if includeTypes {
		list := make([]localityResponse, 0, len(e.TicketTypes))
		for _, locality := range e.TicketTypes {
			list = append(list, localityView(locality, public))
		}
		result.TicketTypes = &list
	}
	return result
}

func localityView(t domain.TicketType, public bool) localityResponse {
	result := localityResponse{ID: t.ID, EventID: t.EventID, Title: t.Title, Description: t.Description,
		Price: cents(t.PriceCents), Active: t.Active, Available: t.Capacity - t.SoldCount}
	if !public {
		result.Capacity, result.SoldCount = &t.Capacity, &t.SoldCount
	}
	return result
}

func orderView(o domain.Order) orderResponse {
	return orderResponse{ID: o.ID, EventID: o.EventID, Total: cents(o.TotalCents), Status: o.Status,
		CreatedAt: o.CreatedAt, Tickets: ticketViews(o.Tickets)}
}

func ticketView(t domain.Ticket) ticketResponse {
	return ticketResponse{ID: t.ID, EventID: t.EventID, OrderID: t.OrderID, TicketTypeID: t.TicketTypeID,
		Code: t.Code, UnitPrice: cents(t.UnitPriceCents), CreatedAt: t.CreatedAt, UsedAt: t.UsedAt}
}

func ticketViews(tickets []domain.Ticket) []ticketResponse {
	out := make([]ticketResponse, 0, len(tickets))
	for _, t := range tickets {
		out = append(out, ticketView(t))
	}
	return out
}

func identityID(r *http.Request) string {
	user, _ := middleware.Identity(r.Context())
	return user.ID
}

func eventPage(r *http.Request) (events.Page, error) {
	page := events.Page{Limit: 20}
	query := r.URL.Query()
	if query.Has("limit") {
		n, err := strconv.Atoi(query.Get("limit"))
		if err != nil || n < 1 || n > 100 {
			return page, domain.ErrEventInput
		}
		page.Limit = n
	}
	if query.Has("offset") {
		n, err := strconv.ParseInt(query.Get("offset"), 10, 32)
		if err != nil || n < 0 {
			return page, domain.ErrEventInput
		}
		page.Offset = int(n)
	}
	return page, nil
}

func (h *Events) Create(w http.ResponseWriter, r *http.Request) {
	in, cover, err := multipartEvent(w, r, false)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	patch, err := in.patch()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var details domain.EventDetails
	patch(&details)
	e, err := h.service.Create(r.Context(), identityID(r), details, cover)
	h.reply(w, r, http.StatusCreated, eventView(e, false, false), err)
}

func (h *Events) ReplaceCover(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.validID(w, r, id) {
		return
	}
	_, cover, err := multipartEvent(w, r, true)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	e, err := h.service.ReplaceCover(r.Context(), identityID(r), id, *cover)
	h.reply(w, r, http.StatusOK, eventView(e, false, false), err)
}

func (h *Events) Edit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in eventInput
	if !h.validID(w, r, id) || !h.decode(w, r, &in) {
		return
	}
	patch, err := in.patch()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	e, err := h.service.Edit(r.Context(), identityID(r), id, patch)
	h.reply(w, r, http.StatusOK, eventView(e, false, false), err)
}

func (h *Events) Publish(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.validID(w, r, id) {
		return
	}
	e, err := h.service.Publish(r.Context(), identityID(r), id)
	h.reply(w, r, http.StatusOK, eventView(e, false, false), err)
}

func (h *Events) CreateTicketType(w http.ResponseWriter, r *http.Request) {
	h.changeType(w, r, true)
}

func (h *Events) EditTicketType(w http.ResponseWriter, r *http.Request) {
	h.changeType(w, r, false)
}

func (h *Events) changeType(w http.ResponseWriter, r *http.Request, create bool) {
	id, typeID := chi.URLParam(r, "id"), chi.URLParam(r, "ticketTypeId")
	var in localityInput
	if !h.validID(w, r, id) || (!create && !h.validID(w, r, typeID)) || !h.decode(w, r, &in) {
		return
	}
	patch, err := in.patch(create)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if create {
		details := domain.TicketTypeDetails{Active: true}
		patch(&details)
		t, err := h.service.CreateTicketType(r.Context(), identityID(r), id, details)
		h.reply(w, r, http.StatusCreated, localityView(t, false), err)
		return
	}
	t, err := h.service.EditTicketType(r.Context(), identityID(r), id, typeID, patch)
	h.reply(w, r, http.StatusOK, localityView(t, false), err)
}

func (h *Events) OwnedEvents(w http.ResponseWriter, r *http.Request) {
	h.eventList(w, r, false)
}

func (h *Events) PublicEvents(w http.ResponseWriter, r *http.Request) {
	h.eventList(w, r, true)
}

func (h *Events) eventList(w http.ResponseWriter, r *http.Request, public bool) {
	page, err := eventPage(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var list []domain.Event
	if public {
		list, err = h.service.PublicEvents(r.Context(), page)
	} else {
		list, err = h.service.OwnedEvents(r.Context(), identityID(r), page)
	}
	out := make([]eventResponse, 0, len(list))
	for _, e := range list {
		out = append(out, eventView(e, public, true))
	}
	h.reply(w, r, http.StatusOK, out, err)
}

func (h *Events) OwnedEvent(w http.ResponseWriter, r *http.Request) {
	h.eventDetail(w, r, false)
}

func (h *Events) PublicEvent(w http.ResponseWriter, r *http.Request) {
	h.eventDetail(w, r, true)
}

func (h *Events) eventDetail(w http.ResponseWriter, r *http.Request, public bool) {
	id := chi.URLParam(r, "id")
	if !h.validID(w, r, id) {
		return
	}
	var e domain.Event
	var err error
	if public {
		e, err = h.service.PublicEvent(r.Context(), id)
	} else {
		e, err = h.service.OwnedEvent(r.Context(), identityID(r), id)
	}
	h.reply(w, r, http.StatusOK, eventView(e, public, true), err)
}

func (h *Events) Buy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		EventID string `json:"event_id"`
		Items   []struct {
			TicketTypeID string `json:"ticket_type_id"`
			Quantity     int    `json:"quantity"`
		} `json:"items"`
	}
	if !h.decode(w, r, &in) {
		return
	}
	items := make([]domain.OrderItem, 0, len(in.Items))
	for _, i := range in.Items {
		items = append(items, domain.OrderItem{TicketTypeID: i.TicketTypeID, Quantity: i.Quantity})
	}
	o, err := h.service.Buy(r.Context(), identityID(r), in.EventID, items)
	h.reply(w, r, http.StatusCreated, orderView(o), err)
}

func (h *Events) Orders(w http.ResponseWriter, r *http.Request) {
	page, err := eventPage(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	orders, err := h.service.Orders(r.Context(), identityID(r), page)
	out := make([]orderResponse, 0, len(orders))
	for _, o := range orders {
		out = append(out, orderView(o))
	}
	h.reply(w, r, http.StatusOK, out, err)
}

func (h *Events) Tickets(w http.ResponseWriter, r *http.Request) {
	page, err := eventPage(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	tickets, err := h.service.Tickets(r.Context(), identityID(r), page)
	h.reply(w, r, http.StatusOK, ticketViews(tickets), err)
}

func (h *Events) Ticket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.validID(w, r, id) {
		return
	}
	t, qr, err := h.service.Ticket(r.Context(), identityID(r), id)
	out := ticketView(t)
	out.QR = qr
	h.reply(w, r, http.StatusOK, out, err)
}

func (h *Events) validID(w http.ResponseWriter, r *http.Request, id string) bool {
	if events.ValidID(id) {
		return true
	}
	h.fail(w, r, domain.ErrEventInput)
	return false
}

func (h *Events) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := readEventJSON(w, r, dst); err != nil {
		h.fail(w, r, err)
		return false
	}
	return true
}

func (h *Events) reply(w http.ResponseWriter, r *http.Request, status int, value any, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	response.JSON(w, status, value)
}

func (h *Events) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errLargeBody):
		response.Error(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", errLargeBody.Error())
	case errors.Is(err, errCoverFormat):
		response.Error(w, http.StatusUnsupportedMediaType, "INVALID_COVER", errCoverFormat.Error())
	case errors.Is(err, domain.ErrEventInput):
		response.Error(w, http.StatusBadRequest, "INVALID_INPUT", "Verifica los campos de la petición")
	case errors.Is(err, domain.ErrEventNotFound):
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "Recurso no encontrado")
	case errors.Is(err, domain.ErrEventConflict):
		response.Error(w, http.StatusConflict, "EVENT_CONFLICT", domain.ErrEventConflict.Error())
	case errors.Is(err, domain.ErrTicketStock):
		response.Error(w, http.StatusConflict, "TICKET_STOCK", domain.ErrTicketStock.Error())
	case errors.Is(err, events.ErrStorage):
		response.Error(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", events.ErrStorage.Error())
	case errors.Is(err, events.ErrQR), errors.Is(err, context.DeadlineExceeded):
		response.Error(w, http.StatusServiceUnavailable, "UNAVAILABLE", "Servicio temporalmente no disponible")
	default:
		h.logger.ErrorContext(r.Context(), "Event operation failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Error interno del servidor")
	}
}
