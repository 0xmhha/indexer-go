package rpcpool

import "time"

// A periodic control read may wait for an independent commit read rather than
// consuming the last free HTTP slot just before it. The grace never exceeds
// 500ms; foreground arrival ends it immediately. Only low-budget, burst-one
// pools use the grace. Unlimited/high-rate/bursting pools and batch-unsupported
// endpoints retain the original 10ms window. No result is reused, and caller
// deadlines still apply to every request while it waits.
func (c *readCoalescer) maintenanceDelay(first *readRequest) time.Duration {
	if !first.maintenance || c.pool.limiter == nil || c.pool.limiter.Limit() > 10 || c.pool.limiter.Burst() != 1 || c.unsupported.Load() {
		return readWindow
	}
	return maintenanceWindow
}

type readGroup struct {
	jobs    []*readRequest
	members int
	carry   *readRequest
}

// Keep FIFO across credential/size boundaries, including jobs collected during
// the grace period. A canceled request occupies no member and is never revived.
func (g *readGroup) add(job *readRequest) bool {
	if job.req.Context().Err() != nil {
		return true
	}
	if g.members+len(job.parts) > readBatchMembers || !sameReadHeaders(g.jobs[0].req, job.req) {
		g.carry = job
		return false
	}
	g.jobs = append(g.jobs, job)
	g.members += len(job.parts)
	return true
}

// Only the existing dispatcher waits here. Foreground arrival ends the grace
// immediately; the original shared limiter still reserves the physical token.
// Further maintenance arrivals never reset the fixed expiry (no starvation).
func (c *readCoalescer) waitMaintenance(g *readGroup, expiry <-chan time.Time) error {
	for !c.unsupported.Load() && g.members < readBatchMembers && g.carry == nil {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case <-expiry:
			return nil
		case job := <-c.jobs:
			if job.req.Context().Err() != nil {
				continue
			}
			if !g.add(job) || !job.maintenance {
				return nil
			}
		}
	}
	return nil
}
