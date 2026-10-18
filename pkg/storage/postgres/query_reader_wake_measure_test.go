package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// Isolates admission wakeup from SQL/network execution. The pool hook models
// an occupied/slow spare; it is not part of the mixed HTTP throughput fixture.
func TestMeasureReservedReaderWake(t *testing.T) {
	if os.Getenv("INDEXER_MEASURE_READER_WAKE") != "1" {
		t.Skip("set INDEXER_MEASURE_READER_WAKE=1 for the admission-only comparison")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := newTestStore(t)
	cfg := s.pool.Config()
	s.pool.Close()
	var armed atomic.Bool
	entered := make(chan struct{}, 1)
	cfg.BeforeAcquire = func(ctx context.Context, _ *pgx.Conn) bool {
		if !armed.Load() {
			return true
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return false
	}
	var err error
	s.pool, err = pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	lease, err := s.ReserveQueryReader(ctx)
	require.NoError(t, err)
	defer lease.Close()
	reader := s.queryReader
	var ms []float64
	const rounds = 256
	for i := 0; i < rounds; i++ {
		select {
		case <-entered:
		default:
		}
		armed.Store(false)
		spare, err := s.pool.Acquire(ctx)
		require.NoError(t, err)
		spare.Release()
		_, releasePin, err := reader.acquire(lease.Context(ctx))
		require.NoError(t, err)
		releasePin = sync.OnceFunc(releasePin)
		defer releasePin()
		armed.Store(true)
		done := make(chan error, 1)
		go func() {
			_, release, err := reader.acquire(lease.Context(ctx))
			if err == nil {
				release()
			}
			done <- err
		}()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		start := time.Now()
		releasePin()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		ms = append(ms, float64(time.Since(start))/float64(time.Millisecond))
	}
	armed.Store(false)
	require.Eventually(t, func() bool { return s.pool.Stat().AcquiredConns() == 1 }, time.Second, time.Millisecond)
	sort.Float64s(ms)
	percentile := func(p float64) float64 { return ms[int(math.Ceil(p*float64(len(ms))))-1] }
	data, err := json.Marshal(map[string]any{"rounds": rounds, "maxConns": cfg.MaxConns, "p50Ms": percentile(.5), "p95Ms": percentile(.95), "p99Ms": percentile(.99), "maxMs": ms[len(ms)-1]})
	require.NoError(t, err)
	fmt.Printf("READER_WAKE_MEASURE %s\n", data)
}
