package database

import (
	"context"
	"errors"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (t eventTx) QueuePurchaseEmail(orderID string) error {
	_, err := t.tx.Exec(t.ctx, `INSERT INTO tickethn_internal.purchase_emails (order_id) VALUES ($1)`, orderID)
	return err
}

// ClaimPurchaseEmail leases one pending order for up to two minutes.
func (r *Events) ClaimPurchaseEmail(ctx context.Context) (events.PurchaseEmailJob, bool, error) {
	var job events.PurchaseEmailJob
	err := r.pool.QueryRow(ctx, `WITH ready AS (
		SELECT order_id FROM tickethn_internal.purchase_emails
		WHERE sent_at IS NULL AND failed_at IS NULL AND available_at <= statement_timestamp()
		ORDER BY available_at, order_id LIMIT 1 FOR UPDATE SKIP LOCKED
	)
	UPDATE tickethn_internal.purchase_emails AS mail
	SET attempts = mail.attempts + 1, available_at = clock_timestamp() + interval '2 minutes'
	FROM ready WHERE mail.order_id = ready.order_id
	RETURNING mail.order_id::text, mail.attempts, mail.request_body, mail.first_attempt_at`).
		Scan(&job.OrderID, &job.Attempt, &job.RequestBody, &job.FirstAttemptAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return job, false, nil
	}
	return job, err == nil, err
}

// PurchaseEmail reads the authenticated buyer and issued tickets for an order.
func (r *Events) PurchaseEmail(ctx context.Context, orderID string) (events.PurchaseEmail, error) {
	var mail events.PurchaseEmail
	rows, err := r.pool.Query(ctx, `SELECT u.full_name, u.email, e.id::text, e.title, coalesce(e.address, ''), e.starts_at,
		t.id::text, t.code, tt.title
		FROM public.orders AS o
		JOIN public.users AS u ON u.id = o.buyer_id
		JOIN public.events AS e ON e.id = o.event_id
		JOIN public.tickets AS t ON t.order_id = o.id AND t.event_id = o.event_id
		JOIN public.ticket_types AS tt ON tt.id = t.ticket_type_id AND tt.event_id = t.event_id
		WHERE o.id = $1 ORDER BY t.code`, orderID)
	if err != nil {
		return mail, err
	}
	defer rows.Close()
	for rows.Next() {
		var ticket events.PurchaseTicket
		if err := rows.Scan(&mail.BuyerName, &mail.Recipient, &mail.EventID, &mail.EventTitle, &mail.Address, &mail.StartsAt,
			&ticket.ID, &ticket.Code, &ticket.Locality); err != nil {
			return mail, err
		}
		mail.Tickets = append(mail.Tickets, ticket)
	}
	if err := rows.Err(); err != nil {
		return mail, err
	}
	if len(mail.Tickets) == 0 {
		return mail, domain.ErrEventNotFound
	}
	return mail, nil
}

// SavePurchaseEmailRequest stores the exact payload before the first attempt at Resend.
func (r *Events) SavePurchaseEmailRequest(ctx context.Context, job events.PurchaseEmailJob, body []byte) error {
	return checkMailLease(r.pool.Exec(ctx, `UPDATE tickethn_internal.purchase_emails
		SET request_body = $3, first_attempt_at = clock_timestamp()
		WHERE order_id = $1 AND attempts = $2 AND available_at > clock_timestamp() AND request_body IS NULL
		AND sent_at IS NULL AND failed_at IS NULL`, job.OrderID, job.Attempt, body))
}

// RetryPurchaseEmail releases the lease and schedules another attempt.
func (r *Events) RetryPurchaseEmail(ctx context.Context, job events.PurchaseEmailJob, delay time.Duration) error {
	return checkMailLease(r.pool.Exec(ctx, `UPDATE tickethn_internal.purchase_emails
		SET available_at = clock_timestamp() + $3::bigint * interval '1 second'
		WHERE order_id = $1 AND attempts = $2 AND available_at > clock_timestamp()
		AND sent_at IS NULL AND failed_at IS NULL`, job.OrderID, job.Attempt, int64(delay/time.Second)))
}

// FinishPurchaseEmail records the result and discards the prepared attachment.
func (r *Events) FinishPurchaseEmail(ctx context.Context, job events.PurchaseEmailJob, sent bool) error {
	return checkMailLease(r.pool.Exec(ctx, `UPDATE tickethn_internal.purchase_emails
		SET sent_at = CASE WHEN $3::boolean THEN clock_timestamp() END,
			failed_at = CASE WHEN NOT $3::boolean THEN clock_timestamp() END, request_body = NULL
		WHERE order_id = $1 AND attempts = $2 AND available_at > clock_timestamp()
		AND sent_at IS NULL AND failed_at IS NULL`, job.OrderID, job.Attempt, sent))
}

func checkMailLease(tag pgconn.CommandTag, err error) error {
	if err == nil && tag.RowsAffected() != 1 {
		return errors.New("purchase email lease is no longer current")
	}
	return err
}
