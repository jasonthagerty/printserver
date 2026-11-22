// Package api serves the printserver HTTP API and the embedded web GUI. API
// operations are declared with huma so the OpenAPI document is generated from
// the handlers, never written by hand.
//
// There is no login: the house network is the trust boundary. What protects
// the API instead is that browsers may only call it from its own pages (the
// same-origin check) and only under a name or address that belongs to this
// server (the Host check, against DNS rebinding).
package api

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/jasonthagerty/printserver/internal/airscan"
	"github.com/jasonthagerty/printserver/internal/config"
	"github.com/jasonthagerty/printserver/internal/cups"
	"github.com/jasonthagerty/printserver/internal/destinations"
	"github.com/jasonthagerty/printserver/internal/escl"
	"github.com/jasonthagerty/printserver/internal/scans"
	"github.com/jasonthagerty/printserver/internal/settings"
	"github.com/jasonthagerty/printserver/internal/usbprobe"
	"github.com/jasonthagerty/printserver/web"
)

// maxUpload bounds a print upload. Phone photos and long PDFs fit easily.
const maxUpload = 100 << 20

// Deps are the services handlers use. Tests build them with fakes.
type Deps struct {
	Version   string
	Device    string
	CUPS      *cups.Client
	Scanner   *escl.Client
	Scans     *scans.Store
	Settings  *settings.Store
	Nextcloud *destinations.Nextcloud
	Log       *slog.Logger
	// WebFS holds the built GUI (index.html at its root).
	WebFS fs.FS
	// Hostname is the server's mDNS name without .local.
	Hostname string
	// ExtraHosts are more names accepted in the Host header.
	ExtraHosts []string
	// USB checks the printer's USB connection; nil in test mode.
	USB *usbprobe.Probe
}

// App is the running server.
type App struct {
	deps Deps
}

// NewApp wires the real services from cfg and starts background work, which
// stops when ctx ends.
func NewApp(ctx context.Context, cfg config.Config, log *slog.Logger, version string) (*App, error) {
	var cc *cups.Client
	if cfg.CUPSURL != "" {
		cc = cups.NewHTTP(cfg.CUPSURL, cfg.Queue, nil)
	} else {
		cc = cups.NewSocket(cfg.CUPSSocket, cfg.Queue)
	}
	scanner := escl.New(cfg.ESCLURL, nil)
	store, err := scans.NewStore(filepath.Join(cfg.DataDir, "scans"), scanner, log)
	if err != nil {
		return nil, err
	}
	st, err := settings.Open(filepath.Join(cfg.DataDir, "settings.json"))
	if err != nil {
		return nil, err
	}
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return nil, err
	}
	var usb *usbprobe.Probe
	if cfg.Device == "usb" {
		usb = &usbprobe.Probe{Vendor: cfg.USBVendor, SysRoot: "/sys", DevRoot: "/dev"}
	}
	deps := Deps{
		USB:        usb,
		Version:    version,
		Device:     cfg.Device,
		CUPS:       cc,
		Scanner:    scanner,
		Scans:      store,
		Settings:   st,
		Nextcloud:  destinations.NewNextcloud(nil),
		Log:        log,
		WebFS:      dist,
		Hostname:   cfg.Hostname,
		ExtraHosts: cfg.ExtraHosts,
	}
	go cleanupLoop(ctx, deps)
	return &App{deps: deps}, nil
}

// Handler is the full HTTP handler.
func (a *App) Handler() http.Handler { return NewRouter(a.deps) }

// NewRouter builds the HTTP handler: /api/v1 operations, the OpenAPI
// document at /api/v1/openapi.json, and the GUI for every other path.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders, allowHosts(deps.Hostname, deps.ExtraHosts))
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(noStore, sameOrigin, limitBody(maxUpload+1<<20))
		api := humachi.New(r, humaConfig(deps.Version))
		register(api, deps)
	})
	r.Handle("/*", spaHandler(deps.WebFS))
	return r
}

// ESCLHandler serves the scanner's eSCL API to the network: the Host and
// same-origin checks, then a proxy to AirSane (which has neither).
func ESCLHandler(target *url.URL, hostname string, extra []string, log *slog.Logger) http.Handler {
	return allowHosts(hostname, extra)(sameOrigin(airscan.Proxy(target, log)))
}

// OpenAPI returns the generated document without serving anything.
func OpenAPI(version string) *huma.OpenAPI {
	api := humachi.New(chi.NewRouter(), humaConfig(version))
	register(api, Deps{Version: version})
	return api.OpenAPI()
}

// WriteOpenAPI writes the document as indented JSON, for the checked-in
// contract.
func WriteOpenAPI(w io.Writer, version string) error {
	b, err := json.MarshalIndent(OpenAPI(version), "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

func init() {
	// Every handler returns empty slices rather than nil, so arrays in the
	// contract are never null and the GUI need not check.
	huma.DefaultArrayNullable = false
}

func humaConfig(version string) huma.Config {
	cfg := huma.DefaultConfig("printserver API", version)
	cfg.Servers = []*huma.Server{{URL: "/api/v1"}}
	cfg.OpenAPIPath = "/openapi"
	cfg.DocsPath = ""
	// Schemas are named after their Go types; no $schema links in bodies.
	cfg.CreateHooks = nil
	return cfg
}

func register(api huma.API, deps Deps) {
	registerStatus(api, deps)
	registerJobs(api, deps)
	registerScans(api, deps)
	registerSettings(api, deps)
}

func cleanupLoop(ctx context.Context, deps Deps) {
	run := func() {
		days := deps.Settings.Get().RetentionDays
		n, err := deps.Scans.Cleanup(time.Duration(days) * 24 * time.Hour)
		if err != nil {
			deps.Log.Warn("scan cleanup failed", "err", err)
		} else if n > 0 {
			deps.Log.Info("removed old scans", "count", n, "retention_days", days)
		}
	}
	run()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// allowHosts rejects requests whose Host header is not this server: an IP
// address, localhost, its mDNS name, or a configured extra name. A web page
// that rebinds its own DNS name to this server's address is refused, so it
// cannot read scans through the victim's browser.
func allowHosts(hostname string, extra []string) func(http.Handler) http.Handler {
	allowed := map[string]bool{"localhost": true}
	if hostname != "" {
		allowed[strings.ToLower(hostname)] = true
		allowed[strings.ToLower(hostname)+".local"] = true
	}
	for _, h := range extra {
		allowed[strings.ToLower(strings.TrimSpace(h))] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
			if net.ParseIP(host) == nil && !allowed[host] {
				http.Error(w, "unknown host name; add it to PRINTSERVER_ALLOWED_HOSTS", http.StatusMisdirectedRequest)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sameOrigin refuses state-changing requests that a browser sent from
// another site's page. Browsers mark such requests with Sec-Fetch-Site or
// an Origin header; non-browser clients (curl, scripts) send neither and
// are allowed, since anyone on the network may use the API.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		switch r.Header.Get("Sec-Fetch-Site") {
		case "", "same-origin", "none":
		default:
			writeProblem(w, http.StatusForbidden, "requests from other sites are not allowed")
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			u, err := url.Parse(o)
			if err != nil || !strings.EqualFold(u.Host, r.Host) {
				writeProblem(w, http.StatusForbidden, "request origin does not match this server")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func limitBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// securityHeaders sets headers every response gets. The GUI loads nothing
// from outside the server, so the CSP allows only same-origin content.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; frame-src 'self' blob:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		next.ServeHTTP(w, r)
	})
}

// noStore keeps API responses, which can carry scanned documents, out of
// caches.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
	})
}
