package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
)

func newCode(key []byte, purpose domain.CodePurpose, email string) (code, hash string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", "", err
	}
	code = fmt.Sprintf("%06d", n.Int64())
	salt := rand.Text()
	return code, salt + ":" + hex.EncodeToString(codeMAC(key, purpose, email, salt, code)), nil
}

func codeMatches(key []byte, purpose domain.CodePurpose, email, code, stored string) bool {
	salt, encoded, ok := strings.Cut(stored, ":")
	if !ok {
		return false
	}
	digest, err := hex.DecodeString(encoded)
	return err == nil && hmac.Equal(digest, codeMAC(key, purpose, email, salt, code))
}

func codeMAC(key []byte, purpose domain.CodePurpose, email, salt, code string) []byte {
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "tickethn-code-v1\x00%s\x00%s\x00%s\x00%s", purpose, email, salt, code)
	return mac.Sum(nil)
}

func newRefresh() string {
	var secret [32]byte
	rand.Read(secret[:])
	return base64.RawURLEncoding.EncodeToString(secret[:])
}

func refreshHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func validRefresh(token string) bool {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == token
}
