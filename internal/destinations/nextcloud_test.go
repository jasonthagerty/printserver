package destinations

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jasonthagerty/printserver/internal/settings"
)

// fakeDAV is a minimal Nextcloud WebDAV endpoint for user "me".
type fakeDAV struct {
	mu      sync.Mutex
	dirs    map[string]bool
	files   map[string]string
	methods []string
}

func newFakeDAV(t *testing.T) (*httptest.Server, *fakeDAV) {
	f := &fakeDAV{dirs: map[string]bool{"/": true}, files: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.methods = append(f.methods, r.Method)
		user, pass, ok := r.BasicAuth()
		if !ok || user != "me" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		const prefix = "/remote.php/dav/files/me"
		p := strings.TrimPrefix(r.URL.Path, prefix)
		if p == r.URL.Path {
			http.NotFound(w, r)
			return
		}
		if p == "" {
			p = "/"
		}
		switch r.Method {
		case "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
		case "MKCOL":
			if f.dirs[p] {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			f.dirs[p] = true
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			if _, exists := f.files[p]; exists && r.Header.Get("If-None-Match") == "*" {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			b, _ := io.ReadAll(r.Body)
			f.files[p] = string(b)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, f
}

func TestUploadCreatesFolderAndAvoidsOverwrite(t *testing.T) {
	srv, f := newFakeDAV(t)
	nc := NewNextcloud(nil)
	cfg := settings.Nextcloud{URL: srv.URL, User: "me", Password: "secret", Folder: "Home/Scans"}
	ctx := context.Background()

	p1, err := nc.Upload(ctx, cfg, "Scan 1.pdf", strings.NewReader("one"), "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := nc.Upload(ctx, cfg, "Scan 1.pdf", strings.NewReader("two"), "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if p1 != "/Home/Scans/Scan 1.pdf" || p2 != "/Home/Scans/Scan 1 (1).pdf" {
		t.Errorf("paths %q, %q", p1, p2)
	}
	if !f.dirs["/Home"] || !f.dirs["/Home/Scans"] {
		t.Errorf("folders not created: %v", f.dirs)
	}
	if f.files["/Home/Scans/Scan 1.pdf"] != "one" || f.files["/Home/Scans/Scan 1 (1).pdf"] != "two" {
		t.Errorf("files = %v", f.files)
	}
}

func TestTestReportsBadPassword(t *testing.T) {
	srv, _ := newFakeDAV(t)
	nc := NewNextcloud(nil)
	err := nc.Test(context.Background(), settings.Nextcloud{URL: srv.URL, User: "me", Password: "wrong", Folder: "Scans"})
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("err = %v", err)
	}
	if err := nc.Test(context.Background(), settings.Nextcloud{URL: srv.URL, User: "me", Password: "secret", Folder: "Scans"}); err != nil {
		t.Fatalf("good credentials: %v", err)
	}
}

func TestNotConfigured(t *testing.T) {
	nc := NewNextcloud(nil)
	if _, err := nc.Upload(context.Background(), settings.Nextcloud{URL: "https://x"}, "a.pdf", strings.NewReader(""), "application/pdf"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v", err)
	}
}
