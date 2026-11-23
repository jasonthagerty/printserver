package cups_test

import (
	"context"
	"strings"
	"testing"

	"github.com/OpenPrinting/goipp"

	"github.com/jasonthagerty/printserver/internal/cups"
	"github.com/jasonthagerty/printserver/internal/cups/cupstest"
)

func newClient(t *testing.T) (*cups.Client, *cupstest.Server) {
	t.Helper()
	srv := cupstest.New()
	t.Cleanup(srv.Close)
	return cups.NewHTTP(srv.URL, "MG3620", nil), srv
}

func TestPrinter(t *testing.T) {
	c, srv := newClient(t)
	srv.SetReasons("media-empty-error", "none")
	p, err := c.Printer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "MG3620" || p.State != "idle" || !p.Accepting {
		t.Errorf("printer = %+v", p)
	}
	if len(p.StateReasons) != 1 || p.StateReasons[0] != "media-empty-error" {
		t.Errorf("reasons = %v, want [media-empty-error] (none dropped)", p.StateReasons)
	}
	if len(p.Markers) != 2 || p.Markers[0].Level != 40 || p.Markers[0].Color != "#000000" {
		t.Errorf("markers = %+v", p.Markers)
	}
	if got := strings.Join(p.Qualities, ","); got != "draft,normal,high" {
		t.Errorf("qualities = %s", got)
	}
}

func TestMarkerLevelOutOfRangeIsUnknown(t *testing.T) {
	c, srv := newClient(t)
	srv.MarkerLevels = []int{-1, 101}
	p, err := c.Printer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range p.Markers {
		if m.Level != -1 {
			t.Errorf("marker %s level = %d, want -1", m.Name, m.Level)
		}
	}
}

func TestPrintSendsOptionsAndDocument(t *testing.T) {
	c, srv := newClient(t)
	id, err := c.Print(context.Background(), strings.NewReader("%PDF-1.4 hello"), cups.PrintOptions{
		Name: "a.pdf", Format: "application/pdf", Copies: 2, ColorMode: "monochrome",
		Sides: "two-sided-long-edge", Media: "na_letter_8.5x11in", Quality: "high",
		Scaling: "fit", PageRange: "1-2, 4",
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs := srv.Received()
	if id != 1 || len(jobs) != 1 {
		t.Fatalf("id=%d jobs=%d", id, len(jobs))
	}
	j := jobs[0]
	if string(j.Data) != "%PDF-1.4 hello" || j.Format != "application/pdf" || j.Name != "a.pdf" {
		t.Errorf("job = %+v", j)
	}
	got := map[string]string{}
	for _, a := range j.JobAttrs {
		got[a.Name] = a.Values.String()
	}
	want := map[string]string{
		"copies": "2", "print-color-mode": "monochrome", "sides": "two-sided-long-edge",
		"media": "na_letter_8.5x11in", "print-quality": "5", "print-scaling": "fit",
		"page-ranges": "[1-2,4-4]",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestPrintDefaultsLeaveQueueSettings(t *testing.T) {
	c, srv := newClient(t)
	if _, err := c.Print(context.Background(), strings.NewReader("x"), cups.PrintOptions{Copies: 1}); err != nil {
		t.Fatal(err)
	}
	if attrs := srv.Received()[0].JobAttrs; len(attrs) != 0 {
		t.Errorf("job attributes sent for defaults: %v", attrs)
	}
}

func TestBadPageRange(t *testing.T) {
	c, srv := newClient(t)
	for _, r := range []string{"0", "3-1", "a", ",", "1-b"} {
		if _, err := c.Print(context.Background(), strings.NewReader("x"), cups.PrintOptions{PageRange: r}); err == nil {
			t.Errorf("page range %q accepted", r)
		}
	}
	if n := srv.Requests[goipp.OpPrintJob]; n != 0 {
		t.Errorf("%d print requests sent for invalid ranges", n)
	}
}

func TestJobsAndCancel(t *testing.T) {
	c, srv := newClient(t)
	ctx := context.Background()
	for _, n := range []string{"one", "two", "three"} {
		if _, err := c.Print(ctx, strings.NewReader("x"), cups.PrintOptions{Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	srv.Complete(1)
	srv.Complete(2)
	active, err := c.Jobs(ctx, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != 3 || active[0].State != "pending" || !active[0].Active() {
		t.Errorf("active = %+v", active)
	}
	done, err := c.Jobs(ctx, true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 2 || done[0].ID != 2 || done[1].ID != 1 {
		t.Errorf("completed = %+v, want newest first", done)
	}

	if err := c.Cancel(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if err := c.Cancel(ctx, 3); !cups.IsNotPossible(err) {
		t.Errorf("second cancel err = %v, want not-possible", err)
	}
	if err := c.Cancel(ctx, 99); !cups.IsNotFound(err) {
		t.Errorf("cancel unknown err = %v, want not-found", err)
	}
}

func TestUnreachable(t *testing.T) {
	c := cups.NewSocket("/nonexistent/cups.sock", "MG3620")
	if _, err := c.Printer(context.Background()); err == nil {
		t.Fatal("no error for a missing socket")
	}
}

func TestRecentJobsAreTheNewest(t *testing.T) {
	c, srv := newClient(t)
	ctx := context.Background()
	for i := 1; i <= 12; i++ {
		if _, err := c.Print(ctx, strings.NewReader("x"), cups.PrintOptions{}); err != nil {
			t.Fatal(err)
		}
		srv.Complete(i)
	}
	done, err := c.Jobs(ctx, true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 10 || done[0].ID != 12 || done[9].ID != 3 {
		ids := []int{}
		for _, j := range done {
			ids = append(ids, j.ID)
		}
		t.Fatalf("recent = %v, want 12 down to 3", ids)
	}
}

func TestPrinterMarkers(t *testing.T) {
	srv := cupstest.New()
	defer srv.Close()
	uri := strings.Replace(srv.URL, "http://", "ipp://", 1) + "/ipp/print"
	m, err := cups.PrinterMarkers(context.Background(), nil, uri)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || m[0].Name != "Black" || m[0].Level != 40 || m[0].Low != 15 || m[1].Level != 75 {
		t.Errorf("markers = %+v", m)
	}
	if _, err := cups.PrinterMarkers(context.Background(), nil, "ftp://x/"); err == nil {
		t.Error("accepted ftp")
	}
}
