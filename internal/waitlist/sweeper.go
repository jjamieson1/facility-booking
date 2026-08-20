package waitlist

import (
	"context"
	"log"
	"time"
)

// Sweeper periodically expires waitlist entries whose slot has passed. It runs as
// a background ticker (scanning once immediately on start), so entries for
// finished slots don't linger on the resident's list or the C2 callout.
type Sweeper struct {
	svc   *Service
	every time.Duration
}

// NewSweeper builds the sweeper. every is the scan interval.
func NewSweeper(svc *Service, every time.Duration) *Sweeper {
	return &Sweeper{svc: svc, every: every}
}

// Run sweeps immediately, then on every tick until ctx is cancelled. Intended to
// be started in its own goroutine.
func (w *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(w.every)
	defer ticker.Stop()
	for {
		if n, err := w.svc.ExpireStale(ctx); err != nil {
			log.Printf("waitlist: expire failed: %v", err)
		} else if n > 0 {
			log.Printf("waitlist: expired %d past-slot entries", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
