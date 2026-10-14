package fetch

import (
	"context"
	"sync"
	"time"
)

type heightObservation struct {
	height  uint64
	ok      bool
	err     error
	at      time.Time
	latency time.Duration
}

type heightFlight struct {
	done   chan struct{}
	owner  context.Context
	result heightObservation
	gen    uint64
}

// heightObserver shares an in-flight read and one recent successful result.
// The requesting goroutine owns the RPC; there is no detached background job.
type heightObserver struct {
	mu     sync.Mutex
	last   heightObservation
	valid  bool
	flight *heightFlight
	gen    uint64
	now    func() time.Time // nil uses time.Now; tests move freshness without sleeps
}

func (h *heightObserver) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

func (h *heightObserver) read(ctx context.Context, age time.Duration, query func(context.Context) (uint64, bool, error)) heightObservation {
	for {
		if err := ctx.Err(); err != nil {
			return heightObservation{err: err}
		}
		h.mu.Lock()
		if pending := h.flight; age > 0 && pending != nil {
			h.mu.Unlock()
			select {
			case <-ctx.Done():
				return heightObservation{err: ctx.Err()}
			case <-pending.done:
				if err := ctx.Err(); err != nil {
					return heightObservation{err: err}
				}
				// A short-lived observer must not cancel another caller's read.
				if pending.owner.Err() != nil && ctx.Err() == nil {
					continue
				}
				h.mu.Lock()
				newer := pending.gen != h.gen
				h.mu.Unlock()
				if newer {
					continue
				}
				return pending.result
			}
		}
		if age > 0 && h.valid && h.clock().Sub(h.last.at) < age {
			last := h.last
			h.mu.Unlock()
			return last
		}
		h.gen++
		pending := &heightFlight{done: make(chan struct{}), owner: ctx, gen: h.gen}
		h.flight = pending
		h.mu.Unlock()

		start := time.Now()
		height, ok, err := query(ctx)
		if err == nil && ctx.Err() != nil {
			ok, err = false, ctx.Err()
		}
		result := heightObservation{height: height, ok: ok, err: err, at: h.clock(), latency: time.Since(start)}
		h.mu.Lock()
		newer := pending.gen != h.gen
		if !newer {
			h.last, h.valid = result, err == nil
		}
		pending.result = result
		if h.flight == pending {
			h.flight = nil
		}
		close(pending.done)
		h.mu.Unlock()
		if age > 0 && newer {
			continue
		}
		return result
	}
}

func (h *heightObserver) nextRead(age time.Duration) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.valid {
		return max(0, age-h.clock().Sub(h.last.at))
	}
	return age
}

func (f *Fetcher) readNodeHeight(ctx context.Context, age time.Duration) heightObservation {
	return f.nodeHead.read(ctx, age, func(ctx context.Context) (uint64, bool, error) {
		ctx, cancel := f.rpcCtx(ctx)
		defer cancel()
		n, err := f.client.GetLatestBlockNumber(ctx)
		return n, err == nil, err
	})
}

// ObserveNodeHeight shares a raw node-head observation at most maxAge old,
// capped at progressPoll. Zero starts a fresh RPC, even during an older read.
// It preserves RPC latency and invalidates successes after a failed read.
// This is independent of the indexing finality target.
func (f *Fetcher) ObserveNodeHeight(ctx context.Context, maxAge time.Duration) (uint64, time.Duration, error) {
	observation := f.readNodeHeight(ctx, min(maxAge, progressPoll))
	return observation.height, observation.latency, observation.err
}
