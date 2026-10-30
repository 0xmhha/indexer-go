package arcingress

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeOwnedLoopPausesJoinedRoundAndResumesWithNewOwnerUntilStop(t *testing.T) {
	p := rawProgram(t)
	p.cfg.Timeout = time.Second
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	entered := make(chan *roundRun, 2)
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- p.runOwnedLoop(ctx, func(ctx context.Context, r *roundRun) error {
			n := calls.Add(1)
			entered <- r
			if n == 1 {
				select {
				case <-r.stop:
					return port.ErrExecutionRewinding
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	defer p.Stop()
	var first *roundRun
	select {
	case first = <-entered:
	case err := <-done:
		t.Fatalf("owner returned before first round: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, p.quiesce(ctx))
	select {
	case <-first.done:
	default:
		t.Fatal("pause returned before round joined")
	}
	select {
	case <-p.done:
		t.Fatal("pause terminated the long-lived App owner")
	default:
	}
	p.mu.Lock()
	p.recovering = false
	p.resume <- struct{}{}
	p.mu.Unlock()
	select {
	case second := <-entered:
		require.NotSame(t, first, second, "whole peer/worker ownership must be recreated")
	case err := <-done:
		t.Fatalf("owner lost coordinator resume: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	p.Stop()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Equal(t, int32(2), calls.Load())
}
