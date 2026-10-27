package nodebackend

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire/nativecfg"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/x/stablenet/executor/nodestore"
)

type CapacityConfig = nativecfg.CapacityConfig

func checkNodeCapacity(ctx context.Context, c Config, resume bool) error {
	if c.Capacity == nil {
		return nil
	}
	if e := c.Capacity.Validate(); e != nil {
		return e
	}
	free := c.Capacity.StopFreeBytes
	if resume {
		free = c.Capacity.ResumeFreeBytes
	}
	budget := c.Capacity.NodeBytes
	if resume {
		budget -= budget / 10
	}
	return nodestore.CheckCapacity(ctx, c.DataDir, budget, free)
}

// Only sampling is cached. Errors stop the owner immediately; an explicit new
// run/resume-check takes a new sample. No retry starts a node or evicts input.
type capacityChecker struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
	check    func(context.Context) error
}

func (c *capacityChecker) run(ctx context.Context, force bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return e
	}
	if !force && time.Now().Before(c.next) {
		return nil
	}
	if e := c.check(ctx); e != nil {
		return e
	}
	c.next = time.Now().Add(c.interval)
	return nil
}
