package config

import (
	"errors"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var bucketName = regexp.MustCompile("^[a-z0-9][a-z0-9_-]{0,62}$")

type Events struct {
	StorageURL    string
	StorageKey    string
	StorageBucket string
	QRKey         []byte
}

func LoadEvents(sessionKey []byte) (Events, error) {
	c := Events{
		StorageURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("SUPABASE_URL")), "/"),
		StorageKey:    strings.TrimSpace(os.Getenv("SUPABASE_SERVICE_ROLE_KEY")),
		StorageBucket: strings.TrimSpace(os.Getenv("SUPABASE_EVENT_COVERS_BUCKET")),
		QRKey:         []byte(os.Getenv("TICKET_QR_SECRET")),
	}
	if c.StorageBucket == "" {
		c.StorageBucket = "event-covers"
	}
	if !bucketName.MatchString(c.StorageBucket) {
		return Events{}, errors.New("invalid SUPABASE_EVENT_COVERS_BUCKET")
	}
	if c.StorageURL != "" || c.StorageKey != "" {
		u, err := url.Parse(c.StorageURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
			u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || c.StorageKey == "" {
			return Events{}, errors.New("configure SUPABASE_URL as an HTTPS origin and SUPABASE_SERVICE_ROLE_KEY")
		}
	}
	if len(c.QRKey) > 0 && (len(strings.TrimSpace(string(c.QRKey))) < 32 ||
		strings.TrimSpace(string(c.QRKey)) == strings.TrimSpace(string(sessionKey))) {
		return Events{}, errors.New("TICKET_QR_SECRET must contain at least 32 bytes and differ from JWT_SECRET")
	}
	return c, nil
}
