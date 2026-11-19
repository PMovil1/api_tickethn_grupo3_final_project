package email

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
)

const (
	purchasePollInterval = 5 * time.Second
	purchaseWorkTimeout  = 25 * time.Second
	purchaseMaxAttempts  = 8
	// Stop before Resend's 24-hour idempotency window expires.
	purchaseRetryWindow = 23 * time.Hour
)

type purchaseWorker struct {
	store  events.PurchaseEmailStore
	resend *Resend
	signer events.QRSigner
	logger *slog.Logger
}

// RunPurchaseEmails handles pending purchase confirmations until ctx is cancelled.
func RunPurchaseEmails(ctx context.Context, store events.PurchaseEmailStore, resend *Resend, signer events.QRSigner, logger *slog.Logger) {
	worker := purchaseWorker{store: store, resend: resend, signer: signer, logger: logger}
	ticker := time.NewTicker(purchasePollInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		found, err := worker.deliverNext(ctx)
		if err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "Could not process purchase email queue", "error", err)
		}
		if found && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w purchaseWorker) deliverNext(ctx context.Context) (bool, error) {
	workCtx, cancel := context.WithTimeout(ctx, purchaseWorkTimeout)
	defer cancel()
	job, found, err := w.store.ClaimPurchaseEmail(workCtx)
	if err != nil || !found {
		return found, err
	}
	if job.Attempt > purchaseMaxAttempts || (job.FirstAttemptAt != nil && time.Since(*job.FirstAttemptAt) >= purchaseRetryWindow) {
		w.logger.WarnContext(ctx, "Purchase email retry window closed", "order_id", job.OrderID, "attempt", job.Attempt)
		return true, w.store.FinishPurchaseEmail(workCtx, job, false)
	}

	err = w.deliver(workCtx, job)
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	updateCtx, stopUpdate := context.WithTimeout(ctx, 5*time.Second)
	defer stopUpdate()
	if err == nil {
		return true, w.store.FinishPurchaseEmail(updateCtx, job, true)
	}

	w.logger.WarnContext(ctx, "Purchase email attempt failed", "order_id", job.OrderID, "attempt", job.Attempt, "error", err)
	if job.Attempt >= purchaseMaxAttempts || !retryablePurchaseEmail(err) {
		return true, w.store.FinishPurchaseEmail(updateCtx, job, false)
	}
	delay := time.Minute << (job.Attempt - 1)
	return true, w.store.RetryPurchaseEmail(updateCtx, job, delay)
}

func (w purchaseWorker) deliver(ctx context.Context, job events.PurchaseEmailJob) error {
	body := job.RequestBody
	if len(body) == 0 {
		details, err := w.store.PurchaseEmail(ctx, job.OrderID)
		if err != nil {
			return err
		}
		body, err = w.resend.preparePurchase(details, w.signer)
		if err != nil {
			return err
		}
		if err := w.store.SavePurchaseEmailRequest(ctx, job, body); err != nil {
			return err
		}
	}
	return w.resend.send(ctx, body, "purchase-confirmation/"+job.OrderID)
}

func retryablePurchaseEmail(err error) bool {
	var status resendStatusError
	return !errors.As(err, &status) || status >= 500 || status == http.StatusTooManyRequests ||
		status == http.StatusRequestTimeout || status == http.StatusConflict
}
