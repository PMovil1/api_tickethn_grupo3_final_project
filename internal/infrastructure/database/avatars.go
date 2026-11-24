package database

import (
	"context"
	"errors"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
	"github.com/jackc/pgx/v5"
)

const selectAvatarURL = "SELECT avatar_url FROM public.users WHERE id = $1"

func (r *AuthRepository) AvatarURL(ctx context.Context, userID string) (*string, error) {
	return scanAvatarURL(r.pool.QueryRow(ctx, selectAvatarURL, userID))
}

func (r *AuthRepository) ConfirmAvatarURL(ctx context.Context, userID string) (*string, error) {
	return scanAvatarURL(r.pool.QueryRow(ctx, selectAvatarURL+" FOR UPDATE", userID))
}

func (r *AuthRepository) ReplaceAvatarURL(ctx context.Context, userID, url string) (*string, error) {
	var previous *string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		previous, err = scanAvatarURL(tx.QueryRow(ctx, selectAvatarURL+" FOR UPDATE", userID))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE public.users SET avatar_url = $2 WHERE id = $1", userID, url)
		return err
	})
	return previous, err
}

func scanAvatarURL(row pgx.Row) (*string, error) {
	var url *string
	err := row.Scan(&url)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrNotFound
	}
	return url, err
}
