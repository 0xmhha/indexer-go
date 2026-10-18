package rpcpool

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func schedulingJob(t *testing.T, maintenance bool, members int, credential string) *readRequest {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://node", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", credential)
	parts := make([]rpcObject, members)
	for i := range parts {
		parts[i] = rpcObject{"jsonrpc": json.RawMessage(`"2.0"`), "id": json.RawMessage(`1`), "method": json.RawMessage(`"eth_getBlockByNumber"`), "params": json.RawMessage(`["0x1",false]`)}
	}
	return &readRequest{req: req, parts: parts, maintenance: maintenance}
}

func TestMaintenanceGraceWakesAndPreservesFIFO(t *testing.T) {
	for _, name := range []string{"foreground", "credentials", "capacity", "cancelled", "expiry", "closed"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			c := &readCoalescer{ctx: ctx, jobs: make(chan *readRequest, readQueueCapacity)}
			first := schedulingJob(t, true, 1, "a")
			g := readGroup{jobs: []*readRequest{first}, members: 1}
			expiry := make(chan time.Time, 1)
			foreground := schedulingJob(t, false, 1, "a")
			switch name {
			case "foreground":
				c.jobs <- foreground // expiry never fires: this must end the grace
			case "credentials":
				c.jobs <- schedulingJob(t, true, 1, "b")
				c.jobs <- foreground
			case "capacity":
				g.members = readBatchMembers - 1
				g.jobs[0] = schedulingJob(t, true, g.members, "a")
				c.jobs <- schedulingJob(t, true, 2, "a")
				c.jobs <- foreground
			case "cancelled":
				dead, stop := context.WithCancel(ctx)
				stop()
				job := schedulingJob(t, false, 1, "b")
				job.req = job.req.WithContext(dead)
				c.jobs <- job
				c.jobs <- foreground
			case "closed":
				cancel()
			case "expiry":
				// Let a further maintenance read enter before expiring the
				// ORIGINAL window. No foreground is needed to finish it.
				c.jobs <- schedulingJob(t, true, 1, "a")
			}
			done := make(chan error, 1)
			go func() { done <- c.waitMaintenance(&g, expiry) }()
			if name == "expiry" {
				require.Eventually(t, func() bool { return len(c.jobs) == 0 }, time.Second, time.Millisecond)
				expiry <- time.Time{}
			}
			err := <-done // channel handoff before inspecting the mutable group
			if name == "closed" {
				require.ErrorIs(t, err, context.Canceled)
				return
			}
			require.NoError(t, err)
			if name == "credentials" || name == "capacity" {
				require.Len(t, g.jobs, 1)
				require.NotNil(t, g.carry)
				require.Equal(t, foreground, <-c.jobs, "do not reorder past incompatible request")
			} else {
				require.Len(t, g.jobs, 2)
				require.Nil(t, g.carry)
				require.Equal(t, 2, g.members, "cancelled request consumes no member")
			}
		})
	}
}

func TestMaintenanceGraceOnlyOnSlowLimitedBatchPools(t *testing.T) {
	for _, test := range []struct {
		name        string
		limit       float64
		maintenance bool
		unsupported bool
		burst       int
		want        time.Duration
	}{
		{"five", 5, true, false, 1, maintenanceWindow},
		{"ten", 10, true, false, 1, maintenanceWindow},
		{"burst", 5, true, false, 4, readWindow},
		{"fast", 100, true, false, 1, readWindow},
		{"unlimited", 0, true, false, 1, readWindow},
		{"foreground", 5, false, false, 1, readWindow},
		{"unsupported", 5, true, true, 1, readWindow},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := &Pool{}
			if test.limit > 0 {
				p.limiter = rate.NewLimiter(rate.Limit(test.limit), test.burst)
			}
			c := &readCoalescer{pool: p}
			c.unsupported.Store(test.unsupported)
			require.Equal(t, test.want, c.maintenanceDelay(&readRequest{maintenance: test.maintenance}))
		})
	}
}

func TestMaintenanceBatchKeepsEveryFreshReadAndPhysicalBudget(t *testing.T) {
	var mu sync.Mutex
	var attempts []time.Time
	p, start := stagedReadPool(t, readTransport(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		attempts = append(attempts, time.Now())
		n := len(attempts)
		mu.Unlock()
		var parts []rpcObject
		require.NoError(t, json.NewDecoder(req.Body).Decode(&parts))
		require.Len(t, parts, 2)
		for _, part := range parts {
			require.JSONEq(t, `["0x1",false]`, string(part["params"]), "fresh height/params are unchanged")
			delete(part, "method")
			delete(part, "params")
			if n == 1 {
				part["result"] = json.RawMessage(`"first"`)
			} else {
				part["result"] = json.RawMessage(`"changed"`)
			}
		}
		data, _ := json.Marshal(parts)
		return readResponse(data), nil
	}))
	p.limiter = rate.NewLimiter(5, 1)
	body := `{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x1",false]}`
	background := submitRead(p, port.WithRPCMaintenance(t.Context()), body)
	require.Eventually(t, func() bool { return len(p.reads.jobs) == 1 }, time.Second, time.Millisecond)
	fresh := submitRead(p, t.Context(), body)
	start(2)
	require.Contains(t, string(replyJSON(t, background)), `"first"`)
	require.Contains(t, string(replyJSON(t, fresh)), `"first"`)
	// Hold the original dispatcher's next token so both callers can queue.
	// This still charges the actual HTTP to the SAME limiter.
	held := p.limiter.Reserve()
	earliest := time.Now().Add(held.Delay() + 180*time.Millisecond)
	background = submitRead(p, port.WithRPCMaintenance(t.Context()), body)
	fresh = submitRead(p, t.Context(), body)
	require.Contains(t, string(replyJSON(t, background)), `"changed"`)
	require.Contains(t, string(replyJSON(t, fresh)), `"changed"`, "a repeated fresh read must get the new response")
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, attempts, 2)
	require.False(t, attempts[1].Before(earliest), "both groups use the same physical token budget")
}

func TestMaintenanceGraceKeepsCallerDeadline(t *testing.T) {
	var calls atomic.Int32
	p, err := New(Config{Endpoints: []string{"http://node"}, RateLimit: 5, Burst: 1, CoalesceReads: true, Transport: readTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return readResponse([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`)), nil
	})})
	require.NoError(t, err)
	defer p.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	out := submitRead(p, port.WithRPCMaintenance(ctx), `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	select {
	case result := <-out:
		require.ErrorIs(t, result.err, context.DeadlineExceeded)
	case <-time.After(2 * time.Second):
		t.Fatal("maintenance grace extended caller deadline")
	}
	p.Close()
	require.Zero(t, calls.Load(), "expired request must not be dispatched later")
}
