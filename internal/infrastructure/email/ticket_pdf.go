package email

import (
	"bytes"
	"errors"
	"strings"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
	"github.com/phpdave11/gofpdf"
	"github.com/skip2/go-qrcode"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func purchasePDF(view purchaseView, signer events.QRSigner) ([]byte, error) {
	if signer == nil || len(view.Tickets) == 0 {
		return nil, errors.New("ticket PDF requires signed tickets")
	}
	pdf := gofpdf.NewCustom(&gofpdf.InitType{UnitStr: "mm", Size: gofpdf.SizeType{Wd: 108, Ht: 180}})
	pdf.SetTitle("Boletos de acceso | TicketHN", true)
	pdf.SetAuthor("TicketHN", true)
	pdf.SetMargins(0, 0, 0)
	pdf.SetCellMargin(0)
	pdf.SetAutoPageBreak(false, 0)
	pdf.AddUTF8FontFromBytes("Ticket", "", goregular.TTF)
	pdf.AddUTF8FontFromBytes("Ticket", "B", gobold.TTF)

	for _, ticket := range view.Tickets {
		qr, err := qrcode.New(signer.Sign(ticket.ID, view.EventID), qrcode.Medium)
		if err != nil {
			return nil, err
		}
		ticketPage(pdf, view, ticket, qr.Bitmap())
	}
	var output bytes.Buffer
	if err := pdf.Output(&output); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func ticketPage(pdf *gofpdf.Fpdf, view purchaseView, ticket events.PurchaseTicket, qr [][]bool) {
	pdf.AddPage()
	pdf.SetFillColor(237, 242, 249)
	pdf.Rect(0, 0, 108, 180, "F")
	pdf.SetFillColor(255, 255, 255)
	pdf.RoundedRect(7, 7, 94, 166, 4, "1234", "F")
	pdf.SetFillColor(11, 31, 77)
	pdf.RoundedRect(7, 7, 94, 53, 4, "12", "F")
	pdf.SetFillColor(37, 99, 235)
	pdf.Rect(7, 56, 94, 4, "F")

	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Ticket", "B", 14)
	pdf.Text(14, 18, "TicketHN")
	pdf.SetTextColor(110, 231, 183)
	pdf.SetFont("Ticket", "", 9)
	pdf.Text(14, 27, "BOLETO DE ACCESO")
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Ticket", "B", 17)
	ticketText(pdf, 14, 32, 80, 6.6, 3, view.EventTitle)

	ticketDetail(pdf, 14, 70, "FECHA Y HORA", view.When, 1)
	ticketDetail(pdf, 14, 88, "LUGAR", view.Address, 2)
	ticketDetail(pdf, 14, 108, "LOCALIDAD", ticket.Locality, 2)

	pdf.SetDrawColor(133, 155, 185)
	pdf.SetDashPattern([]float64{1.5, 1.4}, 0)
	pdf.Line(7, 127, 101, 127)
	pdf.SetDashPattern(nil, 0)
	pdf.SetFillColor(237, 242, 249)
	pdf.Circle(7, 127, 3, "F")
	pdf.Circle(101, 127, 3, "F")

	drawTicketQR(pdf, qr, 12, 131, 38)
	pdf.SetTextColor(56, 75, 106)
	pdf.SetFont("Ticket", "B", 8)
	pdf.Text(55, 142, "FOLIO")
	pdf.SetTextColor(11, 31, 77)
	pdf.SetFont("Ticket", "B", 12)
	ticketText(pdf, 55, 145, 40, 5, 2, ticket.Code)
	pdf.SetTextColor(56, 75, 106)
	pdf.SetFont("Ticket", "", 9)
	ticketText(pdf, 55, 157, 39, 4, 2, "Presenta este QR en la entrada.")
}

func ticketDetail(pdf *gofpdf.Fpdf, x, y float64, label, value string, lines int) {
	pdf.SetTextColor(56, 75, 106)
	pdf.SetFont("Ticket", "B", 8)
	pdf.Text(x, y, label)
	pdf.SetTextColor(11, 31, 77)
	pdf.SetFont("Ticket", "", 12)
	ticketText(pdf, x, y+2, 80, 4.7, lines, value)
}

func ticketText(pdf *gofpdf.Fpdf, x, y, width, lineHeight float64, limit int, text string) {
	lines := pdf.SplitText(strings.Join(strings.Fields(text), " "), width)
	if len(lines) > limit {
		lines = lines[:limit]
		last := []rune(lines[limit-1])
		for len(last) > 0 && pdf.GetStringWidth(string(last)+"…") > width {
			last = last[:len(last)-1]
		}
		lines[limit-1] = strings.TrimSpace(string(last)) + "…"
	}
	for i, line := range lines {
		pdf.SetXY(x, y+float64(i)*lineHeight)
		pdf.CellFormat(width, lineHeight, line, "", 0, "L", false, 0, "")
	}
}

func drawTicketQR(pdf *gofpdf.Fpdf, qr [][]bool, x, y, size float64) {
	module := size / float64(len(qr))
	pdf.SetFillColor(0, 0, 0)
	for row, modules := range qr {
		start := -1
		// Draw runs so adjacent dark modules have no seams when printed.
		for col := 0; col <= len(modules); col++ {
			dark := col < len(modules) && modules[col]
			if dark && start < 0 {
				start = col
			} else if !dark && start >= 0 {
				pdf.Rect(x+float64(start)*module, y+float64(row)*module, float64(col-start)*module, module, "F")
				start = -1
			}
		}
	}
}
