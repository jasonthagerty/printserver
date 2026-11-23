// Package canon builds Canon PIXMA maintenance jobs: a nozzle check
// pattern and a print head cleaning. Canon's Linux driver
// (cnijfilter2, cmdtocanonij2) lists these two for the MG3600 series in
// its PPD as BJL lines; IVEC-era printers (device ID CMD has IVEC) only act
// on them inside Canon's job wrapper. The wrapper and framing follow the
// bytes documented by mfp-print-scan-server (MIT,
// github.com/bogdik/mfp-print-scan-server, app/printing/maintenance.py),
// captured from Canon's own driver and verified on an MG2500.
//
// The job is sent to the printer as raw data (application/vnd.cups-raw),
// so nothing filters it.
package canon

import (
	"bytes"
	"fmt"
	"time"
)

// Action is a maintenance operation the MG3600 series supports.
type Action string

const (
	NozzleCheck Action = "nozzle-check" // prints Canon's nozzle check pattern
	Cleaning    Action = "cleaning"     // regular print head cleaning, all inks
)

var bjlCommand = map[Action]string{
	NozzleCheck: "@TestPrint=NozzleCheck",
	Cleaning:    "@Cleaning=1ALL",
}

const (
	xmlHeader = `<?xml version="1.0" encoding="utf-8" ?>`
	nsIVEC    = `xmlns:ivec="http://www.canon.com/ns/cmd/2008/07/common/"`
	nsVCN     = `xmlns:vcn="http://www.canon.com/ns/cmd/2008/07/canon/"`
	jobID     = "00000001"
)

// Job returns the raw job for a, stamped with the printer's clock set to now.
func Job(a Action, now time.Time) ([]byte, error) {
	cmd, ok := bjlCommand[a]
	if !ok {
		return nil, fmt.Errorf("canon: unknown maintenance action %q", a)
	}
	var b bytes.Buffer
	// IVEC StartJob, then switch the printer into BJL mode.
	b.WriteString(xmlHeader + `<cmd ` + nsIVEC + `><ivec:contents><ivec:operation>StartJob</ivec:operation>` +
		`<ivec:param_set servicetype="print"><ivec:jobID>` + jobID + `</ivec:jobID><ivec:bidi>0</ivec:bidi>` +
		`</ivec:param_set></ivec:contents></cmd>`)
	b.WriteString(xmlHeader + `<cmd ` + nsIVEC + ` ` + nsVCN + `><ivec:contents><ivec:operation>VendorCmd</ivec:operation>` +
		`<ivec:param_set servicetype="print"><vcn:ijoperation>ModeShift</vcn:ijoperation>` +
		`<vcn:ijmode>1</vcn:ijmode><ivec:jobID>` + jobID + `</ivec:jobID></ivec:param_set></ivec:contents></cmd>`)
	// BJL blocks: a maintenance job, the clock, then the command.
	bjl(&b, "ControlMode=Driver", "$JobType=Mnt")
	bjl(&b, "ControlMode=Common", "SetTime="+now.Format("20060102150405"))
	bjl(&b, cmd)
	b.WriteString(xmlHeader + `<cmd ` + nsIVEC + `><ivec:contents><ivec:operation>EndJob</ivec:operation>` +
		`<ivec:param_set servicetype="print"><ivec:jobID>` + jobID + `</ivec:jobID></ivec:param_set>` +
		`</ivec:contents></cmd>`)
	return b.Bytes(), nil
}

// bjl writes one BJL block: leave packet mode (ESC [ K ...), then the lines
// between BJLSTART and BJLEND.
func bjl(b *bytes.Buffer, lines ...string) {
	b.WriteString("\x1b[K\x02\x00\x00\x1f")
	b.WriteString("BJLSTART\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("BJLEND\n")
}
