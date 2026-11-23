package airprint

import (
	"strings"
	"testing"

	"github.com/jasonthagerty/printserver/internal/cups"
)

func TestRecordMatchesCUPS(t *testing.T) {
	// The queue's IPP attributes, as CUPS 2.4 in the image returns them.
	p := cups.Printer{
		Name:         "MG3620",
		Info:         "Canon MG3620",
		MakeAndModel: "Canon PIXMA MG3620 - CUPS+Gutenprint v5.3.4",
		UUID:         "urn:uuid:5dc96b65-78f2-3996-5dfa-683cd250b35d",
		URF:          []string{"V1.4", "CP1", "W8", "PQ4-5", "SRGB24", "RS601", "DM1", "FN3"},
		Type:         167964, // 0x2901C: includes the default-printer bit
		DocumentTypes: []string{"application/octet-stream", "application/pdf", "application/postscript",
			"image/gif", "image/jpeg", "image/png", "image/pwg-raster", "image/urf", "text/plain"},
	}
	a := &Announcer{Hostname: "printserver", AdminURL: "http://print.example.com/", Port: 631}
	rec := string(a.File(p))

	// What CUPS itself published for this queue (avahi-browse), except
	// adminurl, which now points at the web UI.
	for _, want := range []string{
		"<name>Canon MG3620 @ printserver</name>",
		"<type>_ipp._tcp</type>", "<subtype>_universal._sub._ipp._tcp</subtype>",
		"<type>_ipps._tcp</type>", "<type>_printer._tcp</type>",
		"<port>631</port>", "<port>0</port>",
		"rp=printers/MG3620", "ty=Canon PIXMA MG3620 - CUPS+Gutenprint v5.3.4",
		"product=(Canon PIXMA MG3620)",
		"pdl=application/pdf,application/postscript,image/jpeg,image/png,image/pwg-raster,image/urf",
		"UUID=5dc96b65-78f2-3996-5dfa-683cd250b35d", "TLS=1.2",
		"URF=V1.4,CP1,W8,PQ4-5,SRGB24,RS601,DM1,FN3", "mopria-certified=1.3",
		"Color=T", "Duplex=T", "printer-type=0x901E",
		"adminurl=http://print.example.com/",
	} {
		if !strings.Contains(rec, want) {
			t.Errorf("record lacks %q", want)
		}
	}
	if strings.Contains(rec, ":631/printers") {
		t.Error("record still links CUPS's web interface")
	}
}
