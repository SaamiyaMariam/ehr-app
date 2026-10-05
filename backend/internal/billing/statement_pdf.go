package billing

import (
	"fmt"
	"strings"
	"time"
)

// Statement PDFs are rendered only from a StatementSnapshot (never from live
// data), uncompressed and with fixed document dates, so the same snapshot
// always yields the same bytes. They contain the practice, patient name,
// address and account number, the statement lines and totals - no internal
// ids, tokens or claim data.

const (
	stLeft   = 15.0
	stRight  = 200.9
	stBottom = 262.0
)

type stColumn struct {
	title string
	width float64
	right bool
}

// pdfMoney formats an API decimal string as $1,234.56 (blank stays blank).
func pdfMoney(value string) string {
	if value == "" {
		return ""
	}

	cents, ok := parseSignedMoney(value)
	if !ok {
		return value
	}

	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}

	whole := fmt.Sprintf("%d", cents/100)
	var grouped strings.Builder

	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(r)
	}

	return fmt.Sprintf("%s$%s.%02d", sign, grouped.String(), cents%100)
}

func statementColumns(s StatementSnapshot) []stColumn {
	if s.Type == "open_balance" {
		return []stColumn{
			{"Service date", 22, false}, {"Description", 60, false}, {"Charge", 20, true},
			{"Responsibility", 22, true}, {"Payments", 20, true}, {"Adjustments", 20, true}, {"Balance", 22, true},
		}
	}

	return []stColumn{
		{"Date", 22, false}, {"Description", 78, false}, {"Charges", 22, true},
		{"Payments", 22, true}, {"Adjustments", 22, true}, {"Balance", 20, true},
	}
}

func statementRow(s StatementSnapshot, l StatementLine) []string {
	if s.Type == "open_balance" {
		return []string{l.Date, l.Description, pdfMoney(l.Charge), pdfMoney(l.Responsibility), pdfMoney(l.Payments), pdfMoney(l.Adjustments), pdfMoney(l.Balance)}
	}

	return []string{l.Date, l.Description, pdfMoney(l.Charge), pdfMoney(l.Payments), pdfMoney(l.Adjustments), pdfMoney(l.Balance)}
}

func (p *pdfDoc) rightText(x, y, size float64, style, value string) {
	p.SetFont("Helvetica", style, size)
	p.Text(x-p.GetStringWidth(p.tr(value)), y, p.tr(value))
}

func addressBlock(a SnapshotAddress) []string {
	var lines []string

	if a.Address1 != "" {
		lines = append(lines, a.Address1)
	}
	if a.Address2 != "" {
		lines = append(lines, a.Address2)
	}

	city := strings.Trim(strings.TrimSpace(a.City+", "+a.State+" "+a.Zip), ", ")
	if city != "" {
		lines = append(lines, city)
	}

	return lines
}

func renderStatementPDF(snapshots ...StatementSnapshot) ([]byte, string, error) {
	p := newPDF()
	p.SetMargins(stLeft, 15, 15)
	p.SetCompression(false)
	p.SetCatalogSort(true)

	// Fixed document dates keep the output reproducible.
	stamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if len(snapshots) > 0 {
		if d, ok := parseDate(snapshots[0].StatementDate); ok {
			stamp = d
		}
	}
	p.SetCreationDate(stamp)
	p.SetModificationDate(stamp)

	for _, s := range snapshots {
		drawStatement(p, s)
	}

	return p.output()
}

