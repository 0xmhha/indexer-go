package nodebackend

import (
	"context"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/nodestore"
	"os"
	"sync"
	"testing"
	"time"
)

func TestCapacityStopsBeforeNodeStartWhenSpaceUnobservable(t *testing.T) {
	c, g := runtimeConfig(t)
	c.Peers = nil
	c.Capacity = &CapacityConfig{NodeBytes: 1 << 30, StopFreeBytes: 1 << 20, ResumeFreeBytes: 2 << 20, IntervalSeconds: 30}
	if err := checkNodeCapacity(t.Context(), c, false); err == nil {
		t.Fatal("missing node storage must halt, not authorize import")
	}
	c.Capacity.NodeBytes = 0
	if _, _, err := settings(c, g); err == nil {
		t.Fatal("invalid capacity config accepted")
	}

}

func TestCapacityBudgetResumeAndConcurrentChecks(t *testing.T) {
	c, _ := runtimeConfig(t)
	if e := os.Mkdir(c.DataDir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(c.DataDir+"/owned", make([]byte, 16), 0600); e != nil {
		t.Fatal(e)
	}
	c.Capacity = &CapacityConfig{NodeBytes: 1, StopFreeBytes: 1, ResumeFreeBytes: 2}
	if e := checkNodeCapacity(t.Context(), c, false); e != nodestore.ErrCapacity {
		t.Fatal("over budget continued", e)
	}
	c.Capacity.NodeBytes = 1 << 20
	if e := checkNodeCapacity(t.Context(), c, true); e != nil {
		t.Fatal("capacity did not resume", e)
	}
	calls := 0
	checker := &capacityChecker{interval: time.Hour, check: func(context.Context) error { calls++; return nil }}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := checker.run(t.Context(), false); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatal("concurrent capacity probes escaped sample gate", calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if checker.run(ctx, true) != context.Canceled {
		t.Fatal("cancellation ignored")
	}
}
