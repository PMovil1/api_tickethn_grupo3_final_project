package database

import (
	"context"
	"errors"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Events struct {
	pool *pgxpool.Pool
}

func NewEvents(pool *pgxpool.Pool) *Events { return &Events{pool: pool} }

const eventColumns = `id::text, organizer_id::text, title, description, address,
	latitude::float8, longitude::float8, starts_at, cover_image_url, status, created_at`
const typeColumns = `id::text, event_id::text, title, description,
	(price * 100)::bigint, capacity, sold_count, is_active`
const orderColumns = `id::text, event_id::text, (total * 100)::bigint, status, created_at`
const ticketColumns = `id::text, event_id::text, order_id::text, ticket_type_id::text,
	code, (unit_price * 100)::bigint, created_at, used_at`

type rowScanner interface{ Scan(...any) error }

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrEventNotFound
	}
	return err
}

func scanEvent(row rowScanner) (domain.Event, error) {
	var e domain.Event
	err := row.Scan(&e.ID, &e.OrganizerID, &e.Title, &e.Description, &e.Address,
		&e.Latitude, &e.Longitude, &e.StartsAt, &e.CoverURL, &e.Status, &e.CreatedAt)
	return e, notFound(err)
}

func scanType(row rowScanner) (domain.TicketType, error) {
	var t domain.TicketType
	err := row.Scan(&t.ID, &t.EventID, &t.Title, &t.Description, &t.PriceCents, &t.Capacity, &t.SoldCount, &t.Active)
	return t, notFound(err)
}

func scanOrder(row rowScanner) (domain.Order, error) {
	var o domain.Order
	err := row.Scan(&o.ID, &o.EventID, &o.TotalCents, &o.Status, &o.CreatedAt)
	return o, notFound(err)
}

func scanTicket(row rowScanner) (domain.Ticket, error) {
	var t domain.Ticket
	err := row.Scan(&t.ID, &t.EventID, &t.OrderID, &t.TicketTypeID, &t.Code, &t.UnitPriceCents, &t.CreatedAt, &t.UsedAt)
	return t, notFound(err)
}

func collect[T any](rows pgx.Rows, err error, scan func(rowScanner) (T, error)) ([]T, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (T, error) { return scan(row) })
}

// The advisory lock lets confirmation wait for a possibly uncommitted INSERT,
// including when the ID is still invisible to SELECT FOR UPDATE.
func lockEventInsert(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tickethn:event:' || ($1::uuid)::text, 0))`, id)
	return err
}

func (r *Events) Create(ctx context.Context, e domain.Event) (domain.Event, error) {
	var saved domain.Event
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		if err := lockEventInsert(ctx, tx, e.ID); err != nil {
			return err
		}
		var err error
		saved, err = scanEvent(tx.QueryRow(ctx, `INSERT INTO public.events
			(id, organizer_id, title, description, address, latitude, longitude, starts_at, cover_image_url)
			VALUES ($1,$2,$3,$4,$5,$6::float8,$7::float8,$8,$9) RETURNING `+eventColumns,
			e.ID, e.OrganizerID, e.Title, e.Description, e.Address, e.Latitude, e.Longitude, e.StartsAt, e.CoverURL))
		return err
	})
	return saved, err
}

func (r *Events) ConfirmCover(ctx context.Context, id, url string) (bool, error) {
	var saved bool
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		if err := lockEventInsert(ctx, tx, id); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT coalesce(cover_image_url = $2,false)
			FROM public.events WHERE id=$1 FOR UPDATE`, id, url).Scan(&saved)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	return saved, err
}

func (r *Events) CoverReferenced(ctx context.Context, url string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.events WHERE cover_image_url=$1)`, url).Scan(&exists)
	return exists, err
}

const upcoming = `status='published' AND starts_at > statement_timestamp()`

func (r *Events) OwnedEvents(ctx context.Context, owner string, page events.Page) ([]domain.Event, error) {
	return r.readEvents(ctx, false, `WHERE organizer_id=$1 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		owner, page.Limit, page.Offset)
}

func (r *Events) OwnedEvent(ctx context.Context, owner, id string) (domain.Event, error) {
	return singleEvent(r.readEvents(ctx, false, `WHERE id=$1 AND organizer_id=$2`, id, owner))
}

func (r *Events) PublicEvents(ctx context.Context, page events.Page) ([]domain.Event, error) {
	return r.readEvents(ctx, true, `WHERE `+upcoming+` ORDER BY starts_at, id LIMIT $1 OFFSET $2`, page.Limit, page.Offset)
}

func (r *Events) PublicEvent(ctx context.Context, id string) (domain.Event, error) {
	return singleEvent(r.readEvents(ctx, true, `WHERE id=$1 AND `+upcoming, id))
}

func singleEvent(list []domain.Event, err error) (domain.Event, error) {
	if err != nil {
		return domain.Event{}, err
	}
	if len(list) == 0 {
		return domain.Event{}, domain.ErrEventNotFound
	}
	return list[0], nil
}

func (r *Events) readEvents(ctx context.Context, public bool, filter string, args ...any) ([]domain.Event, error) {
	var list []domain.Event
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, queryErr := tx.Query(ctx, `SELECT `+eventColumns+` FROM public.events `+filter, args...)
		var err error
		list, err = collect(rows, queryErr, scanEvent)
		if err != nil || len(list) == 0 {
			return err
		}
		ids := make([]string, len(list))
		index := make(map[string]int, len(list))
		for i, event := range list {
			ids[i] = event.ID
			index[event.ID] = i
		}
		query := `SELECT ` + typeColumns + ` FROM public.ticket_types WHERE event_id=ANY($1::text[]::uuid[])`
		if public {
			query += ` AND is_active`
		}
		rows, err = tx.Query(ctx, query+` ORDER BY created_at, id`, ids)
		types, err := collect(rows, err, scanType)
		if err != nil {
			return err
		}
		for _, locality := range types {
			i := index[locality.EventID]
			list[i].TicketTypes = append(list[i].TicketTypes, locality)
		}
		return nil
	})
	return list, err
}

func (r *Events) WithEvent(ctx context.Context, id string, action func(events.EventTransaction, domain.Event) error) error {
	return pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		event, err := scanEvent(tx.QueryRow(ctx, `SELECT `+eventColumns+` FROM public.events WHERE id=$1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		return action(eventTx{ctx: ctx, tx: tx, id: id}, event)
	})
}

