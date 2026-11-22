package airscan_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonthagerty/printserver/internal/airscan"
	"github.com/jasonthagerty/printserver/internal/api"
	"github.com/jasonthagerty/printserver/internal/escl"
	"github.com/jasonthagerty/printserver/internal/escl/escltest"
)

func TestAnnouncerWritesAndRemovesRecord(t *testing.T) {
	fake := escltest.New()
	dir := t.TempDir()
	reloads := 0
	a := &airscan.Announcer{
		Scanner: escl.New(fake.Base(), nil), Dir: dir, Port: 8090, Hostname: "printserver", Name: "Canon MG3620 Scanner",
		Log:    slog.New(slog.DiscardHandler),
		Reload: func(context.Context) error { reloads++; return nil },
	}
	ctx := context.Background()
	a.Update(ctx)
	b, err := os.ReadFile(filepath.Join(dir, "airscan.service"))
	if err != nil {
		t.Fatal(err)
	}
	rec := string(b)
	for _, want := range []string{
		"<type>_uscan._tcp</type>", "<port>8090</port>", "<name>Canon MG3620 Scanner</name>", "ty=Canon PIXMA MG3600 Series",
		"rs=eSCL", "cs=color,grayscale", "pdl=image/jpeg,application/pdf",
		"representation=http://printserver.local:8090/eSCL/ScannerIcon",
	} {
		if !strings.Contains(rec, want) {
			t.Errorf("record lacks %q:\n%s", want, rec)
		}
	}
	a.Update(ctx)
	if reloads != 1 {
		t.Errorf("%d reloads for an unchanged record, want 1", reloads)
	}

	fake.Close()
	a.Update(ctx)
	if _, err := os.Stat(filepath.Join(dir, "airscan.service")); !os.IsNotExist(err) {
		t.Errorf("record kept after the scanner went away")
	}
	if reloads != 2 {
		t.Errorf("reloads = %d, want 2", reloads)
	}
}

func TestProxyChecksHostAndOrigin(t *testing.T) {
	fake := escltest.New()
	defer fake.Close()
	target, _ := url.Parse(fake.URL)
	h := api.ESCLHandler(target, "printserver", nil, slog.New(slog.DiscardHandler))

	get := func(host string, hdr map[string]string, method string) int {
		req := httptest.NewRequestWithContext(t.Context(), method, "http://x/eSCL/ScannerCapabilities", nil)
		if method == http.MethodPost {
			req = httptest.NewRequestWithContext(t.Context(), method, "http://x/eSCL/ScanJobs", strings.NewReader("<x/>"))
		}
		req.Host = host
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		_, _ = io.Copy(io.Discard, rec.Body)
		return rec.Code
	}
	if code := get("printserver.local:8090", nil, http.MethodGet); code != http.StatusOK {
		t.Errorf("own name: %d", code)
	}
	if code := get("192.168.1.9:8090", nil, http.MethodGet); code != http.StatusOK {
		t.Errorf("IP address: %d", code)
	}
	if code := get("rebound.example:8090", nil, http.MethodGet); code != http.StatusMisdirectedRequest {
		t.Errorf("foreign name: %d, want 421", code)
	}
	if code := get("printserver.local:8090", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.MethodPost); code != http.StatusForbidden {
		t.Errorf("cross-site scan job: %d, want 403", code)
	}
	if code := get("printserver.local:8090", nil, http.MethodPost); code != http.StatusCreated {
		t.Errorf("scan job from a scanning app: %d, want 201", code)
	}
}
