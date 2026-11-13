package database

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Minimal local fixture matching the event schema read from Supabase.
const eventFixture = `
CREATE TABLE public.users (id uuid PRIMARY KEY);
CREATE TABLE public.events (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	organizer_id uuid NOT NULL REFERENCES public.users(id),
	title text NOT NULL CHECK (btrim(title) <> ''),
	description text, address text, latitude numeric(9,6), longitude numeric(9,6),
	starts_at timestamptz, cover_image_url text, status text NOT NULL DEFAULT 'draft'
		CHECK (status IN ('draft','published','cancelled')),
	created_at timestamptz NOT NULL DEFAULT now(),
	CHECK ((latitude IS NULL AND longitude IS NULL) OR
		(latitude IS NOT NULL AND longitude IS NOT NULL AND latitude BETWEEN -90 AND 90 AND longitude BETWEEN -180 AND 180)),
	CHECK (status <> 'published' OR (NULLIF(btrim(description),'') IS NOT NULL AND
		NULLIF(btrim(address),'') IS NOT NULL AND starts_at IS NOT NULL))
);
CREATE TABLE public.ticket_types (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	event_id uuid NOT NULL REFERENCES public.events(id),
	title text NOT NULL CHECK (btrim(title) <> ''), description text,
	price numeric(12,2) NOT NULL CHECK (price >= 0),
	capacity integer NOT NULL CHECK (capacity > 0),
	sold_count integer NOT NULL DEFAULT 0 CHECK (sold_count BETWEEN 0 AND capacity),
	is_active boolean NOT NULL DEFAULT true,
	created_at timestamptz NOT NULL DEFAULT now(),
	UNIQUE (event_id,id)
);
CREATE TABLE public.orders (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	buyer_id uuid NOT NULL REFERENCES public.users(id),
	event_id uuid NOT NULL REFERENCES public.events(id),
	total numeric(14,2) NOT NULL CHECK (total >= 0),
	status text NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed','cancelled')),
	created_at timestamptz NOT NULL DEFAULT now(),
	UNIQUE (event_id,id)
);
CREATE SEQUENCE public.tickethn_ticket_code_seq;
CREATE TABLE public.tickets (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	event_id uuid NOT NULL,
	order_id uuid NOT NULL,
	ticket_type_id uuid NOT NULL,
	code text NOT NULL UNIQUE DEFAULT ('TKHN-' || nextval('public.tickethn_ticket_code_seq')),
	unit_price numeric(12,2) NOT NULL CHECK (unit_price >= 0),
	created_at timestamptz NOT NULL DEFAULT now(),
	used_at timestamptz,
	FOREIGN KEY (event_id,order_id) REFERENCES public.orders(event_id,id),
	FOREIGN KEY (event_id,ticket_type_id) REFERENCES public.ticket_types(event_id,id)
);
`

