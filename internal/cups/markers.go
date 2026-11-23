package cups

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/OpenPrinting/goipp"
)

// InkMarker is one supply as a printer reports it, with its "low" threshold.
type InkMarker struct {
	Name  string
	Color string // #RRGGBB, or several for a tri-color cartridge
	Level int    // percent; -1 unknown
	Low   int    // percent at which the printer calls it low; -1 unknown
}

// PrinterMarkers asks a network printer (not CUPS) for its ink levels, at
// an ipp:// or http:// URI such as ipp://192.0.2.10/ipp/print. It is read
// only: one Get-Printer-Attributes request.
func PrinterMarkers(ctx context.Context, hc *http.Client, uri string) ([]InkMarker, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("ink: bad printer URI %q", uri)
	}
	ippURI, httpURL := *u, *u
	switch u.Scheme {
	case "ipp", "http":
		ippURI.Scheme, httpURL.Scheme = "ipp", "http"
	case "ipps", "https":
		ippURI.Scheme, httpURL.Scheme = "ipps", "https"
	default:
		return nil, fmt.Errorf("ink: printer URI must be ipp:// or http://, got %q", uri)
	}
	if httpURL.Port() == "" {
		httpURL.Host += ":631"
	}
	if hc == nil {
		hc = &http.Client{}
	}
	c := &Client{http: hc, baseURL: strings.TrimSuffix(httpURL.Scheme+"://"+httpURL.Host, "/")}
	req := c.newRequest(goipp.OpGetPrinterAttributes)
	req.Operation.Add(goipp.MakeAttribute("printer-uri", goipp.TagURI, goipp.String(ippURI.String())))
	req.Operation.Add(keywords("requested-attributes", "marker-names", "marker-colors", "marker-levels", "marker-low-levels"))
	resp, err := c.do(ctx, httpURL.EscapedPath(), req, nil)
	if err != nil {
		return nil, err
	}
	a := resp.Printer
	names, colors, levels, lows := strs(a, "marker-names"), strs(a, "marker-colors"), ints(a, "marker-levels"), ints(a, "marker-low-levels")
	out := make([]InkMarker, 0, len(names))
	for i, n := range names {
		m := InkMarker{Name: n, Level: -1, Low: -1}
		if i < len(colors) {
			m.Color = colors[i]
		}
		if i < len(levels) && levels[i] >= 0 && levels[i] <= 100 {
			m.Level = levels[i]
		}
		if i < len(lows) && lows[i] >= 0 && lows[i] <= 100 {
			m.Low = lows[i]
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ink: the printer reported no supplies")
	}
	return out, nil
}
