package events

import (
	"context"
	"time"
)

// PurchaseEmail is the set of order details needed to render the confirmation.
type PurchaseEmail struct {
	BuyerName  string
	Recipient  string
	EventID    string
	EventTitle string
	Address    string
	StartsAt   *time.Time
	Tickets    []PurchaseTicket
}

// PurchaseTicket identifies an issued ticket and its admission class.
type PurchaseTicket struct {
	ID       string
	Code     string
	Locality string
}

// PurchaseEmailJob is a leased delivery. Attempt identifies the current lease.
type PurchaseEmailJob struct {
	OrderID        string
	Attempt        int
	RequestBody    []byte
	FirstAttemptAt *time.Time
}

// PurchaseEmailStore manages durable delivery and its prepared Resend request.
type PurchaseEmailStore interface {
	ClaimPurchaseEmail(context.Context) (PurchaseEmailJob, bool, error)
	PurchaseEmail(ctx context.Context, orderID string) (PurchaseEmail, error)
	SavePurchaseEmailRequest(ctx context.Context, job PurchaseEmailJob, body []byte) error
	RetryPurchaseEmail(ctx context.Context, job PurchaseEmailJob, delay time.Duration) error
	FinishPurchaseEmail(ctx context.Context, job PurchaseEmailJob, sent bool) error
}
