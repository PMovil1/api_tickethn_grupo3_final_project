package config

import (
	"errors"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"
)

type Auth struct {
	JWTSecret  []byte
	Issuer     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	ResendKey  string
	EmailFrom  string
}

func LoadAuth() (Auth, error) {
	key := os.Getenv("JWT_SECRET")
	if len(strings.TrimSpace(key)) < 32 {
		return Auth{}, errors.New("JWT_SECRET must contain at least 32 bytes of random data")
	}
	issuer := strings.TrimSpace(os.Getenv("JWT_ISSUER"))
	if issuer == "" {
		issuer = "tickethn-api"
	}
	accessTTL, err := durationEnv("JWT_ACCESS_TTL_MINUTES", 30, time.Minute)
	if err != nil {
		return Auth{}, err
	}
	refreshTTL, err := durationEnv("JWT_REFRESH_TTL_DAYS", 7, 24*time.Hour)
	if err != nil {
		return Auth{}, err
	}
	resendKey := strings.TrimSpace(os.Getenv("RESEND_API_KEY"))
	emailFrom := strings.TrimSpace(os.Getenv("EMAIL_FROM"))
	if resendKey == "" {
		return Auth{}, errors.New("RESEND_API_KEY is required")
	}
	if _, err := mail.ParseAddress(emailFrom); err != nil {
		return Auth{}, errors.New("EMAIL_FROM must be a valid email address")
	}
	return Auth{
		JWTSecret: []byte(key), Issuer: issuer, AccessTTL: accessTTL, RefreshTTL: refreshTTL,
		ResendKey: resendKey, EmailFrom: emailFrom,
	}, nil
}

func durationEnv(name string, fallback int, unit time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return time.Duration(fallback) * unit, nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 || n > int64(time.Duration(1<<63-1)/unit) {
		return 0, errors.New(name + " must be a positive duration")
	}
	return time.Duration(n) * unit, nil
}