func drawStatement(p *pdfDoc, s StatementSnapshot) {
	cols := statementColumns(s)
	page := 0

	footer := func() {
		p.SetTextColor(120, 120, 120)
		p.SetFont("Helvetica", "", 7.5)
		note := fmt.Sprintf("Statement %s  |  Page %d", s.StatementNumber, page)
		p.Text((stLeft+stRight)/2-p.GetStringWidth(note)/2, 272, p.tr(note))
		p.SetTextColor(0, 0, 0)
	}

	tableHeader := func(y float64) float64 {
		p.SetFillColor(235, 238, 242)
		p.SetDrawColor(180, 180, 180)
		p.Rect(stLeft, y, stRight-stLeft, 6.5, "FD")
		p.SetFont("Helvetica", "B", 8)

		x := stLeft
		for _, c := range cols {
			if c.right {
				p.rightText(x+c.width-1.5, y+4.4, 8, "B", c.title)
			} else {
				p.Text(x+1.5, y+4.4, p.tr(c.title))
			}
			x += c.width
		}

		return y + 6.5
	}

	newPage := func(first bool) float64 {
		if page > 0 {
			footer()
		}
		p.AddPage()
		page++

		if first {
			return 15
		}

		p.text(stLeft, 16, 9, "B", "Statement "+s.StatementNumber+" (continued)")
		return tableHeader(22)
	}

	// --- First page: header --------------------------------------------
	y := newPage(true)

	p.SetTextColor(0, 0, 0)
	p.text(stLeft, y+4, 14, "B", s.Practice.Name)
	ly := y + 10
	for _, line := range addressBlock(s.Practice.Address) {
		p.text(stLeft, ly, 9, "", line)
		ly += 4.5
	}
	if s.Practice.Phone != "" {
		p.text(stLeft, ly, 9, "", "Phone: "+s.Practice.Phone)
	}

	p.rightText(stRight, y+5, 20, "B", "STATEMENT")

	period := "Open balance as of " + s.StatementDate
	if s.Type == "date_range" {
		period = "Period: " + s.StartDate + " to " + s.EndDate
	}

	info := []string{"Statement #: " + s.StatementNumber, "Statement date: " + s.StatementDate, period}
	if s.Patient.AccountNumber != "" {
		info = append(info, "Account #: "+s.Patient.AccountNumber)
	}

	iy := y + 12
	for _, line := range info {
		p.rightText(stRight, iy, 9, "", line)
		iy += 4.8
	}

	// --- Bill to + amount due ------------------------------------------
	by := y + 38
	p.text(stLeft, by, 8, "B", "BILL TO")
	p.text(stLeft, by+5.5, 10.5, "B", s.Patient.Name)

	ay := by + 10.5
	for _, line := range addressBlock(s.Patient.Address) {
		p.text(stLeft, ay, 9.5, "", line)
		ay += 4.6
	}

	p.SetDrawColor(90, 90, 90)
	p.Rect(125, by-4, stRight-125, 27, "D")
	p.text(127.5, by+1, 8, "B", "AMOUNT DUE")
	p.rightText(stRight-2.5, by+9.5, 18, "B", pdfMoney(s.AmountDue))
	p.rightText(stRight-2.5, by+15, 8.5, "", "Patient balance: "+pdfMoney(s.BalanceDue))
	if cents, _ := parseSignedMoney(s.CreditOnAccount); cents > 0 {
		p.rightText(stRight-2.5, by+19.5, 8.5, "", "Credit on account: "+pdfMoney(s.CreditOnAccount))
	}

	// --- Lines -----------------------------------------------------------
	y = tableHeader(by + 36)

	row := func(cells []string, bold bool) {
		if y+5.5 > stBottom {
			y = newPage(false)
		}

		style := ""
		if bold {
			style = "B"
		}

		p.SetDrawColor(215, 215, 215)
		p.Line(stLeft, y+5.5, stRight, y+5.5)

		x := stLeft
		for i, c := range cols {
			p.SetFont("Helvetica", style, 8)
			value := p.fit(cells[i], c.width-3)
			if c.right {
				p.rightText(x+c.width-1.5, y+3.9, 8, style, value)
			} else {
				p.Text(x+1.5, y+3.9, p.tr(value))
			}
			x += c.width
		}

		y += 5.5
	}

	if s.Type == "date_range" {
		if cents, _ := parseSignedMoney(s.PreviousBalance); cents != 0 {
			cells := make([]string, len(cols))
			cells[0], cells[1], cells[len(cols)-1] = s.StartDate, "Balance brought forward", pdfMoney(s.PreviousBalance)
			row(cells, true)
		}
	}

	if len(s.Lines) == 0 {
		cells := make([]string, len(cols))
		cells[1] = "No activity in this period"
		row(cells, false)
	}

	for _, l := range s.Lines {
		row(statementRow(s, l), false)
	}

	// --- Totals ------------------------------------------------------------
	if y+34 > stBottom {
		y = newPage(false)
	}

	y += 4
	totals := [][2]string{}

	if s.Type == "open_balance" {
		totals = append(totals,
			[2]string{"Total patient responsibility", s.TotalCharges},
			[2]string{"Payments and credits applied", s.TotalPayments},
			[2]string{"Adjustments", s.TotalAdjustments},
		)
	} else {
		totals = append(totals,
			[2]string{"Balance brought forward", s.PreviousBalance},
			[2]string{"Charges", s.TotalCharges},
			[2]string{"Payments", s.TotalPayments},
			[2]string{"Adjustments", s.TotalAdjustments},
		)
	}

	totals = append(totals, [2]string{"Balance due", s.BalanceDue})
	if cents, _ := parseSignedMoney(s.CreditOnAccount); cents > 0 {
		totals = append(totals, [2]string{"Credit on account (not yet applied)", "-" + s.CreditOnAccount}, [2]string{"Amount due", s.AmountDue})
	}

	for _, t := range totals {
		style := ""
		if t[0] == "Balance due" || t[0] == "Amount due" {
			style = "B"
		}

		p.text(125, y+3.8, 9, style, t[0])
		p.rightText(stRight-1.5, y+3.8, 9, style, pdfMoney(t[1]))
		y += 5.2

		if y > stBottom {
			y = newPage(false)
		}
	}

	y += 4

	if s.Comment != "" {
		if y+20 > stBottom {
			y = newPage(false)
		}

		p.text(stLeft, y+3, 8, "B", "MESSAGE")
		p.SetFont("Helvetica", "", 9)
		p.SetXY(stLeft, y+5)
		p.MultiCell(stRight-stLeft, 4.5, p.tr(s.Comment), "", "L", false)
		y = p.GetY() + 3
	}

	if y+14 > stBottom {
		y = newPage(false)
	}

	p.SetTextColor(70, 70, 70)
	p.SetFont("Helvetica", "", 8)
	p.SetXY(stLeft, y)
	note := "Insurance payments and adjustments are applied to your account as they are received. "
	if s.Practice.Phone != "" {
		note += "Questions about this statement? Call " + s.Practice.Phone + ". "
	}
	note += "Please make payments payable to " + s.Practice.Name + "."
	p.MultiCell(stRight-stLeft, 4, p.tr(note), "", "L", false)

	footer()
}
