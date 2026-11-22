package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/jasonthagerty/printserver/internal/cups"
	"github.com/jasonthagerty/printserver/internal/escl"
	"github.com/jasonthagerty/printserver/internal/usbprobe"
)

// Reason is one printer condition, in words a person can act on.
type Reason struct {
	Code     string `json:"code" doc:"IPP printer-state-reasons keyword, without its severity suffix"`
	Severity string `json:"severity" enum:"error,warning,report"`
	Text     string `json:"text"`
}

// Marker is one ink cartridge.
type Marker struct {
	Name  string `json:"name"`
	Color string `json:"color" doc:"#RRGGBB, or empty when unknown"`
	Level int    `json:"level" doc:"percent; -1 when the printer does not report it"`
}

// PrinterStatus is the print queue's state.
type PrinterStatus struct {
	Name       string   `json:"name"`
	Info       string   `json:"info"`
	Model      string   `json:"model"`
	State      string   `json:"state" enum:"idle,processing,stopped,unknown"`
	Reasons    []Reason `json:"reasons"`
	Message    string   `json:"message"`
	Accepting  bool     `json:"accepting"`
	Connected  bool     `json:"connected" doc:"false when the printer is off, unplugged or unreachable"`
	USB        string   `json:"usb" enum:"connected,stale,missing,unknown,none" doc:"USB check: none in test mode; stale means reconnected, restart the container"`
	QueuedJobs int      `json:"queued_jobs"`
	Markers    []Marker `json:"markers"`
}

// ScannerStatus is the scanner's state.
type ScannerStatus struct {
	Available bool   `json:"available"`
	State     string `json:"state" doc:"eSCL state: Idle, Processing, Testing, Stopped, Down; empty when unavailable"`
	Model     string `json:"model"`
}

// StatusOutput is everything the home screen shows.
type StatusOutput struct {
	Body struct {
		Printer      *PrinterStatus `json:"printer" doc:"null when the print service is not answering"`
		PrinterError string         `json:"printer_error,omitempty"`
		Scanner      ScannerStatus  `json:"scanner"`
		Mode         string         `json:"mode" enum:"test,usb" doc:"test: a simulated printer and scanner"`
		Version      string         `json:"version"`
	}
}

// HealthOutput is the liveness check.
type HealthOutput struct {
	Body struct {
		Status  string `json:"status" enum:"ok,degraded"`
		CUPS    bool   `json:"cups"`
		Scanner bool   `json:"scanner"`
		Version string `json:"version"`
	}
}

func registerStatus(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-status",
		Method:      http.MethodGet,
		Path:        "/status",
		Summary:     "Printer and scanner status",
		Tags:        []string{"Status"},
	}, func(ctx context.Context, _ *struct{}) (*StatusOutput, error) {
		out := &StatusOutput{}
		out.Body.Mode, out.Body.Version = deps.Device, deps.Version
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if p, err := deps.CUPS.Printer(ctx); err != nil {
			deps.Log.Warn("printer status", "err", err)
			out.Body.PrinterError = "The print service is not answering."
		} else {
			ps := printerStatus(p)
			applyUSB(&ps, deps.USB)
			out.Body.Printer = &ps
		}
		out.Body.Scanner = scannerStatus(ctx, deps.Scanner)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Server health",
		Tags:        []string{"Status"},
	}, func(ctx context.Context, _ *struct{}) (*HealthOutput, error) {
		out := &HealthOutput{}
		out.Body.Version = deps.Version
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_, err := deps.CUPS.Printer(ctx)
		out.Body.CUPS = err == nil
		out.Body.Scanner = scannerStatus(ctx, deps.Scanner).Available
		out.Body.Status = "ok"
		if !out.Body.CUPS {
			out.Body.Status = "degraded"
		}
		return out, nil
	})
}

