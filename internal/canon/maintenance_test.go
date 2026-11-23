package canon

import (
	"strings"
	"testing"
	"time"
)

func TestJobMatchesCanonFraming(t *testing.T) {
	now := time.Date(2026, 11, 23, 15, 4, 5, 0, time.UTC)
	b, err := Job(Cleaning, now)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	// The reference bytes (mfp-print-scan-server's _canon_command), in order.
	want := []string{
		`<ivec:operation>StartJob</ivec:operation><ivec:param_set servicetype="print"><ivec:jobID>00000001</ivec:jobID><ivec:bidi>0</ivec:bidi>`,
		`<vcn:ijoperation>ModeShift</vcn:ijoperation><vcn:ijmode>1</vcn:ijmode>`,
		"\x1b[K\x02\x00\x00\x1fBJLSTART\nControlMode=Driver\n$JobType=Mnt\nBJLEND\n",
		"\x1b[K\x02\x00\x00\x1fBJLSTART\nControlMode=Common\nSetTime=20261123150405\nBJLEND\n",
		"\x1b[K\x02\x00\x00\x1fBJLSTART\n@Cleaning=1ALL\nBJLEND\n",
		`<ivec:operation>EndJob</ivec:operation>`,
	}
	at := 0
	for _, w := range want {
		i := strings.Index(s[at:], w)
		if i < 0 {
			t.Fatalf("missing or out of order: %q", w)
		}
		at += i + len(w)
	}
	if !strings.HasSuffix(s, "</ivec:contents></cmd>") {
		t.Error("job does not end with the EndJob document")
	}
	if n, _ := Job(NozzleCheck, now); !strings.Contains(string(n), "BJLSTART\n@TestPrint=NozzleCheck\nBJLEND\n") {
		t.Error("nozzle check command missing")
	}
	if _, err := Job("deep-clean", now); err == nil {
		t.Error("unknown action accepted")
	}
}