type eventTx struct {
	ctx context.Context
	tx  pgx.Tx
	id  string
}

func (t eventTx) UpdateEvent(e domain.Event) (domain.Event, error) {
	return scanEvent(t.tx.QueryRow(t.ctx, `UPDATE public.events SET title=$2, description=$3, address=$4,
		latitude=$5::float8, longitude=$6::float8, starts_at=$7, status=$8 WHERE id=$1 RETURNING `+eventColumns,
		t.id, e.Title, e.Description, e.Address, e.Latitude, e.Longitude, e.StartsAt, e.Status))
}

func (t eventTx) SetCover(url string) (domain.Event, error) {
	return scanEvent(t.tx.QueryRow(t.ctx, `UPDATE public.events SET cover_image_url=$2 WHERE id=$1 RETURNING `+eventColumns, t.id, url))
}

func (t eventTx) HasTickets() (bool, error) {
	var exists bool
	err := t.tx.QueryRow(t.ctx, `SELECT EXISTS(SELECT 1 FROM public.tickets WHERE event_id=$1)`, t.id).Scan(&exists)
	return exists, err
}

func (t eventTx) HasOtherActiveType(except string) (bool, error) {
	var exists bool
	err := t.tx.QueryRow(t.ctx, `SELECT EXISTS(SELECT 1 FROM public.ticket_types
		WHERE event_id=$1 AND is_active AND (NULLIF($2,'') IS NULL OR id<>NULLIF($2,'')::uuid))`, t.id, except).Scan(&exists)
	return exists, err
}

func (t eventTx) TicketType(id string) (domain.TicketType, error) {
	return scanType(t.tx.QueryRow(t.ctx, `SELECT `+typeColumns+` FROM public.ticket_types WHERE event_id=$1 AND id=$2`, t.id, id))
}

func (t eventTx) CreateTicketType(d domain.TicketTypeDetails) (domain.TicketType, error) {
	return scanType(t.tx.QueryRow(t.ctx, `INSERT INTO public.ticket_types (event_id,title,description,price,capacity,is_active)
		VALUES ($1,$2,$3,$4::bigint / 100.0,$5,$6) RETURNING `+typeColumns,
		t.id, d.Title, d.Description, d.PriceCents, d.Capacity, d.Active))
}

func (t eventTx) UpdateTicketType(d domain.TicketType) (domain.TicketType, error) {
	return scanType(t.tx.QueryRow(t.ctx, `UPDATE public.ticket_types SET title=$3,description=$4,price=$5::bigint / 100.0,
		capacity=$6,is_active=$7 WHERE event_id=$1 AND id=$2 RETURNING `+typeColumns,
		t.id, d.ID, d.Title, d.Description, d.PriceCents, d.Capacity, d.Active))
}

func (t eventTx) Reserve(id string, quantity int) (int64, error) {
	var price int64
	err := t.tx.QueryRow(t.ctx, `UPDATE public.ticket_types SET sold_count=sold_count+$3
		WHERE event_id=$1 AND id=$2 AND is_active AND capacity-sold_count >= $3
		RETURNING (price * 100)::bigint`, t.id, id, quantity).Scan(&price)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrTicketStock
	}
	return price, err
}

func (t eventTx) CreateOrder(buyer string, total int64) (domain.Order, error) {
	return scanOrder(t.tx.QueryRow(t.ctx, `INSERT INTO public.orders (buyer_id,event_id,total)
		VALUES ($1,$2,$3::bigint / 100.0) RETURNING `+orderColumns, buyer, t.id, total))
}

func (t eventTx) IssueTickets(order, locality string, price int64, quantity int) ([]domain.Ticket, error) {
	rows, err := t.tx.Query(t.ctx, `INSERT INTO public.tickets (event_id,order_id,ticket_type_id,unit_price)
		SELECT $1::uuid,$2::uuid,$3::uuid,$4::bigint / 100.0 FROM generate_series(1,$5::int)
		RETURNING `+ticketColumns, t.id, order, locality, price, quantity)
	return collect(rows, err, scanTicket)
}

func (r *Events) Orders(ctx context.Context, buyer string, page events.Page) ([]domain.Order, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+orderColumns+` FROM public.orders
		WHERE buyer_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, buyer, page.Limit, page.Offset)
	return collect(rows, err, scanOrder)
}

func (r *Events) Tickets(ctx context.Context, buyer string, page events.Page) ([]domain.Ticket, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+ticketColumns+` FROM public.tickets
		WHERE order_id IN (SELECT id FROM public.orders WHERE buyer_id=$1)
		ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, buyer, page.Limit, page.Offset)
	return collect(rows, err, scanTicket)
}

func (r *Events) Ticket(ctx context.Context, buyer, id string) (domain.Ticket, error) {
	return scanTicket(r.pool.QueryRow(ctx, `SELECT `+ticketColumns+` FROM public.tickets
		WHERE id=$1 AND order_id IN (SELECT id FROM public.orders WHERE buyer_id=$2)`, id, buyer))
}
