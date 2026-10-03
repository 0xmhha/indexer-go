package fetch

import (
	"context"
	"time"
)

// sleepCtx waits for d or until ctx is done, whichever comes first, and
// returns ctx.Err() in the latter case so retry loops stop on shutdown.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
