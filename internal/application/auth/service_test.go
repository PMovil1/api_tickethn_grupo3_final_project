package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
)

const (
	userID    = "73428bdd-e13e-44bc-92b3-66c43a622824"
	sessionID = "6b34edec-e72f-4c58-b545-7589ce3da931"
	address   = "juan@example.com"
	password  = "Password123!"
)

type memoryRepo struct {
	Repository
	user     domain.User
	codes    map[domain.CodePurpose]string
	sessions map[string]bool
	events   []domain.LoginEvent
}

func (m *memoryRepo) UserByEmail(_ context.Context, email string) (domain.User, error) {
	if m.user.ID == "" || email != m.user.Email {
		return domain.User{}, ErrNotFound
	}
	return m.user, nil
}

func (m *memoryRepo) Register(_ context.Context, user domain.User, hash string) error {
	user.ID = userID
	user.Active = true
	user.Roles = []domain.Role{domain.Buyer}
	m.user, m.codes[domain.VerifyEmail] = user, hash
	return nil
}

func (m *memoryRepo) IssueCode(_ context.Context, email string, purpose domain.CodePurpose, hash string) (bool, error) {
	if email != m.user.Email {
		return false, nil
	}
	m.codes[purpose] = hash
	return true, nil
}

func (m *memoryRepo) ConsumeCode(_ context.Context, email string, purpose domain.CodePurpose, matches func(string) bool, passwordHash string) error {
	stored, exists := m.codes[purpose]
	if !exists || email != m.user.Email || !matches(stored) {
		return ErrInvalidCode
	}
	delete(m.codes, purpose)
	if purpose == domain.VerifyEmail {
		now := time.Now()
		m.user.EmailVerifiedAt = &now
	} else {
		m.user.PasswordHash = passwordHash
		for hash := range m.sessions {
			m.sessions[hash] = false
		}
	}
	return nil
}

func (m *memoryRepo) RecordLogin(_ context.Context, event domain.LoginEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *memoryRepo) StartSession(ctx context.Context, _ domain.User, hash string, _ time.Duration, event domain.LoginEvent) error {
	m.sessions[hash] = true
	return m.RecordLogin(ctx, event)
}

func (m *memoryRepo) RotateSession(_ context.Context, oldHash, newHash string, _ domain.Client) (domain.User, error) {
	if !m.sessions[oldHash] {
		return domain.User{}, ErrUnauthorized
	}
	delete(m.sessions, oldHash)
	m.sessions[newHash] = true
	return m.user, nil
}

func (m *memoryRepo) RevokeRefresh(_ context.Context, id, hash string) error {
	if id != m.user.ID || !m.sessions[hash] {
		return ErrUnauthorized
	}
	m.sessions[hash] = false
	return nil
}

func (m *memoryRepo) RevokeSession(_ context.Context, id, session string) error {
	if id != m.user.ID || session != sessionID {
		return ErrNotFound
	}
	for hash := range m.sessions {
		m.sessions[hash] = false
	}
	return nil
}

type fakePasswords struct{}

func (fakePasswords) Hash(password string) (string, error) { return "hash:" + password, nil }
func (fakePasswords) Matches(password, hash string) bool   { return hash == "hash:"+password }
func (fakePasswords) DummyHash() string                    { return "dummy-hash" }

type fakeTokens struct{ AccessTokens }

func (fakeTokens) Issue(id string) (string, error) { return "access:" + id, nil }

type fakeMail struct {
	code    string
	purpose domain.CodePurpose
	err     error
}

func (m *fakeMail) Send(_ context.Context, _ string, purpose domain.CodePurpose, code string) error {
	m.purpose, m.code = purpose, code
	return m.err
}

func fixture() (*Service, *memoryRepo, *fakeMail, *bytes.Buffer) {
	repo := &memoryRepo{codes: make(map[domain.CodePurpose]string), sessions: make(map[string]bool)}
	mail := &fakeMail{}
	logs := &bytes.Buffer{}
	service := New(repo, fakePasswords{}, fakeTokens{}, mail, slog.New(slog.NewTextHandler(logs, nil)),
		[]byte("test code key"), 30*time.Minute, 7*24*time.Hour)
	return service, repo, mail, logs
}

