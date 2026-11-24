package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"time"
)

const MaxAvatarBytes = 5 * 1024 * 1024

var ErrAvatarStorage = errors.New("avatar storage unavailable")

type AvatarImage struct {
	Bytes     []byte
	MediaType string
}

type AvatarRepository interface {
	AvatarURL(ctx context.Context, userID string) (*string, error)
	ReplaceAvatarURL(ctx context.Context, userID, url string) (*string, error)
	ConfirmAvatarURL(ctx context.Context, userID string) (*string, error)
}

type AvatarStore interface {
	Upload(ctx context.Context, object string, image AvatarImage) (string, error)
	Download(ctx context.Context, userID, url string) (*AvatarImage, error)
	Delete(ctx context.Context, userID, url string) error
}

type AvatarService struct {
	repo   AvatarRepository
	store  AvatarStore
	logger *slog.Logger
}

func NewAvatarService(repo AvatarRepository, store AvatarStore, logger *slog.Logger) *AvatarService {
	return &AvatarService{repo: repo, store: store, logger: logger}
}

func (s *AvatarService) Read(ctx context.Context, userID string) (*AvatarImage, error) {
	url, err := s.repo.AvatarURL(ctx, userID)
	if err != nil || url == nil {
		return nil, err
	}
	if s.store == nil {
		return nil, ErrAvatarStorage
	}
	image, err := s.store.Download(ctx, userID, *url)
	if err != nil {
		s.logger.WarnContext(ctx, "Avatar download failed", "user", userID, "error", err)
		return nil, ErrAvatarStorage
	}
	return image, nil
}

func (s *AvatarService) Replace(ctx context.Context, userID string, image AvatarImage) error {
	if s.store == nil {
		return ErrAvatarStorage
	}
	url, err := s.store.Upload(ctx, userID+"/"+rand.Text(), image)
	if err != nil {
		s.logger.WarnContext(ctx, "Avatar upload failed", "user", userID, "error", err)
		return ErrAvatarStorage
	}

	previous, err := s.repo.ReplaceAvatarURL(ctx, userID, url)
	if err != nil {
		check, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		current, checkErr := s.repo.ConfirmAvatarURL(check, userID)
		cancel()
		switch {
		case errors.Is(checkErr, ErrNotFound), checkErr == nil && (current == nil || *current != url):
			s.remove(ctx, userID, url)
			return err
		case checkErr != nil:
			s.logger.WarnContext(ctx, "Avatar update could not be confirmed", "user", userID, "error", checkErr)
			return err
		}
	}
	if previous != nil {
		s.remove(ctx, userID, *previous)
	}
	return nil
}

func (s *AvatarService) remove(ctx context.Context, userID, url string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := s.store.Delete(ctx, userID, url); err != nil {
		s.logger.WarnContext(ctx, "Avatar cleanup failed", "user", userID, "error", err)
	}
}
