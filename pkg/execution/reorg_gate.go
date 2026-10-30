package execution

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"math"
	"sync"
)

// ReorgGate borrows the receiver's current Controller. It owns no worker,
// generation, database connection or target-selection authority.
type ReorgGate struct {
	store   ControllerStore
	mu      sync.Mutex
	current *Controller
	changed chan struct{}
	serial  uint64
}

func NewReorgGate(store ControllerStore) *ReorgGate {
	return &ReorgGate{store: store, changed: make(chan struct{})}
}
func (g *ReorgGate) Bind(c *Controller) func() {
	g.mu.Lock()
	g.serial++
	serial := g.serial
	g.current = c
	close(g.changed)
	g.changed = make(chan struct{})
	g.mu.Unlock()
	return sync.OnceFunc(func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.serial == serial {
			g.current = nil
			close(g.changed)
			g.changed = make(chan struct{})
		}
	})
}
func (g *ReorgGate) Fence(ctx context.Context, ref execwire.Ref) error {
	if ref.Generation == 0 || ref.Generation > math.MaxInt64 || ref.Height > math.MaxInt64 || ref.Hash == (execwire.Hash{}) || g.store == nil {
		return port.ErrInvalidData
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		status, err := g.store.ExecutionStatus(ctx)
		if err != nil {
			return err
		}
		if status.Maintenance {
			return port.ErrExecutionMaintenance
		}
		if status.Rewinding || status.ReorgPending {
			return nil
		}
		if status.Generation != ref.Generation {
			return port.ErrExecutionStale
		}
		g.mu.Lock()
		current, changed := g.current, g.changed
		g.mu.Unlock()
		if current == nil {
			select {
			case <-changed:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		err = current.CheckReorgAt(ctx, ref.Generation)
		if err == nil {
			return nil
		}
		if !errors.Is(err, port.ErrClosed) && !errors.Is(err, context.Canceled) && !errors.Is(err, port.ErrExecutionRewinding) {
			return err
		}
		g.mu.Lock()
		replaced := g.current != current
		changed = g.changed
		g.mu.Unlock()
		if replaced {
			continue
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
