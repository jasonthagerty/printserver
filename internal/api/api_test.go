package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jasonthagerty/printserver/internal/cups"
	"github.com/jasonthagerty/printserver/internal/cups/cupstest"
	"github.com/jasonthagerty/printserver/internal/destinations"
	"github.com/jasonthagerty/printserver/internal/escl"
	"github.com/jasonthagerty/printserver/internal/escl/escltest"
	"github.com/jasonthagerty/printserver/internal/scans"
	"github.com/jasonthagerty/printserver/internal/settings"
	"github.com/jasonthagerty/printserver/internal/supplies"
	"github.com/jasonthagerty/printserver/internal/usbprobe"
)

type harness struct {
	t       *testing.T
	handler http.Handler
	cups    *cupstest.Server
	scanner *escltest.Scanner
	deps    Deps
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	cs := cupstest.New()
	t.Cleanup(cs.Close)
	sc := escltest.New()
	t.Cleanup(sc.Close)
	log := slog.New(slog.DiscardHandler)
	dir := t.TempDir()
	scanner := escl.New(sc.Base(), nil)
	store, err := scans.NewStore(filepath.Join(dir, "scans"), scanner, log)
	if err != nil {
		t.Fatal(err)
	}
	st, err := settings.Open(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	sup, err := supplies.Open(filepath.Join(dir, "supplies.json"))
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{
		Version: "test", Device: "test", Log: log,
		CUPS: cups.NewHTTP(cs.URL, "MG3620", nil), Scanner: scanner, Scans: store,
		Settings: st, Nextcloud: destinations.NewNextcloud(nil), Supplies: sup,
		WebFS:    fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Printer</title>")}},
		Hostname: "printserver", ExtraHosts: []string{"print.home.example"},
	}
	return &harness{t: t, handler: NewRouter(deps), cups: cs, scanner: sc, deps: deps}
}

func (h *harness) do(method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequestWithContext(h.t.Context(), method, "http://printserver.local"+path, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func (h *harness) json(method, path string, body any, want int, out any) {
	h.t.Helper()
	var r io.Reader
	hdr := map[string]string{}
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
		hdr["Content-Type"] = "application/json"
	}
	rec := h.do(method, path, r, hdr)
	if rec.Code != want {
		h.t.Fatalf("%s %s = %d, want %d: %s", method, path, rec.Code, want, rec.Body)
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			h.t.Fatalf("%s %s: %v: %s", method, path, err, rec.Body)
		}
	}
}

func upload(t *testing.T, name string, content []byte, fields map[string]string) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(content)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func TestStatus(t *testing.T) {
	h := newHarness(t)
	h.cups.SetReasons("media-empty-error", "cups-waiting-for-job-completed")
	var st StatusOutput
	h.json(http.MethodGet, "/api/v1/status", nil, http.StatusOK, &st.Body)
	p := st.Body.Printer
	if p == nil || p.Name != "MG3620" || !p.Connected {
		t.Fatalf("printer = %+v", p)
	}
	if len(p.Reasons) != 1 || p.Reasons[0].Code != "media-empty" || p.Reasons[0].Severity != "error" || !strings.Contains(p.Reasons[0].Text, "paper") {
		t.Errorf("reasons = %+v (CUPS internals must be dropped)", p.Reasons)
	}
	if !st.Body.Scanner.Available || st.Body.Scanner.Model != "Canon PIXMA MG3600 Series" {
		t.Errorf("scanner = %+v", st.Body.Scanner)
	}

	h.cups.SetReasons("offline-report")
	h.json(http.MethodGet, "/api/v1/status", nil, http.StatusOK, &st.Body)
	if st.Body.Printer.Connected {
		t.Error("offline printer reported connected")
	}
}

func TestStatusWhenServicesDown(t *testing.T) {
	h := newHarness(t)
	h.cups.Close()
	h.scanner.Close()
	var st StatusOutput
	h.json(http.MethodGet, "/api/v1/status", nil, http.StatusOK, &st.Body)
	if st.Body.Printer != nil || st.Body.PrinterError == "" || st.Body.Scanner.Available {
		t.Errorf("status = %+v", st.Body)
	}
}

func TestPrintUploadAndCancel(t *testing.T) {
	h := newHarness(t)
	body, ct := upload(t, "../../etc/report.pdf", []byte("%PDF-1.4\n%fake"), map[string]string{"copies": "2", "color": "monochrome"})
	rec := h.do(http.MethodPost, "/api/v1/jobs", body, map[string]string{"Content-Type": ct, "Sec-Fetch-Site": "same-origin", "Origin": "http://printserver.local"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("print = %d: %s", rec.Code, rec.Body)
	}
	jobs := h.cups.Received()
	if len(jobs) != 1 || jobs[0].Format != "application/pdf" || jobs[0].Name != "report.pdf" {
		t.Fatalf("job = %+v", jobs)
	}

	var list JobsOutput
	h.json(http.MethodGet, "/api/v1/jobs", nil, http.StatusOK, &list.Body)
	if len(list.Body.Active) != 1 || !list.Body.Active[0].Cancelable {
		t.Fatalf("jobs = %+v", list.Body)
	}
	h.json(http.MethodDelete, "/api/v1/jobs/1", nil, http.StatusNoContent, nil)
	h.json(http.MethodDelete, "/api/v1/jobs/1", nil, http.StatusConflict, nil)
	h.json(http.MethodDelete, "/api/v1/jobs/42", nil, http.StatusNotFound, nil)
}

func TestPrintRejectsUnknownFileTypes(t *testing.T) {
	h := newHarness(t)
	for name, content := range map[string][]byte{
		"empty.pdf": {},
		"a.zip":     {'P', 'K', 3, 4, 0, 0, 0, 0},
		"a.html":    []byte("<html><script>alert(1)</script>"),
	} {
		body, ct := upload(t, name, content, nil)
		rec := h.do(http.MethodPost, "/api/v1/jobs", body, map[string]string{"Content-Type": ct})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d, want 422: %s", name, rec.Code, rec.Body)
		}
	}
	if n := len(h.cups.Received()); n != 0 {
		t.Errorf("%d jobs reached CUPS", n)
	}
}

func TestPrintOptions(t *testing.T) {
	h := newHarness(t)
	var out PrintOptionsOutput
	h.json(http.MethodGet, "/api/v1/print-options", nil, http.StatusOK, &out.Body)
	var media []string
	for _, m := range out.Body.Media {
		media = append(media, m.Label)
	}
	if strings.Join(media, ",") != "Letter,A4,4 × 6 photo" {
		t.Errorf("media = %v (unknown sizes must be hidden)", media)
	}
	if out.Body.MediaDefault != "na_letter_8.5x11in" || len(out.Body.Sides) != 3 || len(out.Body.Qualities) != 3 {
		t.Errorf("options = %+v", out.Body)
	}
}

func TestScanFlow(t *testing.T) {
	h := newHarness(t)
	var doc ScanDoc
	h.json(http.MethodPost, "/api/v1/scans", map[string]any{"resolution": 300, "color": "gray", "format": "pdf"}, http.StatusCreated, &doc)
	h.json(http.MethodPost, "/api/v1/scans/"+doc.ID+"/pages", nil, http.StatusOK, &doc)
	if doc.Pages != 2 || doc.Color != "gray" {
		t.Fatalf("doc = %+v", doc)
	}
	rec := h.do(http.MethodGet, "/api/v1/scans/"+doc.ID+"/file?download=true", nil, nil)
	if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("download = %d %q", rec.Code, rec.Body.Bytes()[:min(20, rec.Body.Len())])
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	rec = h.do(http.MethodGet, "/api/v1/scans/"+doc.ID+"/pages/2", nil, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("page image = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	h.json(http.MethodPatch, "/api/v1/scans/"+doc.ID, map[string]string{"name": "Receipts"}, http.StatusOK, &doc)
	if doc.FileName != "Receipts.pdf" {
		t.Errorf("file name %q", doc.FileName)
	}

	var printed PrintOutput
	h.json(http.MethodPost, "/api/v1/scans/"+doc.ID+"/print", map[string]any{"copies": 1, "color": "monochrome"}, http.StatusCreated, &printed.Body)
	if jobs := h.cups.Received(); len(jobs) != 1 || jobs[0].Format != "application/pdf" {
		t.Errorf("print scan sent %+v", jobs)
	}

	h.json(http.MethodPost, "/api/v1/scans/"+doc.ID+"/send", map[string]string{"destination": "nextcloud"}, http.StatusConflict, nil)

	var list ScansOutput
	h.json(http.MethodGet, "/api/v1/scans", nil, http.StatusOK, &list.Body)
	if len(list.Body.Scans) != 1 {
		t.Errorf("scans = %+v", list.Body.Scans)
	}
	h.json(http.MethodDelete, "/api/v1/scans/"+doc.ID, nil, http.StatusNoContent, nil)
	h.json(http.MethodGet, "/api/v1/scans/"+doc.ID+"/file", nil, http.StatusNotFound, nil)
	h.json(http.MethodGet, "/api/v1/scans/..%2F..%2Fetc/file", nil, http.StatusUnprocessableEntity, nil)
}

func TestScannerBusy(t *testing.T) {
	h := newHarness(t)
	h.scanner.Busy = true
	h.json(http.MethodPost, "/api/v1/scans", map[string]any{"resolution": 300, "color": "color", "format": "pdf"}, http.StatusConflict, nil)
}

func TestCopy(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPost, "/api/v1/copy", map[string]any{"copies": 3, "color": "monochrome"}, http.StatusCreated, nil)
	jobs := h.cups.Received()
	if len(jobs) != 1 || jobs[0].Format != "image/jpeg" || len(jobs[0].Data) == 0 {
		t.Fatalf("copy job = %+v", jobs)
	}
	if !strings.Contains(h.scanner.Settings[0], "Grayscale8") {
		t.Errorf("black-and-white copy scanned in color")
	}
}

func TestSettingsPasswordIsWriteOnly(t *testing.T) {
	h := newHarness(t)
	var out SettingsOutput
	h.json(http.MethodPut, "/api/v1/settings", map[string]any{
		"retention_days": 14,
		"nextcloud":      map[string]any{"url": "https://cloud.example.com", "user": "me", "folder": "Scans", "password": "app-secret"},
	}, http.StatusOK, &out.Body)
	if !out.Body.Nextcloud.PasswordSet || !out.Body.Nextcloud.Configured || out.Body.RetentionDays != 14 {
		t.Fatalf("settings = %+v", out.Body)
	}
	rec := h.do(http.MethodGet, "/api/v1/settings", nil, nil)
	if strings.Contains(rec.Body.String(), "app-secret") {
		t.Fatal("password returned by the API")
	}

	// Saving without a password keeps it.
	h.json(http.MethodPut, "/api/v1/settings", map[string]any{
		"retention_days": 14,
		"nextcloud":      map[string]any{"url": "https://cloud.example.com/", "user": "me", "folder": "Docs"},
	}, http.StatusOK, &out.Body)
	if h.deps.Settings.Get().Nextcloud.Password != "app-secret" {
		t.Error("password lost on save")
	}
	// Pointing at another server drops it, so it is not sent there.
	h.json(http.MethodPut, "/api/v1/settings", map[string]any{
		"retention_days": 14,
		"nextcloud":      map[string]any{"url": "https://other.example.com", "user": "me", "folder": "Docs"},
	}, http.StatusOK, &out.Body)
	if out.Body.Nextcloud.PasswordSet {
		t.Error("password kept for a different server")
	}
	h.json(http.MethodPut, "/api/v1/settings", map[string]any{"retention_days": 0, "nextcloud": map[string]any{}}, http.StatusUnprocessableEntity, nil)
}

func TestHostAndOriginChecks(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		host    string
		headers map[string]string
		method  string
		want    int
	}{
		{"printserver.local", nil, http.MethodGet, http.StatusOK},
		{"PrintServer.local.", nil, http.MethodGet, http.StatusOK},
		{"192.168.252.30:80", nil, http.MethodGet, http.StatusOK},
		{"[fe80::1]", nil, http.MethodGet, http.StatusOK},
		{"print.home.example", nil, http.MethodGet, http.StatusOK},
		{"rebound.example", nil, http.MethodGet, http.StatusMisdirectedRequest},
		{"printserver.local", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.MethodDelete, http.StatusForbidden},
		{"printserver.local", map[string]string{"Origin": "http://evil.example"}, http.MethodDelete, http.StatusForbidden},
		{"printserver.local", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://printserver.local"}, http.MethodDelete, http.StatusNotFound},
		{"printserver.local", nil, http.MethodDelete, http.StatusNotFound}, // curl and scripts
	}
	for _, c := range cases {
		path := "/api/v1/health"
		if c.method == http.MethodDelete {
			path = "/api/v1/jobs/99"
		}
		req := httptest.NewRequestWithContext(t.Context(), c.method, "http://x"+path, nil)
		req.Host = c.host
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s %s %v = %d, want %d", c.method, c.host, c.headers, rec.Code, c.want)
		}
	}
}

func TestSPAFallback(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/scan", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>Printer") {
		t.Errorf("SPA route = %d %s", rec.Code, rec.Body)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP = %q", csp)
	}
	if rec := h.do(http.MethodGet, "/assets/missing.js", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing asset = %d", rec.Code)
	}
}

