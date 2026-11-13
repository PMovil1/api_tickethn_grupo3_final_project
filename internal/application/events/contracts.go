package events

import (
	"context"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
)

type Page struct {
	Limit  int
	Offset int
}

type Repository interface {
	Create(context.Context, domain.Event) (domain.Event, error)
	ConfirmCover(context.Context, string, string) (bool, error)
	CoverReferenced(context.Context, string) (bool, error)
	OwnedEvents(context.Context, string, Page) ([]domain.Event, error)
	OwnedEvent(context.Context, string, string) (domain.Event, error)
	PublicEvents(context.Context, Page) ([]domain.Event, error)
	PublicEvent(context.Context, string) (domain.Event, error)
	WithEvent(context.Context, string, func(EventTransaction, domain.Event) error) error
	Orders(context.Context, string, Page) ([]domain.Order, error)
	Tickets(context.Context, string, Page) ([]domain.Ticket, error)
	Ticket(context.Context, string, string) (domain.Ticket, error)
}

// EventTransaction operates while holding the event row lock.
type EventTransaction interface {
	UpdateEvent(domain.Event) (domain.Event, error)
	SetCover(string) (domain.Event, error)
	HasTickets() (bool, error)
	HasOtherActiveType(string) (bool, error)
	TicketType(string) (domain.TicketType, error)
	CreateTicketType(domain.TicketTypeDetails) (domain.TicketType, error)
	UpdateTicketType(domain.TicketType) (domain.TicketType, error)
	Reserve(string, int) (int64, error)
	CreateOrder(string, int64) (domain.Order, error)
	IssueTickets(string, string, int64, int) ([]domain.Ticket, error)
}

type Cover struct {
	Bytes     []byte
	MediaType string
	Extension string
}

type CoverStore interface {
	Upload(context.Context, string, Cover) (string, error)
	Delete(context.Context, string) error
}

type QRSigner interface {
	Sign(ticketID, eventID string) string
}
