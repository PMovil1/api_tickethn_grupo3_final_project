package security

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWT(t *testing.T) {
	key := []byte("test secret with at least 32 bytes")
	service := NewJWT(key, "tickethn-api", time.Minute)
	const subject = "73428bdd-e13e-44bc-92b3-66c43a622824"
	token, err := service.Issue(subject)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := service.Subject(token); err != nil || got != subject {
		t.Fatalf("Subject = %q, %v", got, err)
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer: "tickethn-api", Subject: subject,
		IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute)), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
	}
	for _, tc := range []struct {
		name   string
		method jwt.SigningMethod
		key    any
		change func(*jwt.RegisteredClaims)
	}{
		{"wrong key", jwt.SigningMethodHS256, []byte("other key"), nil},
		{"wrong issuer", jwt.SigningMethodHS256, key, func(c *jwt.RegisteredClaims) { c.Issuer = "other" }},
		{"expired", jwt.SigningMethodHS256, key, func(c *jwt.RegisteredClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Second)) }},
		{"missing expiration", jwt.SigningMethodHS256, key, func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil }},
		{"wrong algorithm", jwt.SigningMethodHS512, key, nil},
		{"unsigned", jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := claims
			if tc.change != nil {
				tc.change(&c)
			}
			raw, err := jwt.NewWithClaims(tc.method, c).SignedString(tc.key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Subject(raw); err == nil {
				t.Fatal("accepted an invalid access token")
			}
		})
	}
}
