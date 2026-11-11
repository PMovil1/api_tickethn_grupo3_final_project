package health

import (
	"context"
	"time"
)

type DatabasePinger interface {
	Ping(context.Context) error
}

type Check struct {
	database DatabasePinger
	timeout  time.Duration
}

func NewCheck(database DatabasePinger, timeout time.Duration) *Check {
	return &Check{database: database, timeout: timeout}
}

func (c *Check) Execute(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.database.Ping(ctx)
}
