package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
)

const footer = "Este es un mensaje automático de TicketHN. Por favor, no respondas a este correo, ya que esta dirección no está habilitada para recibir respuestas."

var codeTemplate = template.Must(template.New("code").Parse(`
<!doctype html>
<html lang="es">
<body style="font-family:Arial,sans-serif;color:#182230;margin:0;padding:24px">
<main style="max-width:480px;margin:0 auto">
  <p style="color:#14489c;font-weight:bold">TicketHN</p>
  <h1 style="font-size:24px">{{.Subject}}</h1>
  <p>{{.Instructions}}</p>
  <p style="background:#eef4ff;color:#14489c;padding:16px;text-align:center;font-size:28px;letter-spacing:6px"><strong>{{.Code}}</strong></p>
  <p>El código vence en 10 minutos. Si no solicitaste este código, puedes ignorar el mensaje.</p>
  <hr style="border:0;border-top:1px solid #ddd">
  <p style="color:#475467;font-size:13px">{{.Footer}}</p>
</main>
</body>
</html>`))

type Resend struct {
	apiKey string
	from   string
	client *http.Client
}

func NewResend(apiKey, from string) *Resend {
	return &Resend{
		apiKey: apiKey, from: from,
		client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

func (r *Resend) Send(ctx context.Context, to string, purpose domain.CodePurpose, code string) error {
	data := struct{ Subject, Instructions, Code, Footer string }{Code: code, Footer: footer}
	switch purpose {
	case domain.VerifyEmail:
		data.Subject = "Verifica tu correo en TicketHN"
		data.Instructions = "Ingresa este código para verificar tu dirección de correo."
	case domain.ResetPassword:
		data.Subject = "Restablece tu contraseña de TicketHN"
		data.Instructions = "Ingresa este código para elegir una nueva contraseña."
	default:
		return errors.New("unknown email purpose")
	}
	var html bytes.Buffer
	if err := codeTemplate.Execute(&html, data); err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		HTML    string   `json:"html"`
	}{From: r.from, To: []string{to}, Subject: data.Subject, HTML: html.String()})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return errors.New("Resend request failed")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Resend responded with HTTP %d", resp.StatusCode)
	}
	return nil
}
