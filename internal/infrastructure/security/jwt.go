package security

import (
	"crypto/rand"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWT struct {
	key    []byte
	issuer string
	ttl    time.Duration
	parser *jwt.Parser
}

func NewJWT(key []byte, issuer string, ttl time.Duration) *JWT {
	return &JWT{
		key: key, issuer: issuer, ttl: ttl,
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(issuer),
			jwt.WithExpirationRequired(),
			jwt.WithIssuedAt(),
			jwt.WithStrictDecoding(),
		),
	}
}

func (j *JWT) Issue(userID string) (string, error) {
	now := time.Now()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: userID, Issuer: j.issuer, ID: rand.Text(),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(j.ttl)),
	}).SignedString(j.key)
}

func (j *JWT) Subject(raw string) (string, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := j.parser.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
		return j.key, nil
	})
	if err != nil || !token.Valid || claims.Subject == "" || claims.IssuedAt == nil || claims.ExpiresAt == nil || !claims.ExpiresAt.After(claims.IssuedAt.Time) {
		return "", errors.New("invalid access token")
	}
	return claims.Subject, nil
}
