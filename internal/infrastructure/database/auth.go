package database

import (
	"context"
	"errors"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuthRepository struct {
	pool *pgxpool.Pool
}

func NewAuthRepository(pool *pgxpool.Pool) *AuthRepository {
	return &AuthRepository{pool: pool}
}

const userRoles = `ARRAY(SELECT r.code::text FROM public.user_roles ur JOIN public.roles r ON r.id = ur.role_id
	      WHERE ur.user_id = u.id ORDER BY r.code)`

const userFields = `
	u.id::text, u.full_name, u.email, u.password_hash, u.is_active, u.email_verified_at, u.created_at, ` + userRoles

func scanUser(row pgx.Row) (domain.User, error) {
	var user domain.User
	var roles []string
	err := row.Scan(&user.ID, &user.FullName, &user.Email, &user.PasswordHash, &user.Active,
		&user.EmailVerifiedAt, &user.CreatedAt, &roles)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, auth.ErrNotFound
	}
	for _, role := range roles {
		user.Roles = append(user.Roles, domain.Role(role))
	}
	return user, err
}

func (r *AuthRepository) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userFields+` FROM public.users u WHERE lower(u.email) = $1`, email))
}

func (r *AuthRepository) UserByID(ctx context.Context, id string) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userFields+` FROM public.users u WHERE u.id = $1`, id))
}

func (r *AuthRepository) Users(ctx context.Context) ([]domain.User, error) {
	rows, err := r.pool.Query(ctx, `SELECT u.id::text, u.full_name, u.email, `+userRoles+`
		FROM public.users u ORDER BY lower(u.full_name), u.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.User, error) {
		var user domain.User
		err := row.Scan(&user.ID, &user.FullName, &user.Email, &user.Roles)
		return user, err
	})
}

func (r *AuthRepository) SetUserRole(ctx context.Context, userID string, role domain.Role, assigned bool) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Serialize changes to this user's role assignments and detect missing users.
		var exists int
		err := tx.QueryRow(ctx, `SELECT 1 FROM public.users WHERE id = $1 FOR UPDATE`, userID).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrNotFound
		}
		if err != nil {
			return err
		}

		const selectedRole = `WITH selected_role AS (SELECT id FROM public.roles WHERE code::text = $2), changed AS (`
		mutation := `DELETE FROM public.user_roles USING selected_role
			WHERE user_id = $1::uuid AND role_id = selected_role.id`
		if assigned {
			mutation = `INSERT INTO public.user_roles (user_id, role_id)
				SELECT $1::uuid, id FROM selected_role
				WHERE NOT EXISTS (SELECT 1 FROM public.user_roles WHERE user_id = $1::uuid AND role_id = selected_role.id)
				ON CONFLICT DO NOTHING`
		}
		var roleExists bool
		err = tx.QueryRow(ctx, selectedRole+mutation+`) SELECT EXISTS (SELECT 1 FROM selected_role)`, userID, role).Scan(&roleExists)
		if err == nil && !roleExists {
			return auth.ErrRoleNotFound
		}
		return err
	})
}

func (r *AuthRepository) Register(ctx context.Context, user domain.User, codeHash string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var userID string
		err := tx.QueryRow(ctx, `
			INSERT INTO public.users (full_name, email, password_hash)
			VALUES ($1, $2, $3)
			ON CONFLICT (lower(email)) DO NOTHING
			RETURNING id::text`, user.FullName, user.Email, user.PasswordHash).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrDuplicate
		}
		if err != nil {
			return err
		}
		return insertCode(ctx, tx, "public.email_verification_tokens", userID, codeHash)
	})
}

type executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func lockAccount(ctx context.Context, tx pgx.Tx, email string, purpose domain.CodePurpose) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		SELECT id::text FROM public.users WHERE lower(email) = $1 AND is_active
			AND (NOT $2::boolean OR email_verified_at IS NULL) FOR UPDATE`, email, purpose == domain.VerifyEmail).Scan(&id)
	return id, err
}

func codeTable(purpose domain.CodePurpose) (string, error) {
	switch purpose {
	case domain.VerifyEmail:
		return "public.email_verification_tokens", nil
	case domain.ResetPassword:
		return "public.password_reset_tokens", nil
	default:
		return "", errors.New("unknown authentication code purpose")
	}
}

func insertCode(ctx context.Context, tx pgx.Tx, table, userID, hash string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO `+table+` (user_id, token_hash, expires_at)
		VALUES ($1, $2, now() + make_interval(secs => $3))`, userID, hash, domain.CodeLifetime.Seconds())
	return err
}

func (r *AuthRepository) IssueCode(ctx context.Context, email string, purpose domain.CodePurpose, codeHash string) (bool, error) {
	table, err := codeTable(purpose)
	if err != nil {
		return false, err
	}
	var issued bool
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		id, err := lockAccount(ctx, tx, email, purpose)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var recent bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM `+table+` WHERE user_id = $1 AND created_at > clock_timestamp() - interval '60 seconds'
		)`, id).Scan(&recent); err != nil || recent {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE `+table+` SET consumed_at = now() WHERE user_id = $1 AND consumed_at IS NULL`, id); err != nil {
			return err
		}
		if err := insertCode(ctx, tx, table, id, codeHash); err != nil {
			return err
		}
		issued = true
		return nil
	})
	return issued, err
}

func (r *AuthRepository) ConsumeCode(ctx context.Context, email string, purpose domain.CodePurpose, matches func(string) bool, passwordHash string) error {
	table, err := codeTable(purpose)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		id, err := lockAccount(ctx, tx, email, purpose)
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrInvalidCode
		}
		if err != nil {
			return err
		}
		var codeID, stored string
		err = tx.QueryRow(ctx, `SELECT id::text, token_hash FROM `+table+`
			WHERE user_id = $1 AND consumed_at IS NULL AND expires_at > clock_timestamp()
			ORDER BY created_at DESC, id DESC LIMIT 1`, id).Scan(&codeID, &stored)
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrInvalidCode
		}
		if err != nil {
			return err
		}
		if !matches(stored) {
			return auth.ErrInvalidCode
		}
		result, err := tx.Exec(ctx, `UPDATE `+table+` SET consumed_at = clock_timestamp()
			WHERE id = $1 AND consumed_at IS NULL AND expires_at > clock_timestamp()`, codeID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return auth.ErrInvalidCode
		}
		if purpose == domain.VerifyEmail {
			_, err = tx.Exec(ctx, `UPDATE public.users SET email_verified_at = now() WHERE id = $1`, id)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE public.users SET password_hash = $2 WHERE id = $1`, id, passwordHash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE public.password_reset_tokens SET consumed_at = now()
			WHERE user_id = $1 AND consumed_at IS NULL`, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE public.user_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id)
		return err
	})
}

func (r *AuthRepository) RecordLogin(ctx context.Context, event domain.LoginEvent) error {
	return insertLoginEvent(ctx, r.pool, event)
}

func insertLoginEvent(ctx context.Context, db executor, event domain.LoginEvent) error {
	_, err := db.Exec(ctx, `INSERT INTO public.login_events
		(user_id, email_attempted, is_successful, failure_reason, ip_address, user_agent)
		VALUES (NULLIF($1, '')::uuid, $2, $3, NULLIF($4, ''), NULLIF($5, '')::inet, NULLIF($6, ''))`,
		event.UserID, event.Email, event.Failure == "", event.Failure, event.Client.IP, event.Client.UserAgent)
	return err
}
