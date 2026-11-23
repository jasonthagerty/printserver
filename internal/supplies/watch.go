package supplies

import (
	"context"
	"log/slog"
	"time"

	"github.com/jasonthagerty/printserver/internal/cups"
)

// Watch records the newest completed print job every interval until ctx
// ends. CUPS keeps a day of job history, so a minute's polling misses
// nothing.
func (s *Store) Watch(ctx context.Context, c *cups.Client, interval time.Duration, log *slog.Logger) {
	check := func() {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		jobs, err := c.Jobs(cctx, true, 0)
		if err != nil {
			return // CUPS is restarting; the next round catches up
		}
		for _, j := range jobs { // newest first
			if j.State != "completed" || j.CompletedAt.IsZero() {
				continue
			}
			changed, err := s.RecordPrint(j.ID, j.CompletedAt)
			if err != nil {
				log.Warn("recording last print", "err", err)
			} else if changed {
				log.Info("last print recorded", "job", j.ID, "at", j.CompletedAt)
			}
			return
		}
	}
	check()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check()
		}
	}
}
