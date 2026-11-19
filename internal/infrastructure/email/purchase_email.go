package email

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
)

//go:embed purchase_email.html
var purchaseHTML string

var purchaseTemplate = template.Must(template.New("purchase").Parse(purchaseHTML))

var honduras = time.FixedZone("America/Tegucigalpa", -6*60*60)

type purchaseView struct {
	events.PurchaseEmail
	When string
}

func (r *Resend) preparePurchase(purchase events.PurchaseEmail, signer events.QRSigner) ([]byte, error) {
	view := purchaseView{PurchaseEmail: purchase, When: "Fecha y hora por confirmar"}
	if purchase.StartsAt != nil {
		when := purchase.StartsAt.In(honduras).Format("02/01/2006 · 3:04 PM")
		view.When = strings.NewReplacer("AM", "a. m.", "PM", "p. m.").Replace(when)
	}
	if view.Address == "" {
		view.Address = "Ubicación por confirmar"
	}
	var html bytes.Buffer
	if err := purchaseTemplate.Execute(&html, view); err != nil {
		return nil, err
	}
	pdf, err := purchasePDF(view, signer)
	if err != nil {
		return nil, fmt.Errorf("render ticket PDF: %w", err)
	}
	var plain strings.Builder
	fmt.Fprintf(&plain, "¡Gracias por tu compra, %s!\n\n%s\n%s\n%s\n\n", view.BuyerName, view.EventTitle, view.When, view.Address)
	for _, ticket := range view.Tickets {
		fmt.Fprintf(&plain, "%s · %s\n", ticket.Locality, ticket.Code)
	}
	plain.WriteString("\nPresenta el QR de cada boleto del PDF adjunto en la entrada. También puedes abrirlos en Mis boletos en la app TicketHN.")
	return json.Marshal(messagePayload{
		From: r.from, To: []string{view.Recipient},
		Subject: "¡Gracias por tu compra! Tus boletos para " + strings.Join(strings.Fields(view.EventTitle), " "),
		HTML:    html.String(), Text: plain.String(),
		Attachments: []attachment{{Filename: "boletos-ticket-hn.pdf", Content: pdf}},
	})
}