func TestEventsPostgres(t *testing.T) {
	dsn := os.Getenv("TICKETHN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TICKETHN_TEST_DATABASE_URL to a disposable local database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "tickethn_events_test" || len(cfg.ConnConfig.Fallbacks) != 0 {
		t.Fatal("expected a disposable database named tickethn_events_test at 127.0.0.1 with sslmode=disable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, eventFixture)
		return err
	})
	if err != nil {
		t.Fatalf("could not initialize the disposable database: %v", err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := pool.Exec(cleanup, `DROP TABLE public.tickets, public.orders, public.ticket_types, public.events, public.users;
			DROP SEQUENCE public.tickethn_ticket_code_seq`); err != nil {
			t.Errorf("clean up test fixture: %v", err)
		}
	}()
	const (
		owner     = "80000000-0000-4000-8000-000000000001"
		buyer     = "80000000-0000-4000-8000-000000000002"
		another   = "80000000-0000-4000-8000-000000000003"
		firstType = "10000000-0000-4000-8000-000000000001"
		lastType  = "90000000-0000-4000-8000-000000000001"
	)
	if _, err := pool.Exec(ctx, `INSERT INTO public.users VALUES ($1),($2),($3)`, owner, buyer, another); err != nil {
		t.Fatal(err)
	}
	repo := NewEvents(pool)
	service := events.New(repo, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	when := time.Now().Add(time.Hour)
	description, address := "Música en vivo", "Tegucigalpa"
	e, err := service.Create(ctx, owner, domain.EventDetails{Title: "Concierto", Description: &description, Address: &address, StartsAt: &when}, nil)
	if err != nil {
		t.Fatal(err)
	}
	page := events.Page{Limit: 20}
	if list, err := service.PublicEvents(ctx, page); err != nil || len(list) != 0 {
		t.Fatalf("draft in public list: %+v, %v", list, err)
	}
	if _, err := service.PublicEvent(ctx, e.ID); !errors.Is(err, domain.ErrEventNotFound) {
		t.Fatalf("public draft detail: %v", err)
	}
	lat, lon := 14.123456, -87.654321
	if _, err := service.Edit(ctx, owner, e.ID, func(d *domain.EventDetails) { d.Latitude, d.Longitude = &lat, &lon }); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(ctx, owner, e.ID); !errors.Is(err, domain.ErrEventConflict) {
		t.Fatalf("publication without localities: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO public.ticket_types (id,event_id,title,price,capacity)
		VALUES ($1,$3,'General',12.31,2),($2,$3,'VIP',37.29,1)`, firstType, lastType, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(ctx, owner, e.ID); err != nil {
		t.Fatal(err)
	}
	list, err := service.PublicEvents(ctx, page)
	if err != nil || len(list) != 1 || len(list[0].TicketTypes) != 2 {
		t.Fatalf("published list: %+v, %v", list, err)
	}
	if _, err := service.EditTicketType(ctx, owner, e.ID, firstType, func(d *domain.TicketTypeDetails) { d.Active = false }); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EditTicketType(ctx, owner, e.ID, lastType, func(d *domain.TicketTypeDetails) { d.Active = false }); !errors.Is(err, domain.ErrEventConflict) {
		t.Fatalf("last active type could be deactivated: %v", err)
	}
	if _, err := service.EditTicketType(ctx, owner, e.ID, firstType, func(d *domain.TicketTypeDetails) { d.Active = true }); err != nil {
		t.Fatal(err)
	}

	t.Run("only one simultaneous purchase of last ticket", func(t *testing.T) {
		type result struct {
			order domain.Order
			err   error
		}
		start := make(chan struct{})
		results := make(chan result, 2)
		for range 2 {
			go func() {
				<-start
				order, err := service.Buy(ctx, buyer, e.ID, []domain.OrderItem{{TicketTypeID: lastType, Quantity: 1}})
				results <- result{order, err}
			}()
		}
		close(start)
		var succeeded, outOfStock int
		for range 2 {
			got := <-results
			switch {
			case got.err == nil:
				succeeded++
				if got.order.TotalCents != 3729 || got.order.Status != "confirmed" || len(got.order.Tickets) != 1 {
					t.Fatalf("unexpected order: %+v", got.order)
				}
				ticket := got.order.Tickets[0]
				if ticket.UnitPriceCents != 3729 || ticket.Code == "" {
					t.Fatalf("unexpected ticket: %+v", ticket)
				}
				if _, err := repo.Ticket(ctx, buyer, ticket.ID); err != nil {
					t.Fatalf("own ticket: %v", err)
				}
				if _, err := repo.Ticket(ctx, another, ticket.ID); !errors.Is(err, domain.ErrEventNotFound) {
					t.Fatalf("foreign ticket: %v", err)
				}
			case errors.Is(got.err, domain.ErrTicketStock):
				outOfStock++
			default:
				t.Fatalf("purchase failed unexpectedly: %v", got.err)
			}
		}
		if succeeded != 1 || outOfStock != 1 {
			t.Fatalf("succeeded=%d out-of-stock=%d", succeeded, outOfStock)
		}
	})
	t.Run("out-of-stock second item rolls back first reservation", func(t *testing.T) {
		_, err := service.Buy(ctx, another, e.ID, []domain.OrderItem{{TicketTypeID: lastType, Quantity: 1}, {TicketTypeID: firstType, Quantity: 2}})
		if !errors.Is(err, domain.ErrTicketStock) {
			t.Fatalf("purchase result: %v", err)
		}
		var firstSold, lastSold int
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT sold_count FROM public.ticket_types WHERE id=$1),
			(SELECT sold_count FROM public.ticket_types WHERE id=$2)`, firstType, lastType).Scan(&firstSold, &lastSold); err != nil {
			t.Fatal(err)
		}
		if firstSold != 0 || lastSold != 1 {
			t.Fatalf("unexpected sold counts: %d, %d", firstSold, lastSold)
		}
		for _, account := range []struct {
			id    string
			count int
		}{{buyer, 1}, {another, 0}} {
			orders, err := repo.Orders(ctx, account.id, page)
			if err != nil || len(orders) != account.count {
				t.Fatalf("orders: %+v, %v", orders, err)
			}
			tickets, err := repo.Tickets(ctx, account.id, page)
			if err != nil || len(tickets) != account.count {
				t.Fatalf("tickets: %+v, %v", tickets, err)
			}
		}
	})
	if _, err := service.Edit(ctx, owner, e.ID, func(d *domain.EventDetails) { next := when.Add(time.Hour); d.StartsAt = &next }); !errors.Is(err, domain.ErrEventConflict) {
		t.Fatalf("event reschedule after purchase: %v", err)
	}
}
