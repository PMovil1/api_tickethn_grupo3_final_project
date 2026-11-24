package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
)

var avatarKey = regexp.MustCompile(`^[A-Z2-7]+$`)

type Avatars struct {
	*objects
}

func NewAvatars(projectURL, bucket, key string) *Avatars {
	return &Avatars{newObjects(projectURL, bucket, key)}
}

func (s *Avatars) Upload(ctx context.Context, object string, image auth.AvatarImage) (string, error) {
	err := s.request(ctx, http.MethodPost, s.bucket+"/"+object, image.MediaType, bytes.NewReader(image.Bytes))
	if err != nil {
		return "", err
	}
	return s.base + s.privatePath() + object, nil
}

func (s *Avatars) Download(ctx context.Context, userID, url string) (*auth.AvatarImage, error) {
	object, err := s.object(userID, url)
	if err != nil {
		return nil, err
	}
	res, err := s.open(ctx, http.MethodGet, s.privatePath()+object, "", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	mediaType, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	switch mediaType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return nil, errors.New("unexpected avatar media type in storage")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, auth.MaxAvatarBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > auth.MaxAvatarBytes {
		return nil, errors.New("invalid avatar size in storage")
	}
	return &auth.AvatarImage{Bytes: data, MediaType: mediaType}, nil
}

func (s *Avatars) Delete(ctx context.Context, userID, url string) error {
	object, err := s.object(userID, url)
	if err != nil {
		return err
	}
	return s.delete(ctx, object)
}

func (s *Avatars) privatePath() string { return "authenticated/" + s.bucket + "/" }

func (s *Avatars) object(userID, url string) (string, error) {
	key, ok := strings.CutPrefix(url, s.base+s.privatePath()+userID+"/")
	if !ok || !avatarKey.MatchString(key) {
		return "", errors.New("invalid avatar URL for user")
	}
	return userID + "/" + key, nil
}
