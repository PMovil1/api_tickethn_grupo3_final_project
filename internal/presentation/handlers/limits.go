package handlers

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/response"
)

type limitRule struct {
	window   time.Duration
	perIP    int
	perEmail int
}

var authLimits = map[string]limitRule{
	"register":            {15 * time.Minute, 10, 5},
	"verify-email":        {10 * time.Minute, 30, 5},
	"resend-verification": {15 * time.Minute, 15, 3},
	"forgot-password":     {15 * time.Minute, 15, 3},
	"reset-password":      {10 * time.Minute, 30, 5},
	"login":               {time.Minute, 30, 10},
}

type counter struct {
	count   int
	expires time.Time
}

type authLimiter struct {
	mu          sync.Mutex
	counters    map[string]counter
	nextCleanup time.Time
}

func (l *authLimiter) allow(action, ip, email string, now time.Time) time.Duration {
	rule := authLimits[action]
	l.mu.Lock()
	defer l.mu.Unlock()
	if !now.Before(l.nextCleanup) {
		for key, entry := range l.counters {
			if !now.Before(entry.expires) {
				delete(l.counters, key)
			}
		}
		l.nextCleanup = now.Add(time.Minute)
	}
	if retry := l.take(action+"\x00ip\x00"+ip, rule.perIP, now, rule.window); retry > 0 {
		return retry
	}
	if email != "" {
		return l.take(action+"\x00email\x00"+email, rule.perEmail, now, rule.window)
	}
	return 0
}

func (l *authLimiter) take(key string, limit int, now time.Time, window time.Duration) time.Duration {
	entry, exists := l.counters[key]
	if !exists && len(l.counters) >= 8192 {
		return max(time.Second, l.nextCleanup.Sub(now))
	}
	if !now.Before(entry.expires) {
		entry = counter{expires: now.Add(window)}
	}
	if entry.count >= limit {
		return entry.expires.Sub(now)
	}
	entry.count++
	l.counters[key] = entry
	return 0
}

func tooManyRequests(w http.ResponseWriter, retry time.Duration) {
	seconds := (retry + time.Second - 1) / time.Second
	w.Header().Set("Retry-After", strconv.FormatInt(int64(seconds), 10))
	response.Error(w, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", "Demasiados intentos. Intenta más tarde")
}
