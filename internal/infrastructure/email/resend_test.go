package email

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResend(t *testing.T) {
	resend := NewResend("test-api-key", "TicketHN <noreply@example.com>")
	for _, purpose := range []domain.CodePurpose{domain.VerifyEmail, domain.ResetPassword} {
		t.Run(string(purpose), func(t *testing.T) {
			resend.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://api.resend.com/emails" || r.Method != http.MethodPost ||
					r.Header.Get("Authorization") != "Bearer test-api-key" {
					t.Fatal("invalid Resend request")
				}
				var payload struct {
					From, Subject, HTML string
					To                  []string
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if len(payload.To) != 1 || payload.To[0] != "juan@example.com" ||
					payload.From != resend.from || !strings.Contains(payload.Subject, "TicketHN") ||
					!strings.Contains(payload.HTML, footer) || !strings.Contains(payload.HTML, "012345") {
					t.Fatal("missing recipient or template data")
				}
				return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader("provider details"))}, nil
			})
			err := resend.Send(t.Context(), "juan@example.com", purpose, "012345")
			if err == nil || !strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "provider details") {
				t.Fatalf("unsafe provider error: %v", err)
			}
		})
	}
}
