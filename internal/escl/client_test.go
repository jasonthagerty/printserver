package escl_test

import (
	"context"
	"errors"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jasonthagerty/printserver/internal/escl"
	"github.com/jasonthagerty/printserver/internal/escl/escltest"
)

func TestCapabilities(t *testing.T) {
	s := escltest.New()
	defer s.Close()
	caps, err := escl.New(s.Base(), nil).Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if caps.MakeAndModel != "Canon PIXMA MG3600 Series" || caps.MaxWidth != 2550 || caps.MaxHeight != 3508 {
		t.Errorf("caps = %+v", caps)
	}
	if !slices.Equal(caps.Resolutions, []int{75, 150, 300, 600, 1200}) {
		t.Errorf("resolutions = %v", caps.Resolutions)
	}
	if !slices.Equal(caps.ColorModes, []string{"RGB24", "Grayscale8"}) {
		t.Errorf("color modes = %v", caps.ColorModes)
	}
	if !slices.Equal(caps.Formats, []string{"image/jpeg", "application/pdf"}) {
		t.Errorf("formats = %v (duplicates must be dropped)", caps.Formats)
	}
}

func TestScan(t *testing.T) {
	s := escltest.New()
	defer s.Close()
	c := escl.New(s.Base(), nil)
	body, ct, err := c.Scan(context.Background(), escl.Settings{Resolution: 300, ColorMode: "Grayscale8", Format: "image/jpeg", Width: 2550, Height: 3508})
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if ct != "image/jpeg" {
		t.Errorf("content type %q", ct)
	}
	if _, err := jpeg.Decode(body); err != nil {
		t.Errorf("not a JPEG: %v", err)
	}
	sent := s.Settings[0]
	for _, want := range []string{"<pwg:Width>2550</pwg:Width>", "<scan:XResolution>300</scan:XResolution>", "<scan:ColorMode>Grayscale8</scan:ColorMode>", "<pwg:InputSource>Platen</pwg:InputSource>"} {
		if !strings.Contains(sent, want) {
			t.Errorf("settings lack %s:\n%s", want, sent)
		}
	}
}

func TestScanBusy(t *testing.T) {
	s := escltest.New()
	defer s.Close()
	s.Busy = true
	_, _, err := escl.New(s.Base(), nil).Scan(context.Background(), escl.Settings{Resolution: 300, ColorMode: "RGB24", Format: "image/jpeg", Width: 1, Height: 1})
	if !errors.Is(err, escl.ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
}

func TestScanRejectsInjection(t *testing.T) {
	_, _, err := escl.New("http://127.0.0.1:1/eSCL", nil).Scan(context.Background(), escl.Settings{Resolution: 300, ColorMode: "RGB24</scan:ColorMode><x>", Format: "image/jpeg", Width: 1, Height: 1})
	if err == nil || errors.Is(err, escl.ErrUnavailable) {
		t.Fatalf("err = %v, want a validation error before any request", err)
	}
}

func TestJobOnOtherHostRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://evil.example/eSCL/ScanJobs/1")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	_, _, err := escl.New(srv.URL+"/eSCL", nil).Scan(context.Background(), escl.Settings{Resolution: 300, ColorMode: "RGB24", Format: "image/jpeg", Width: 1, Height: 1})
	if err == nil || !strings.Contains(err.Error(), "another host") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnavailable(t *testing.T) {
	c := escl.New("http://127.0.0.1:1/eSCL", nil)
	if _, err := c.Status(context.Background()); !errors.Is(err, escl.ErrUnavailable) {
		t.Errorf("status err = %v", err)
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := escl.New(srv.URL+"/eSCL", nil).Capabilities(context.Background()); !errors.Is(err, escl.ErrUnavailable) {
		t.Errorf("404 err = %v", err)
	}
}
