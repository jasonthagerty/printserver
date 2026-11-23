package supplies

import (
	"bytes"
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

func newStore(t *testing.T, now time.Time) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "supplies.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now }
	return s, path
}

func TestCartridgeDatesPersist(t *testing.T) {
	now := time.Date(2026, 11, 22, 18, 0, 0, 0, time.Local)
	s, path := newStore(t, now)
	if got := s.Get(); len(got.Cartridges) != 2 || got.WarnMonths != 6 || got.NudgeDays != 21 {
		t.Fatalf("defaults = %+v", got)
	}
	if _, err := s.SetInstalled("black", "2026-05-01"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetInstalled("color", s.Today()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetInstalled("cyan", "2026-01-01"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown cartridge: %v", err)
	}
	for _, bad := range []string{"2026-13-01", "yesterday", "2026-12-01"} {
		if _, err := s.SetInstalled("black", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("date %q accepted: %v", bad, err)
		}
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	again.now = s.now
	c := again.Get().Cartridges
	if c[0].InstalledOn != "2026-05-01" || c[1].InstalledOn != "2026-11-22" {
		t.Fatalf("reloaded = %+v", c)
	}
	if d := again.AgeDays(c[0].InstalledOn); d != 205 {
		t.Errorf("age = %d days, want 205", d)
	}
	if !again.Old(c[0].InstalledOn, 6) || again.Old(c[1].InstalledOn, 6) {
		t.Error("6-month warning wrong")
	}
	if again.AgeDays("") != -1 {
		t.Error("unknown date has an age")
	}
}

func TestThresholds(t *testing.T) {
	s, _ := newStore(t, time.Now())
	if _, err := s.SetThresholds(9, 0); err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got.WarnMonths != 9 || got.NudgeDays != 0 {
		t.Errorf("got %+v", got)
	}
	for _, c := range [][2]int{{0, 10}, {61, 10}, {6, -1}, {6, 400}} {
		if _, err := s.SetThresholds(c[0], c[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("%v accepted", c)
		}
	}
}

func TestRecordPrintKeepsTheNewest(t *testing.T) {
	s, path := newStore(t, time.Now())
	t1 := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	if changed, err := s.RecordPrint(4, t1); err != nil || !changed {
		t.Fatalf("first: %v %v", changed, err)
	}
	if changed, _ := s.RecordPrint(3, t1.Add(-time.Hour)); changed {
		t.Error("an older job replaced the newer one")
	}
	if changed, _ := s.RecordPrint(4, t1); changed {
		t.Error("the same job counted twice")
	}
	again, _ := Open(path)
	if got := again.Get(); got.LastJobID != 4 || !got.LastPrinted.Equal(t1) {
		t.Errorf("reloaded = %+v", got)
	}
}

func TestTestPageIsAValidPDF(t *testing.T) {
	pdf := TestPage(time.Date(2026, 11, 22, 18, 0, 0, 0, time.UTC))
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4\n")) || !bytes.HasSuffix(pdf, []byte("%%EOF\n")) {
		t.Fatal("not a PDF")
	}
	m := regexp.MustCompile(`startxref\n(\d+)\n`).FindSubmatch(pdf)
	xref, _ := strconv.Atoi(string(m[1]))
	if !bytes.HasPrefix(pdf[xref:], []byte("xref\n")) {
		t.Fatal("startxref is wrong")
	}
	for i, e := range regexp.MustCompile(`(\d{10}) 00000 n `).FindAllSubmatch(pdf[xref:], -1) {
		off, _ := strconv.Atoi(string(e[1]))
		if want := strconv.Itoa(i+1) + " 0 obj"; !bytes.HasPrefix(pdf[off:], []byte(want)) {
			t.Errorf("object %d offset wrong", i+1)
		}
	}
	for _, ink := range []string{"1.00 0.00 0.00 0.00 k", "0.00 1.00 0.00 0.00 k", "0.00 0.00 1.00 0.00 k", "0.00 0.00 0.00 1.00 k"} {
		if !bytes.Contains(pdf, []byte(ink)) {
			t.Errorf("no %s bar", ink)
		}
	}
}

func TestInkReadingDetectsReplacement(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.Local)
	s, path := newStore(t, now)
	if _, err := s.SetInstalled("color", "2026-03-01"); err != nil {
		t.Fatal(err)
	}
	if r, err := s.RecordInk(now, []Level{{Cartridge: "black", Level: 80, Low: 15}, {Cartridge: "color", Level: 12, Low: 15}}); err != nil || len(r) != 0 {
		t.Fatalf("first reading: %v %v", r, err)
	}
	// Black wobbles up a little; color jumps: a new cartridge.
	r, err := s.RecordInk(now.Add(6*time.Hour), []Level{{Cartridge: "black", Level: 83, Low: 15}, {Cartridge: "color", Level: 100, Low: 15}})
	if err != nil || len(r) != 1 || r[0] != "color" {
		t.Fatalf("replaced = %v (%v), want [color]", r, err)
	}
	again, _ := Open(path)
	got := again.Get()
	if got.Cartridges[1].InstalledOn != "2026-11-23" || got.Cartridges[0].InstalledOn != "" {
		t.Errorf("cartridges = %+v", got.Cartridges)
	}
	if l, ok := got.Ink.Level("color"); !ok || l.Level != 100 || !got.Ink.CheckedAt.Equal(now.Add(6*time.Hour).UTC()) {
		t.Errorf("reading = %+v", got.Ink)
	}
}

func TestCartridgeNames(t *testing.T) {
	for name, want := range map[string]string{"Black": "black", "PGBK": "black", "Color": "color", "Tri-Color": "color", "Cyan": ""} {
		if got := cartridgeFor(name); got != want {
			t.Errorf("%q -> %q, want %q", name, got, want)
		}
	}
}
