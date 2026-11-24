package supplies

import (
	"bytes"
	"fmt"
	"time"
)

// TestPage is a one-page PDF that uses every ink: a bar each of cyan,
// magenta, yellow and black, the mixes, and a gray ramp. Printing it now
// and then keeps the nozzles from drying, and the bars show at a glance
// whether one is clogged. Colors are sRGB, as phones and computers send
// them, so the page also shows how the driver's color looks in practice.
func TestPage(at time.Time) []byte {
	var content bytes.Buffer
	text := func(x, y, size float64, s string) {
		fmt.Fprintf(&content, "BT /F1 %.0f Tf %.1f %.1f Td (%s) Tj ET\n", size, x, y, pdfEscape(s))
	}
	rect := func(x, y, w, h float64, r, g, b float64) {
		fmt.Fprintf(&content, "%.2f %.2f %.2f rg %.1f %.1f %.1f %.1f re f 0 g\n", r, g, b, x, y, w, h)
	}
	text(72, 720, 24, "Printer test page")
	text(72, 696, 11, "Printed "+at.Format("Monday, January 2, 2006 at 3:04 PM")+" by printserver.")
	// Keep every line inside a 1-inch margin: drivers that print at 1:1
	// (IVEC) clip what Gutenprint's fit-to-margins shrank into view.
	text(72, 680, 11, "Every bar should be solid.")
	text(72, 666, 11, "Streaks or gaps mean a clogged nozzle: use Clean print head on the home screen.")
	bars := []struct {
		label   string
		r, g, b float64
	}{
		{"Cyan", 0, 1, 1}, {"Magenta", 1, 0, 1}, {"Yellow", 1, 1, 0}, {"Black", 0, 0, 0},
		{"Red", 1, 0, 0}, {"Green", 0, 1, 0}, {"Blue", 0, 0, 1},
	}
	y := 620.0
	for _, b := range bars {
		text(72, y+12, 11, b.label)
		rect(150, y, 390, 34, b.r, b.g, b.b)
		y -= 50
	}
	text(72, y+12, 11, "Gray")
	for i := 0; i < 10; i++ {
		v := 1 - float64(i+1)/10 // 10% to 100% gray
		rect(150+float64(i)*39, y, 39, 34, v, v, v)
	}

	var pdf bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	pdf.WriteString("%PDF-1.4\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj("<< /Type /Pages /Count 1 /Kids [3 0 R] >>")
	obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>")
	obj(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", content.Len(), content.String()))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return pdf.Bytes()
}

func pdfEscape(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '(', ')', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			if r < 128 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
