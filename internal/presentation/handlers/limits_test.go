package handlers

import (
	"testing"
	"time"
)

func TestVerificationLimits(t *testing.T) {
	l := authLimiter{counters: make(map[string]counter)}
	now := time.Now()
	const email = "juan@example.com"
	for range 5 {
		if retry := l.allow("verify-email", "127.0.0.1", email, now); retry != 0 {
			t.Fatalf("throttled before five attempts: %v", retry)
		}
	}
	if retry := l.allow("verify-email", "127.0.0.2", email, now); retry != 10*time.Minute {
		t.Fatalf("email limit after changing IP = %v", retry)
	}
	if retry := l.allow("reset-password", "127.0.0.2", email, now); retry != 0 {
		t.Fatalf("verification affected a different action: %v", retry)
	}
	if retry := l.allow("verify-email", "127.0.0.1", email, now.Add(10*time.Minute)); retry != 0 {
		t.Fatalf("limit did not expire: %v", retry)
	}
}
