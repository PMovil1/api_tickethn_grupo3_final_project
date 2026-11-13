package domain

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrEventNotFound = errors.New("recurso no encontrado")
	ErrEventInput    = errors.New("datos inválidos")
	ErrEventConflict = errors.New("el estado actual impide la operación")
	ErrTicketStock   = errors.New("cupo insuficiente o localidad inactiva")
)

// Event represents an organizer's draft or listing.
type Event struct {
	ID          string
	OrganizerID string
	EventDetails
	CoverURL    *string
	Status      string
	CreatedAt   time.Time
	TicketTypes []TicketType
}

const (
	Draft              = "draft"
	Published          = "published"
	MaxTicketsPerOrder = 20
)

type EventDetails struct {
	Title       string
	Description *string
	Address     *string
	Latitude    *float64
	Longitude   *float64
	StartsAt    *time.Time
}

func (d *EventDetails) Validate() error {
	d.Title = strings.TrimSpace(d.Title)
	if d.Title == "" || !validText(d.Title) || !validOptionalText(d.Description) ||
		!validOptionalText(d.Address) || (d.Latitude == nil) != (d.Longitude == nil) {
		return ErrEventInput
	}
	if d.Latitude != nil {
		if !coordinate(*d.Latitude, 90) || !coordinate(*d.Longitude, 180) {
			return ErrEventInput
		}
		lat := math.Round(*d.Latitude*1e6) / 1e6
		lon := math.Round(*d.Longitude*1e6) / 1e6
		d.Latitude, d.Longitude = &lat, &lon
	}
	if d.StartsAt != nil {
		date := d.StartsAt.UTC().Round(time.Microsecond)
		d.StartsAt = &date
	}
	return nil
}

func coordinate(value, max float64) bool {
	return !math.IsNaN(value) && math.Abs(value) <= max
}

func validText(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validOptionalText(value *string) bool {
	return value == nil || validText(*value)
}

func (d EventDetails) Ready() bool {
	return d.Description != nil && strings.TrimSpace(*d.Description) != "" &&
		d.Address != nil && strings.TrimSpace(*d.Address) != "" && d.StartsAt != nil
}

func (d EventDetails) ChangesScheduleOrLocation(next EventDetails) bool {
	return !same(d.Address, next.Address) || !same(d.Latitude, next.Latitude) ||
		!same(d.Longitude, next.Longitude) ||
		(d.StartsAt == nil) != (next.StartsAt == nil) ||
		(d.StartsAt != nil && !d.StartsAt.Equal(*next.StartsAt))
}

func same[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

type TicketType struct {
	ID      string
	EventID string
	TicketTypeDetails
	SoldCount int
}

type TicketTypeDetails struct {
	Title       string
	Description *string
	PriceCents  int64
	Capacity    int
	Active      bool
}

func (d *TicketTypeDetails) Validate() error {
	d.Title = strings.TrimSpace(d.Title)
	if d.Title == "" || !validText(d.Title) || !validOptionalText(d.Description) ||
		d.PriceCents < 0 || d.PriceCents > 999999999999 ||
		d.Capacity < 1 || d.Capacity > math.MaxInt32 {
		return ErrEventInput
	}
	return nil
}

type OrderItem struct {
	TicketTypeID string
	Quantity     int
}

type Order struct {
	ID         string
	EventID    string
	TotalCents int64
	Status     string
	CreatedAt  time.Time
	Tickets    []Ticket
}

type Ticket struct {
	ID             string
	EventID        string
	OrderID        string
	TicketTypeID   string
	Code           string
	UnitPriceCents int64
	CreatedAt      time.Time
	UsedAt         *time.Time
}
