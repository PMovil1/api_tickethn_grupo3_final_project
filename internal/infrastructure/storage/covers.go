package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
)

type Covers struct {
	*objects
}

type objects struct {
	base   string
	bucket string
	key    string
	client *http.Client
}

func NewCovers(projectURL, bucket, key string) *Covers {
	return &Covers{newObjects(projectURL, bucket, key)}
}

func newObjects(projectURL, bucket, key string) *objects {
	return &objects{base: projectURL + "/storage/v1/object/", bucket: bucket, key: key,
		client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}}
}

func (s *Covers) Upload(ctx context.Context, object string, cover events.Cover) (string, error) {
	err := s.request(ctx, http.MethodPost, s.bucket+"/"+object, cover.MediaType, bytes.NewReader(cover.Bytes))
	if err != nil {
		return "", err
	}
	return s.publicPrefix() + object, nil
}

func (s *Covers) Delete(ctx context.Context, publicURL string) error {
	raw, ok := strings.CutPrefix(publicURL, s.publicPrefix())
	if !ok {
		// Existing events can reference assets managed outside this backend.
		return nil
	}
	asset, err := url.Parse(raw)
	if err != nil || asset.IsAbs() || asset.Host != "" || asset.Path == "" ||
		asset.Path != path.Clean(asset.Path) || strings.HasPrefix(asset.Path, "/") ||
		asset.Path == "." || asset.Path == ".." || strings.HasPrefix(asset.Path, "../") {
		return fmt.Errorf("invalid path in existing cover URL")
	}
	return s.delete(ctx, asset.Path)
}

func (s *objects) delete(ctx context.Context, object string) error {
	body, err := json.Marshal(map[string][]string{"prefixes": {object}})
	if err != nil {
		return err
	}
	return s.request(ctx, http.MethodDelete, s.bucket, "application/json", bytes.NewReader(body))
}

func (s *Covers) publicPrefix() string { return s.base + "public/" + s.bucket + "/" }

func (s *objects) request(ctx context.Context, method, path, mediaType string, body io.Reader) error {
	res, err := s.open(ctx, method, path, mediaType, body)
	if err == nil {
		res.Body.Close()
	}
	return err
}

func (s *objects) open(ctx context.Context, method, path, mediaType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", s.key)
	req.Header.Set("Authorization", "Bearer "+s.key)
	if mediaType != "" {
		req.Header.Set("Content-Type", mediaType)
	}
	if method == http.MethodPost {
		req.Header.Set("x-upsert", "false")
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		res.Body.Close()
		return nil, fmt.Errorf("storage returned HTTP %d", res.StatusCode)
	}
	return res, nil
}