// applyUSB overrides what CUPS says with what the USB bus says: CUPS only
// notices a missing printer when a job fails.
func applyUSB(ps *PrinterStatus, probe *usbprobe.Probe) {
	if probe == nil {
		ps.USB = "none"
		return
	}
	state, _ := probe.Check()
	ps.USB = string(state)
	switch state {
	case usbprobe.Missing:
		ps.Connected = false
		ps.Reasons = append(ps.Reasons, Reason{Code: "usb-missing", Severity: "error",
			Text: "The printer is off or unplugged. Turn it on and check the USB cable."})
	case usbprobe.Stale:
		ps.Connected = false
		ps.Reasons = append(ps.Reasons, Reason{Code: "usb-reconnected", Severity: "error",
			Text: "The printer was reconnected, and the print server can't see it until it restarts. On the router: /container/stop printserver, then /container/start printserver."})
	}
}

func scannerStatus(ctx context.Context, sc *escl.Client) ScannerStatus {
	state, err := sc.Status(ctx)
	if err != nil {
		return ScannerStatus{}
	}
	s := ScannerStatus{Available: true, State: state}
	if caps, err := sc.Capabilities(ctx); err == nil {
		s.Model = caps.MakeAndModel
	} else if errors.Is(err, escl.ErrUnavailable) {
		s.Available = false
	}
	return s
}

func printerStatus(p cups.Printer) PrinterStatus {
	ps := PrinterStatus{
		Name:       p.Name,
		Info:       p.Info,
		Model:      p.MakeAndModel,
		State:      p.State,
		Message:    p.StateMessage,
		Accepting:  p.Accepting,
		Connected:  true,
		QueuedJobs: p.QueuedJobs,
		Reasons:    []Reason{},
		Markers:    []Marker{},
	}
	for _, r := range p.StateReasons {
		reason := describeReason(r)
		if reason.Text == "" {
			continue
		}
		switch reason.Code {
		case "offline", "connecting-to-device", "timed-out", "shutdown":
			ps.Connected = false
		}
		ps.Reasons = append(ps.Reasons, reason)
	}
	for _, m := range p.Markers {
		ps.Markers = append(ps.Markers, Marker{Name: m.Name, Color: m.Color, Level: m.Level})
	}
	return ps
}

var reasonText = map[string]string{
	"media-empty":              "Out of paper. Load paper, then press the printer's OK or Resume button.",
	"media-needed":             "Out of paper. Load paper, then press the printer's OK or Resume button.",
	"media-jam":                "Paper jam. Clear it, then press OK on the printer.",
	"cover-open":               "The printer's cover is open.",
	"door-open":                "A door on the printer is open.",
	"input-tray-missing":       "The paper tray is missing.",
	"output-tray-missing":      "The output tray is closed or missing.",
	"output-area-full":         "The output tray is full.",
	"marker-supply-low":        "Ink is low.",
	"marker-supply-empty":      "Out of ink.",
	"marker-waste-almost-full": "The ink absorber is almost full.",
	"marker-waste-full":        "The ink absorber is full; the printer needs service.",
	"offline":                  "The printer is off or unplugged.",
	"connecting-to-device":     "Connecting to the printer. Is it on and plugged in?",
	"timed-out":                "The printer stopped responding.",
	"paused":                   "Printing is paused.",
	"shutdown":                 "The printer is shut down.",
	"toner-low":                "Ink is low.",
	"toner-empty":              "Out of ink.",
	"other":                    "The printer reported a problem.",
	"cups-missing-filter":      "The print driver is missing a filter. Check the server log.",
	"cups-insecure-filter":     "The print driver is misconfigured. Check the server log.",
}

// describeReason splits "media-empty-error" into code and severity and adds
// text. Reasons with no text (CUPS internals) are dropped by the caller.
func describeReason(r string) Reason {
	code, sev := r, "error"
	for _, s := range []string{"-error", "-warning", "-report"} {
		if strings.HasSuffix(r, s) {
			code, sev = strings.TrimSuffix(r, s), strings.TrimPrefix(s, "-")
			break
		}
	}
	text := reasonText[code]
	if code == "paused" || code == "connecting-to-device" {
		sev = "warning"
	}
	return Reason{Code: code, Severity: sev, Text: text}
}
