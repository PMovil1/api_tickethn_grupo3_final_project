package domain

import "time"

type Role string

const (
	Buyer     Role = "COMPRADOR"
	Organizer Role = "ORGANIZADOR"
	Validator Role = "VALIDADOR"
	Admin     Role = "ADMIN"
)

type User struct {
	ID              string
	FullName        string
	Email           string
	PasswordHash    string
	Active          bool
	EmailVerifiedAt *time.Time
	Roles           []Role
	CreatedAt       time.Time
}

type Client struct {
	IP        string
	UserAgent string
}

type Session struct {
	ID         string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	Client     Client
}

type CodePurpose string

const (
	VerifyEmail   CodePurpose = "verify_email"
	ResetPassword CodePurpose = "reset_password"
)

const CodeLifetime = 10 * time.Minute

type LoginEvent struct {
	UserID  string
	Email   string
	Failure string
	Client  Client
}
