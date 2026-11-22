package scans

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/jasonthagerty/printserver/internal/escl"
	"github.com/jasonthagerty/printserver/internal/escl/escltest"
)

func newStore(t *testing.T) (*Store, *escltest.Scanner) {
	t.Helper()
	fake := escltest.New()
	t.Cleanup(fake.Close)
	s, err := NewStore(t.TempDir(), escl.New(fake.Base(), nil), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return s, fake
}

func TestScanToPDFWithPages(t *testing.T) {
	s, fake := newStore(t)
	ctx := context.Background()
	d, err := s.New(ctx, Options{Resolution: 300, Color: "color", Format: "pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Pages != 1 || d.Resolution != 300 || d.Bytes == 0 || d.FileName() == "" {
		t.Errorf("doc = %+v", d)
	}
	if d, err = s.AddPage(ctx, d.ID); err != nil || d.Pages != 2 {
		t.Fatalf("add page: %+v %v", d, err)
	}
	if d, err = s.AddPage(ctx, d.ID); err != nil || d.Pages != 3 {
		t.Fatalf("add page: %+v %v", d, err)
	}
	if len(fake.Settings) != 3 {
		t.Errorf("%d scans sent, want 3", len(fake.Settings))
	}
	if d, err = s.RemovePage(d.ID, 2); err != nil || d.Pages != 2 {
		t.Fatalf("remove page: %+v %v", d, err)
	}
	if _, err := s.OpenPage(d.ID, 3); !errors.Is(err, ErrNotFound) {
		t.Errorf("page 3 after removal: %v", err)
	}

	var buf bytes.Buffer
	if _, err := s.Write(d.ID, &buf); err != nil {
		t.Fatal(err)
	}
	checkPDF(t, buf.Bytes(), 2)
}

func TestGrayJPEG(t *testing.T) {
	s, fake := newStore(t)
	d, err := s.New(context.Background(), Options{Resolution: 150, Color: "gray", Format: "jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ContentType() != "image/jpeg" || filepath.Ext(d.FileName()) != ".jpg" {
		t.Errorf("doc = %+v", d)
	}
	if !bytes.Contains([]byte(fake.Settings[0]), []byte("Grayscale8")) {
		t.Errorf("settings: %s", fake.Settings[0])
	}
	if _, err := s.AddPage(context.Background(), d.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("adding a page to a JPEG: %v", err)
	}
	var buf bytes.Buffer
	if _, err := s.Write(d.ID, &buf); err != nil || !bytes.Equal(buf.Bytes(), escltest.Page(true)) {
		t.Errorf("JPEG download differs (err %v)", err)
	}
}

func TestResolutionFallsBackToSupported(t *testing.T) {
	s, _ := newStore(t)
	d, err := s.New(context.Background(), Options{Resolution: 400, Color: "color", Format: "pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Resolution != 300 {
		t.Errorf("resolution = %d, want 300 (highest supported not above 400)", d.Resolution)
	}
}

func TestBusyScannerDoesNotCreateDoc(t *testing.T) {
	s, fake := newStore(t)
	fake.Busy = true
	if _, err := s.New(context.Background(), Options{Resolution: 300, Color: "color", Format: "pdf"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if docs, _ := s.List(); len(docs) != 0 {
		t.Errorf("%d docs left behind", len(docs))
	}
}

func TestOneScanAtATime(t *testing.T) {
	s, _ := newStore(t)
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	if _, err := s.New(context.Background(), Options{Resolution: 300, Color: "color", Format: "pdf"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy while another scan runs", err)
	}
}

func TestIDsCannotEscape(t *testing.T) {
	s, _ := newStore(t)
	for _, id := range []string{"../etc", "..", "", "20261122-101010-abcdeg", "20261122-101010-abcdef/../x"} {
		if _, err := s.Get(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) = %v", id, err)
		}
		if err := s.Delete(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Delete(%q) = %v", id, err)
		}
	}
}

func TestRenameListDeleteCleanup(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	old, err := s.New(ctx, Options{Resolution: 150, Color: "color", Format: "pdf"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(20 * 24 * time.Hour)
	fresh, err := s.New(ctx, Options{Resolution: 150, Color: "color", Format: "pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if fresh, err = s.Rename(fresh.ID, "Tax / form: 2026"); err != nil {
		t.Fatal(err)
	}
	if fresh.FileName() != "Tax _ form_ 2026.pdf" {
		t.Errorf("file name %q", fresh.FileName())
	}
	if _, err := s.Rename(fresh.ID, "   "); !errors.Is(err, ErrInvalid) {
		t.Errorf("blank rename: %v", err)
	}
	docs, _ := s.List()
	if len(docs) != 2 || docs[0].ID != fresh.ID {
		t.Fatalf("list = %+v, want newest first", docs)
	}

	// An interrupted scan: a directory with no meta.json, older than an hour.
	orphan := filepath.Join(s.dir, "20261101-000000-000000")
	if err := os.Mkdir(orphan, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(orphan, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	n, err := s.Cleanup(10 * 24 * time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("cleanup removed %d (%v), want 1", n, err)
	}
	if _, err := s.Get(old.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("old scan still there")
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphan directory kept")
	}
	if err := s.Delete(fresh.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(fresh.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

// checkPDF verifies the structure a reader relies on: the page count and
// that every xref offset points at its object.
func checkPDF(t *testing.T, pdf []byte, pages int) {
	t.Helper()
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4\n")) || !bytes.HasSuffix(pdf, []byte("%%EOF\n")) {
		t.Fatalf("not a PDF: %q ... %q", pdf[:20], pdf[len(pdf)-10:])
	}
	if got := bytes.Count(pdf, []byte("/Type /Page ")); got != pages {
		t.Errorf("%d pages, want %d", got, pages)
	}
	m := regexp.MustCompile(`startxref\n(\d+)\n`).FindSubmatch(pdf)
	if m == nil {
		t.Fatal("no startxref")
	}
	xref, _ := strconv.Atoi(string(m[1]))
	if !bytes.HasPrefix(pdf[xref:], []byte("xref\n")) {
		t.Fatalf("startxref %d does not point at xref", xref)
	}
	entries := regexp.MustCompile(`(\d{10}) 00000 n `).FindAllSubmatch(pdf[xref:], -1)
	if len(entries) != 2+3*pages {
		t.Fatalf("%d xref entries, want %d", len(entries), 2+3*pages)
	}
	for i, e := range entries {
		off, _ := strconv.Atoi(string(e[1]))
		want := strconv.Itoa(i+1) + " 0 obj"
		if !bytes.HasPrefix(pdf[off:], []byte(want)) {
			t.Errorf("xref entry %d points at %q, want %q", i+1, pdf[off:off+12], want)
		}
	}
}

func TestDownloadSurvivesConcurrentDelete(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	d, err := s.New(ctx, Options{Resolution: 150, Color: "color", Format: "pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPage(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	doc, files, err := s.openPages(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The scan is deleted after its pages were opened but before writing.
	if err := s.Delete(d.ID); err != nil {
		t.Fatal(err)
	}
	pages := []pdfPage{{f: files[0], dpi: doc.Resolution}, {f: files[1], dpi: doc.Resolution}}
	var buf bytes.Buffer
	if err := writePDF(&buf, pages); err != nil {
		t.Fatalf("writing from open files after delete: %v", err)
	}
	checkPDF(t, buf.Bytes(), 2)
	for _, f := range files {
		f.Close()
	}
}
