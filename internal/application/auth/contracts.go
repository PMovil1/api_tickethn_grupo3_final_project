package auth

import (
	"context"
	"errors"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrCredentials  = errors.New("invalid credentials")
	ErrDuplicate    = errors.New("email already registered")
	ErrInvalidCode  = errors.New("invalid or expired code")
	ErrUnauthorized = errors.New("invalid authentication")
	ErrNotFound     = errors.New("not found")
	ErrRoleNotFound = errors.New("role not found")
	ErrNotEligible  = errors.New("account or challenge unavailable")
)

type Repository interface {
	UserByEmail(context.Context, string) (domain.User, error)
	UserByID(context.Context, string) (domain.User, error)
	Users(context.Context) ([]domain.User, error)
	SetUserRole(ctx context.Context, userID string, role domain.Role, assigned bool) error
	Register(ctx context.Context, user domain.User, codeHash string) error
	IssueCode(ctx context.Context, email string, purpose domain.CodePurpose, codeHash string) (bool, error)
	ConsumeCode(ctx context.Context, email string, purpose domain.CodePurpose, matches func(string) bool, passwordHash string) error
	RecordLogin(context.Context, domain.LoginEvent) error
	StartSession(ctx context.Context, user domain.User, hash string, ttl time.Duration, event domain.LoginEvent) error
	RotateSession(ctx context.Context, oldHash, newHash string, client domain.Client) (domain.User, error)
	RevokeRefresh(ctx context.Context, userID, hash string) error
	Sessions(ctx context.Context, userID string) ([]domain.Session, error)
	RevokeSession(ctx context.Context, userID, sessionID string) error
}

type Passwords interface {
	Hash(string) (string, error)
	Matches(password, hash string) bool
	// A missing account follows the same password verification path.
	DummyHash() string
}

type AccessTokens interface {
	Issue(userID string) (string, error)
	Subject(token string) (string, error)
}

type Mailer interface {
	Send(ctx context.Context, to string, purpose domain.CodePurpose, code string) error
}
