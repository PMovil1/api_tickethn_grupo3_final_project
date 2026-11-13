package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
)

type Service struct {
	repo       Repository
	passwords  Passwords
	tokens     AccessTokens
	mailer     Mailer
	logger     *slog.Logger
	codeKey    []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func New(repo Repository, passwords Passwords, tokens AccessTokens, mailer Mailer, logger *slog.Logger, codeKey []byte, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{
		repo: repo, passwords: passwords, tokens: tokens, mailer: mailer, logger: logger,
		codeKey: codeKey, accessTTL: accessTTL, refreshTTL: refreshTTL,
	}
}

func (s *Service) Register(ctx context.Context, name, email, password string) error {
	email, err := NormalizeEmail(email)
	name = strings.TrimSpace(name)
	if err != nil || !validName(name) || !validPassword(password) {
		return ErrInvalidInput
	}
	if _, err := s.repo.UserByEmail(ctx, email); err == nil {
		return ErrDuplicate
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	hash, err := s.passwords.Hash(password)
	if err != nil {
		return err
	}
	code, codeHash, err := newCode(s.codeKey, domain.VerifyEmail, email)
	if err != nil {
		return err
	}
	if err := s.repo.Register(ctx, domain.User{FullName: name, Email: email, PasswordHash: hash}, codeHash); err != nil {
		return err
	}
	s.send(ctx, email, domain.VerifyEmail, code)
	return nil
}

func (s *Service) RequestCode(ctx context.Context, email string, purpose domain.CodePurpose) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	code, hash, err := newCode(s.codeKey, purpose, email)
	if err != nil {
		return err
	}
	issued, err := s.repo.IssueCode(ctx, email, purpose, hash)
	if issued && err == nil {
		s.send(ctx, email, purpose, code)
	}
	return err
}

func (s *Service) VerifyEmail(ctx context.Context, email, code string) error {
	return s.consume(ctx, email, code, domain.VerifyEmail, "")
}

func (s *Service) ResetPassword(ctx context.Context, email, code, password string) error {
	if !validPassword(password) {
		return ErrInvalidInput
	}
	return s.consume(ctx, email, code, domain.ResetPassword, password)
}

func (s *Service) consume(ctx context.Context, email, code string, purpose domain.CodePurpose, password string) error {
	email, err := NormalizeEmail(email)
	if err != nil || !validCode(code) {
		return ErrInvalidInput
	}
	var hash string
	if purpose == domain.ResetPassword {
		hash, err = s.passwords.Hash(password)
		if err != nil {
			return err
		}
	}
	return s.repo.ConsumeCode(ctx, email, purpose, func(stored string) bool {
		return codeMatches(s.codeKey, purpose, email, code, stored)
	}, hash)
}

func (s *Service) send(ctx context.Context, email string, purpose domain.CodePurpose, code string) {
	if err := s.mailer.Send(ctx, email, purpose, code); err != nil {
		s.logger.WarnContext(ctx, "Could not queue authentication email", "purpose", purpose)
	}
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	User         domain.User
}

func (s *Service) Login(ctx context.Context, email, password string, client domain.Client) (TokenPair, error) {
	normalized, err := NormalizeEmail(email)
	event := domain.LoginEvent{Email: normalized, Client: client}
	if err != nil {
		attempt := strings.Map(func(c rune) rune {
			if c == 0 {
				return -1
			}
			return c
		}, strings.ToLower(strings.TrimSpace(email)))
		runes := []rune(attempt)
		event.Email = string(runes[:min(255, len(runes))])
		event.Failure = "invalid_input"
		return TokenPair{}, s.failedLogin(ctx, event, ErrInvalidInput)
	}
	user, err := s.repo.UserByEmail(ctx, normalized)
	if err != nil && !errors.Is(err, ErrNotFound) {
		event.Failure = "lookup_failed"
		return TokenPair{}, errors.Join(err, s.repo.RecordLogin(ctx, event))
	}
	event.UserID = user.ID
	if password == "" || len(password) > 72 {
		event.Failure = "invalid_input"
		return TokenPair{}, s.failedLogin(ctx, event, ErrInvalidInput)
	}
	hash := s.passwords.DummyHash()
	if user.ID != "" {
		hash = user.PasswordHash
	}
	switch {
	case !s.passwords.Matches(password, hash) || user.ID == "":
		event.Failure = "invalid_credentials"
	case !user.Active:
		event.Failure = "account_inactive"
	case user.EmailVerifiedAt == nil:
		event.Failure = "email_unverified"
	}
	if event.Failure != "" {
		return TokenPair{}, s.failedLogin(ctx, event, ErrCredentials)
	}
	refresh := newRefresh()
	pair, err := s.pair(user, refresh)
	if err != nil {
		event.Failure = "token_issue_failed"
		return TokenPair{}, errors.Join(err, s.repo.RecordLogin(ctx, event))
	}
	if err := s.repo.StartSession(ctx, user, refreshHash(refresh), s.refreshTTL, event); err != nil {
		if errors.Is(err, ErrNotEligible) {
			event.Failure = "account_changed"
			return TokenPair{}, s.failedLogin(ctx, event, ErrCredentials)
		}
		event.Failure = "session_create_failed"
		return TokenPair{}, errors.Join(err, s.repo.RecordLogin(ctx, event))
	}
	return pair, nil
}

func (s *Service) failedLogin(ctx context.Context, event domain.LoginEvent, cause error) error {
	if err := s.repo.RecordLogin(ctx, event); err != nil {
		return err
	}
	return cause
}

func (s *Service) Refresh(ctx context.Context, token string, client domain.Client) (TokenPair, error) {
	if !validRefresh(token) {
		return TokenPair{}, ErrUnauthorized
	}
	refresh := newRefresh()
	user, err := s.repo.RotateSession(ctx, refreshHash(token), refreshHash(refresh), client)
	if err != nil {
		return TokenPair{}, err
	}
	return s.pair(user, refresh)
}

func (s *Service) pair(user domain.User, refresh string) (TokenPair, error) {
	access, err := s.tokens.Issue(user.ID)
	if err != nil {
		return TokenPair{}, err
	}
	user.PasswordHash = ""
	return TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresIn: int64(s.accessTTL / time.Second), User: user}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	id, err := s.tokens.Subject(token)
	if err != nil || !validUUID(id) {
		return domain.User{}, ErrUnauthorized
	}
	user, err := s.repo.UserByID(ctx, id)
	if errors.Is(err, ErrNotFound) || (err == nil && (!user.Active || user.EmailVerifiedAt == nil)) {
		return domain.User{}, ErrUnauthorized
	}
	user.PasswordHash = ""
	return user, err
}

func (s *Service) Logout(ctx context.Context, userID, token string) error {
	if !validRefresh(token) {
		return ErrUnauthorized
	}
	return s.repo.RevokeRefresh(ctx, userID, refreshHash(token))
}

func (s *Service) Sessions(ctx context.Context, userID string) ([]domain.Session, error) {
	return s.repo.Sessions(ctx, userID)
}

func (s *Service) Users(ctx context.Context) ([]domain.User, error) {
	return s.repo.Users(ctx)
}

func (s *Service) SetUserRole(ctx context.Context, userID string, role domain.Role, assigned bool) error {
	if !validUUID(userID) {
		return ErrInvalidInput
	}
	switch role {
	case domain.Buyer, domain.Organizer, domain.Validator, domain.Admin:
		return s.repo.SetUserRole(ctx, userID, role, assigned)
	default:
		return ErrInvalidInput
	}
}

func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	if !validUUID(sessionID) {
		return ErrInvalidInput
	}
	return s.repo.RevokeSession(ctx, userID, sessionID)
}