func TestStatusReportsUSBState(t *testing.T) {
	h := newHarness(t)
	root := t.TempDir()
	h.deps.USB = &usbprobe.Probe{Vendor: "04a9", SysRoot: filepath.Join(root, "sys"), DevRoot: filepath.Join(root, "dev")}
	h.handler = NewRouter(h.deps)
	var st StatusOutput

	// No sysfs at all: nothing to say.
	h.json(http.MethodGet, "/api/v1/status", nil, http.StatusOK, &st.Body)
	if st.Body.Printer.USB != "unknown" || !st.Body.Printer.Connected {
		t.Errorf("unknown: %+v", st.Body.Printer)
	}

	// The printer is on the bus, but the container has no node for it.
	dev := filepath.Join(root, "sys", "bus", "usb", "devices", "1-1.2")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, v := range map[string]string{"idVendor": "04a9", "busnum": "1", "devnum": "6"} {
		if err := os.WriteFile(filepath.Join(dev, f), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h.json(http.MethodGet, "/api/v1/status", nil, http.StatusOK, &st.Body)
	p := st.Body.Printer
	if p.USB != "stale" || p.Connected || len(p.Reasons) != 1 || p.Reasons[0].Code != "usb-reconnected" {
		t.Errorf("stale: %+v", p)
	}
}

func TestSupplies(t *testing.T) {
	h := newHarness(t)
	var out SuppliesOutput
	h.json(http.MethodGet, "/api/v1/supplies", nil, http.StatusOK, &out.Body)
	if len(out.Body.Cartridges) != 2 || out.Body.Cartridges[0].AgeDays != -1 || out.Body.LastPrinted != nil || out.Body.IdleDays != -1 {
		t.Fatalf("fresh = %+v", out.Body)
	}

	h.json(http.MethodPut, "/api/v1/supplies/cartridges/black", map[string]string{"installed_on": "2020-01-01"}, http.StatusOK, &out.Body)
	if c := out.Body.Cartridges[0]; !c.Old || c.AgeDays < 365 {
		t.Errorf("old cartridge = %+v", c)
	}
	h.json(http.MethodPost, "/api/v1/supplies/cartridges/black/replaced", nil, http.StatusOK, &out.Body)
	if c := out.Body.Cartridges[0]; c.Old || c.AgeDays != 0 {
		t.Errorf("replaced cartridge = %+v", c)
	}
	h.json(http.MethodPut, "/api/v1/supplies/cartridges/black", map[string]string{"installed_on": "2999-01-01"}, http.StatusUnprocessableEntity, nil)
	h.json(http.MethodPut, "/api/v1/supplies/cartridges/magenta", map[string]string{"installed_on": ""}, http.StatusUnprocessableEntity, nil)

	h.json(http.MethodPut, "/api/v1/supplies/thresholds", map[string]int{"warn_months": 3, "nudge_days": 7}, http.StatusOK, &out.Body)
	if out.Body.WarnMonths != 3 || out.Body.NudgeDays != 7 {
		t.Errorf("thresholds = %+v", out.Body)
	}

	// A print long ago makes the reminder show.
	if _, err := h.deps.Supplies.RecordPrint(1, time.Now().AddDate(0, 0, -10)); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodGet, "/api/v1/supplies", nil, http.StatusOK, &out.Body)
	if out.Body.IdleDays != 10 || !out.Body.Nudge {
		t.Errorf("idle = %d nudge = %v", out.Body.IdleDays, out.Body.Nudge)
	}

	var job PrintOutput
	h.json(http.MethodPost, "/api/v1/supplies/test-page", nil, http.StatusCreated, &job.Body)
	jobs := h.cups.Received()
	if len(jobs) != 1 || jobs[0].Format != "application/pdf" || !bytes.HasPrefix(jobs[0].Data, []byte("%PDF-")) {
		t.Errorf("test page job = %+v", jobs)
	}
}

func TestSuppliesShowInkReading(t *testing.T) {
	h := newHarness(t)
	h.cups.SetLevels(10, 90)
	uri := strings.Replace(h.cups.URL, "http://", "ipp://", 1) + "/ipp/print"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go h.deps.Supplies.PollInk(ctx, uri, time.Hour, h.deps.Log)

	var out SuppliesOutput
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.json(http.MethodGet, "/api/v1/supplies", nil, http.StatusOK, &out.Body)
		if out.Body.InkChecked != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if out.Body.InkChecked == nil {
		t.Fatal("no ink reading")
	}
	black, color := out.Body.Cartridges[0], out.Body.Cartridges[1]
	if black.Level != 10 || !black.Low || color.Level != 90 || color.Low {
		t.Errorf("black %+v, color %+v", black, color)
	}
}