func TestRegisterVerifyAndLogin(t *testing.T) {
	s, repo, mail, _ := fixture()
	ctx := t.Context()
	if err := s.Register(ctx, " Juan Pérez ", " JUAN@example.com ", password); err != nil {
		t.Fatal(err)
	}
	if repo.user.FullName != "Juan Pérez" || repo.user.Email != address || repo.user.PasswordHash == password ||
		repo.user.EmailVerifiedAt != nil || !validCode(mail.code) || repo.codes[domain.VerifyEmail] == mail.code {
		t.Fatal("registration did not normalize and protect credentials")
	}
	if err := s.Register(ctx, "Juan Pérez", address, password); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate registration = %v", err)
	}
	client := domain.Client{IP: "127.0.0.1", UserAgent: "test"}
	for _, tc := range []struct{ password, reason string }{
		{"wrong-password", "invalid_credentials"},
		{password, "email_unverified"},
	} {
		if _, err := s.Login(ctx, address, tc.password, client); !errors.Is(err, ErrCredentials) {
			t.Fatalf("login error = %v", err)
		}
		event := repo.events[len(repo.events)-1]
		if event.Failure != tc.reason || event.Email != address || event.UserID != userID || event.Client != client {
			t.Fatalf("unexpected login event: %+v", event)
		}
	}
	code := mail.code
	if err := s.VerifyEmail(ctx, address, code); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyEmail(ctx, address, code); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("verification code reuse = %v", err)
	}
	pair, err := s.Login(ctx, address, password, client)
	if err != nil {
		t.Fatal(err)
	}
	if !validRefresh(pair.RefreshToken) || pair.AccessToken == "" || pair.ExpiresIn != 1800 || pair.User.PasswordHash != "" ||
		!repo.sessions[refreshHash(pair.RefreshToken)] || repo.events[len(repo.events)-1].Failure != "" {
		t.Fatal("login response or audit event did not match")
	}
}

func TestRefreshRevocationAndPasswordRecovery(t *testing.T) {
	s, _, mail, _ := fixture()
	ctx := t.Context()
	if err := s.Register(ctx, "Juan Pérez", address, password); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyEmail(ctx, address, mail.code); err != nil {
		t.Fatal(err)
	}
	client := domain.Client{}
	login, err := s.Login(ctx, address, password, client)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := s.Refresh(ctx, login.RefreshToken, client)
	if err != nil || rotated.RefreshToken == login.RefreshToken {
		t.Fatalf("refresh error = %v", err)
	}
	if _, err := s.Refresh(ctx, login.RefreshToken, client); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("refresh reuse = %v", err)
	}
	if err := s.Logout(ctx, sessionID, rotated.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("foreign logout = %v", err)
	}
	if err := s.RevokeSession(ctx, userID, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Refresh(ctx, rotated.RefreshToken, client); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked refresh = %v", err)
	}
	login, err = s.Login(ctx, address, password, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestCode(ctx, address, domain.ResetPassword); err != nil || mail.purpose != domain.ResetPassword {
		t.Fatalf("recovery request = %v", err)
	}
	const replacement = "NewPassword123!"
	if err := s.ResetPassword(ctx, address, mail.code, replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Refresh(ctx, login.RefreshToken, client); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("refresh after reset = %v", err)
	}
	if _, err := s.Login(ctx, address, replacement, client); err != nil {
		t.Fatalf("login with new password = %v", err)
	}
	if _, err := s.Login(ctx, address, password, client); !errors.Is(err, ErrCredentials) {
		t.Fatalf("login with old password = %v", err)
	}
}

func TestRegistrationSurvivesMailError(t *testing.T) {
	s, repo, mail, logs := fixture()
	mail.err = errors.New("private provider details")
	if err := s.Register(t.Context(), "Juan Pérez", address, password); err != nil {
		t.Fatal(err)
	}
	if repo.user.ID == "" || !bytes.Contains(logs.Bytes(), []byte("Could not queue authentication email")) ||
		bytes.Contains(logs.Bytes(), []byte(mail.err.Error())) || bytes.Contains(logs.Bytes(), []byte(mail.code)) {
		t.Fatal("mail failure should be logged without exposing details or undoing registration")
	}
	mail.err = nil
	if err := s.RequestCode(t.Context(), address, domain.VerifyEmail); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyEmail(t.Context(), address, mail.code); err != nil {
		t.Fatal(err)
	}
}
