package supplies

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jasonthagerty/printserver/internal/cups"
)

// PollInk asks the printer at uri for its ink levels now and every
// interval until ctx ends. A failed poll (the printer asleep, or off its
// Wi-Fi) keeps the last good reading; only success replaces it.
func (s *Store) PollInk(ctx context.Context, uri string, interval time.Duration, log *slog.Logger) {
	hc := &http.Client{Timeout: 20 * time.Second}
	poll := func() {
		markers, err := cups.PrinterMarkers(ctx, hc, uri)
		if err != nil {
			log.Info("ink poll failed; keeping the last reading", "err", err)
			return
		}
		levels := make([]Level, 0, len(markers))
		for _, m := range markers {
			if id := cartridgeFor(m.Name); id != "" {
				levels = append(levels, Level{Cartridge: id, Color: m.Color, Level: m.Level, Low: m.Low})
			}
		}
		replaced, err := s.RecordInk(time.Now(), levels)
		if err != nil {
			log.Warn("recording ink levels", "err", err)
			return
		}
		for _, id := range replaced {
			log.Info("cartridge level jumped; marked replaced today", "cartridge", id)
		}
	}
	poll()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			poll()
		}
	}
}

// cartridgeFor maps a printer's marker name ("Black", "Color", "Tri-color")
// to a cartridge ID.
func cartridgeFor(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "black") || n == "bk" || n == "pgbk":
		return "black"
	case strings.Contains(n, "color") || strings.Contains(n, "colour") || n == "cl":
		return "color"
	}
	return ""
}
