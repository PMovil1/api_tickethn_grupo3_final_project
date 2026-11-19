package email

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/infrastructure/security"
)

func TestPurchaseEmailRetriesWithSameAttachment(t *testing.T) {
	const orderID = "80000000-0000-4000-8000-000000000001"
	starts := time.Date(2026, 11, 29, 0, 30, 0, 0, time.UTC)
	store := &purchaseTestStore{
		job: events.PurchaseEmailJob{OrderID: orderID},
		purchase: events.PurchaseEmail{
			BuyerName: "María López", Recipient: "maria@example.test",
			EventID: "80000000-0000-4000-8000-000000000002", EventTitle: "Música en vivo & café",
			Address: "Teatro Nacional Manuel Bonilla, Tegucigalpa", StartsAt: &starts,
			Tickets: []events.PurchaseTicket{
				{ID: "80000000-0000-4000-8000-000000000003", Code: "TKHN-1001", Locality: "Platea central"},
				{ID: "80000000-0000-4000-8000-000000000004", Code: "TKHN-1002", Locality: "Balcón preferencial"},
			},
		},
	}
	resend := NewResend("test-key", "TicketHN <noreply@example.test>")
	signer := security.NewTicketQR([]byte("test-ticket-signing-key-for-purchase"))
	var requests int
	resend.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, store.job.RequestBody) ||
			r.Header.Get("Idempotency-Key") != "purchase-confirmation/"+orderID {
			t.Fatal("email was sent without a saved, idempotent request")
		}
		status := http.StatusServiceUnavailable
		if requests == 2 {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	worker := purchaseWorker{store: store, resend: resend, signer: signer, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if found, err := worker.deliverNext(t.Context()); err != nil || !found || store.retryAfter != time.Minute || store.finished {
		t.Fatalf("first attempt: found=%t retry=%v finished=%t err=%v", found, store.retryAfter, store.finished, err)
	}
	request := store.job.RequestBody
	if found, err := worker.deliverNext(t.Context()); err != nil || !found || !store.finished || !store.sent {
		t.Fatalf("retry: found=%t sent=%t finished=%t err=%v", found, store.sent, store.finished, err)
	}
	if requests != 2 || store.reads != 1 {
		t.Fatalf("requests=%d reads=%d", requests, store.reads)
	}

	var payload messagePayload
	if err := json.Unmarshal(request, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.To) != 1 || payload.To[0] != store.purchase.Recipient || payload.From != resend.from ||
		!strings.Contains(payload.Subject, store.purchase.EventTitle) {
		t.Fatal("missing message envelope")
	}
	for _, fragment := range []string{"María López", "Música en vivo &amp; café", "28/11/2026 · 6:30 p. m.",
		"Platea central", "Balcón preferencial", "TKHN-1001", "TKHN-1002", store.purchase.Address} {
		if !strings.Contains(payload.HTML, fragment) {
			t.Errorf("missing HTML fragment %q", fragment)
		}
	}
	if len(payload.Attachments) != 1 || payload.Attachments[0].Filename != "boletos-ticket-hn.pdf" {
		t.Fatal("missing ticket attachment")
	}
	pdf := payload.Attachments[0].Content
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || bytes.Count(pdf, []byte("/Type /Page\n")) != 2 {
		t.Fatal("expected one PDF page for each of the two tickets")
	}
	// This opt-in path allows visual inspection of the test fixture outside the repository.
	if path := os.Getenv("TICKETHN_TEST_PDF_PATH"); path != "" {
		if err := os.WriteFile(path, pdf, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

type purchaseTestStore struct {
	job        events.PurchaseEmailJob
	purchase   events.PurchaseEmail
	reads      int
	retryAfter time.Duration
	finished   bool
	sent       bool
}

func (s *purchaseTestStore) ClaimPurchaseEmail(context.Context) (events.PurchaseEmailJob, bool, error) {
	s.job.Attempt++
	return s.job, true, nil
}

func (s *purchaseTestStore) PurchaseEmail(context.Context, string) (events.PurchaseEmail, error) {
	s.reads++
	return s.purchase, nil
}

func (s *purchaseTestStore) SavePurchaseEmailRequest(_ context.Context, _ events.PurchaseEmailJob, body []byte) error {
	now := time.Now()
	s.job.FirstAttemptAt = &now
	s.job.RequestBody = body
	return nil
}

func (s *purchaseTestStore) RetryPurchaseEmail(_ context.Context, _ events.PurchaseEmailJob, delay time.Duration) error {
	s.retryAfter = delay
	return nil
}

func (s *purchaseTestStore) FinishPurchaseEmail(_ context.Context, _ events.PurchaseEmailJob, sent bool) error {
	s.finished, s.sent = true, sent
	return nil
}
