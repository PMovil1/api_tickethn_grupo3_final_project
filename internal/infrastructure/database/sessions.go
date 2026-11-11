package database

import (
	"context"
	"errors"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *AuthRepository) StartSession(ctx context.Context, user domain.User, hash string, ttl time.Duration, event domain.LoginEvent) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Serialize new sessions with password changes and account updates.
		var allowed bool
		err := tx.QueryRow(ctx, `
			SELECT is_active AND email_verified_at IS NOT NULL AND password_hash = $2
			FROM public.users WHERE id = $1 FOR UPDATE`, user.ID, user.PasswordHash).Scan(&allowed)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
			return auth.ErrNotEligible
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO public.user_sessions (user_id, refresh_token_hash, expires_at, last_used_at, ip_address, user_agent)
			VALUES ($1, $2, now() + make_interval(secs => $3), now(), NULLIF($4, '')::inet, NULLIF($5, ''))`,
			user.ID, hash, ttl.Seconds(), event.Client.IP, event.Client.UserAgent); err != nil {
			return err
		}
		return insertLoginEvent(ctx, tx, event)
	})
}

func (r *AuthRepository) RotateSession(ctx context.Context, oldHash, newHash string, client domain.Client) (domain.User, error) {
	var user domain.User
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Acquire the account lock before touching the session, as in password reset.
		var err error
		user, err = scanUser(tx.QueryRow(ctx, `
			SELECT `+userFields+` FROM public.users u
			WHERE u.id = (SELECT user_id FROM public.user_sessions WHERE refresh_token_hash = $1)
			FOR UPDATE OF u`, oldHash))
		if errors.Is(err, auth.ErrNotFound) || (err == nil && (!user.Active || user.EmailVerifiedAt == nil)) {
			return auth.ErrUnauthorized
		}
		if err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `UPDATE public.user_sessions
			SET refresh_token_hash = $2, last_used_at = clock_timestamp(),
				ip_address = NULLIF($3, '')::inet, user_agent = NULLIF($4, '')
			WHERE refresh_token_hash = $1 AND user_id = $5 AND revoked_at IS NULL AND expires_at > clock_timestamp()`,
			oldHash, newHash, client.IP, client.UserAgent, user.ID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return auth.ErrUnauthorized
		}
		return nil
	})
	return user, err
}

func (r *AuthRepository) RevokeRefresh(ctx context.Context, userID, hash string) error {
	result, err := r.pool.Exec(ctx, `UPDATE public.user_sessions SET revoked_at = now()
		WHERE user_id = $1 AND refresh_token_hash = $2 AND revoked_at IS NULL AND expires_at > clock_timestamp()`, userID, hash)
	if err == nil && result.RowsAffected() == 0 {
		return auth.ErrUnauthorized
	}
	return err
}

func (r *AuthRepository) Sessions(ctx context.Context, userID string) ([]domain.Session, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, created_at, last_used_at, expires_at, revoked_at,
		coalesce(host(ip_address), ''), coalesce(user_agent, '') FROM public.user_sessions
		WHERE user_id = $1 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Session, error) {
		var session domain.Session
		err := row.Scan(&session.ID, &session.CreatedAt, &session.LastUsedAt, &session.ExpiresAt, &session.RevokedAt,
			&session.Client.IP, &session.Client.UserAgent)
		return session, err
	})
}

func (r *AuthRepository) RevokeSession(ctx context.Context, userID, sessionID string) error {
	result, err := r.pool.Exec(ctx, `UPDATE public.user_sessions SET revoked_at = coalesce(revoked_at, now())
		WHERE user_id = $1 AND id = $2`, userID, sessionID)
	if err == nil && result.RowsAffected() == 0 {
		return auth.ErrNotFound
	}
	return err
}
