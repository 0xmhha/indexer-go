package evmstate

import (
	"sync/atomic"
	"time"
)

// StageMetric is cumulative wall time, including limiter waits in RPCBatch
// and the fresh canonical request/fsync in Commit. Counts are logical stage
// calls, not physical HTTP attempts; rpcpool.Status owns attempt accounting.
type StageMetric struct {
	Count    uint64        `json:"count"`
	Failures uint64        `json:"failures"`
	Total    time.Duration `json:"total"`
}
type Metrics struct {
	RPCBatch StageMetric `json:"rpcBatch"`
	Verify   StageMetric `json:"verify"`
	Commit   StageMetric `json:"commit"`
}
type stageCounter struct{ count, failures, nanos atomic.Uint64 }

func (c *stageCounter) record(start time.Time, err error) {
	c.count.Add(1)
	c.nanos.Add(uint64(time.Since(start)))
	if err != nil {
		c.failures.Add(1)
	}
}
func (c *stageCounter) snapshot() StageMetric {
	return StageMetric{Count: c.count.Load(), Failures: c.failures.Load(), Total: time.Duration(c.nanos.Load())}
}

// Metrics is safe during concurrent reads/acquisitions. Each counter is
// atomic; this diagnostic snapshot is not a transactional coverage claim.
func (s *Supplier) Metrics() Metrics {
	return Metrics{RPCBatch: s.rpcBatch.snapshot(), Verify: s.verify.snapshot(), Commit: s.commit.snapshot()}
}
