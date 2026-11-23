// Package airscan exposes the scanner to iPhones, Macs and Windows (eSCL,
// "AirScan") on behalf of AirSane. AirSane has no Host-header check, so a
// web page that rebinds its DNS name to the printer could otherwise start
// a scan and read the glass through a visitor's browser. AirSane therefore
// listens on loopback only; Proxy serves the network port with the same
// checks as the web API, and Announcer publishes the mDNS record AirSane
// would have.
package airscan

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/jasonthagerty/printserver/internal/avahi"
	"github.com/jasonthagerty/printserver/internal/escl"
)

// Proxy forwards eSCL requests to AirSane at target (e.g.
// http://127.0.0.1:18090). Wrap it in the Host and same-origin checks.
func Proxy(target *url.URL, log *slog.Logger) http.Handler {
	rp := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			// AirSane answers with paths only; keep the client's Host for
			// any absolute URL it builds.
			r.Out.Host = r.In.Host
		},
		// Scans stream for up to a minute; send bytes as they come.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Warn("eSCL proxy", "path", r.URL.Path, "err", err)
			http.Error(w, "scanner unavailable", http.StatusServiceUnavailable)
		},
	}
	return rp
}

// Announcer keeps an Avahi service file for the scanner in step with what
// AirSane reports: written when a scanner is there, removed when not.
type Announcer struct {
	Scanner  *escl.Client
	Dir      string // Avahi's services directory
	Port     int    // the proxy's port, which phones connect to
	Hostname string // mDNS host name, without .local
	// Name is what scanning apps list. It must differ from AirSane's own
	// loopback record (the scanner's model), or Avahi renames one of them.
	Name string
	Log  *slog.Logger
	// AdminURL is the web UI's scan page, linked from scanning apps.
	AdminURL string
	// Reload makes Avahi reread its service files. Defaults to
	// `avahi-daemon --reload`.
	Reload func(ctx context.Context) error
}

const fileName = "airscan.service"

// Run updates the record now and then every interval until ctx ends.
func (a *Announcer) Run(ctx context.Context, interval time.Duration) {
	a.Update(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.Update(ctx)
		}
	}
}

// Update writes or removes the service file to match the scanner.
func (a *Announcer) Update(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var want []byte
	caps, err := a.Scanner.Capabilities(cctx)
	if err == nil {
		want = a.serviceFile(caps)
	}
	changed, err := avahi.Dir{Path: a.Dir, Reload: a.Reload}.Set(cctx, fileName, want)
	switch {
	case err != nil:
		a.Log.Warn("AirScan record", "err", err)
	case changed && want != nil:
		a.Log.Info("AirScan record published", "scanner", caps.MakeAndModel)
	case changed:
		a.Log.Info("scanner gone; AirScan record removed")
	}
}

func (a *Announcer) serviceFile(c escl.Capabilities) []byte {
	model := c.MakeAndModel
	if model == "" {
		model = "Scanner"
	}
	name := a.Name
	if name == "" || name == model {
		name = model + " on " + a.Hostname
	}
	var pdl, cs []string
	for _, f := range c.Formats {
		if strings.Contains(f, "/") {
			pdl = append(pdl, f)
		}
	}
	for _, m := range c.ColorModes {
		switch m {
		case "RGB24", "RGB48":
			cs = appendNew(cs, "color")
		case "Grayscale8", "Grayscale16":
			cs = appendNew(cs, "grayscale")
		case "BlackAndWhite1":
			cs = appendNew(cs, "binary")
		}
	}
	base := fmt.Sprintf("http://%s.local:%d/eSCL", a.Hostname, a.Port)
	txt := []string{
		"txtvers=1",
		"vers=2.0",
		"rs=eSCL",
		"ty=" + model,
		"note=" + a.Hostname,
		"pdl=" + strings.Join(pdl, ","),
		"cs=" + strings.Join(cs, ","),
		"is=platen",
		"duplex=F",
		"representation=" + base + "/ScannerIcon",
	}
	if c.UUID != "" {
		txt = append(txt, "uuid="+c.UUID)
	}
	if a.AdminURL != "" {
		txt = append(txt, "adminurl="+a.AdminURL)
	}
	return avahi.File(name, avahi.Service{Type: "_uscan._tcp", Port: a.Port, TXT: txt})
}

func appendNew(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
