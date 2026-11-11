package email

import (
	"context"
	"errors"
	"log/slog"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
)

type message struct {
	to      string
	purpose domain.CodePurpose
	code    string
}

type Queue struct {
	jobs chan message
}

func NewQueue(ctx context.Context, provider auth.Mailer, logger *slog.Logger) *Queue {
	q := &Queue{jobs: make(chan message, 64)}
	go func() {
		for ctx.Err() == nil {
			select {
			case job := <-q.jobs:
				if err := provider.Send(ctx, job.to, job.purpose, job.code); err != nil {
					logger.Warn("Authentication email delivery failed", "purpose", job.purpose, "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return q
}

func (q *Queue) Send(ctx context.Context, to string, purpose domain.CodePurpose, code string) error {
	select {
	case q.jobs <- message{to: to, purpose: purpose, code: code}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.New("authentication email queue is full")
	}
}
