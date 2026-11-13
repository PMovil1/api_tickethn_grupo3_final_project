package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// TicketQR signs the versioned ticket and event identifiers with a dedicated key.
type TicketQR struct{ key []byte }

func NewTicketQR(key []byte) *TicketQR { return &TicketQR{key: key} }

func (q *TicketQR) Sign(ticketID, eventID string) string {
	payload := "tickethn-ticket-v1." + ticketID + "." + eventID
	mac := hmac.New(sha256.New, q.key)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
