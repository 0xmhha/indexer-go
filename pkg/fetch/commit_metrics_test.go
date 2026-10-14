package fetch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

type observedTransactor struct {
	port.BlockTransactor
	beforeCommit func() error
}

func (s observedTransactor) BeginBlock(ctx context.Context) (context.Context, port.BlockTx, error) {
	txCtx, tx, err := s.BlockTransactor.BeginBlock(ctx)
	if err != nil {
		return txCtx, tx, err
	}
	return txCtx, observedCommit{tx, s.beforeCommit}, nil
}

type observedCommit struct {
	port.BlockTx
	before func() error
}

func (tx observedCommit) Commit() error {
	if err := tx.before(); err != nil {
		return err
	}
	return tx.BlockTx.Commit()
}

// Measure the actual Commit boundary, including a failed durability wait,
// without turning a failed transaction into an indexed block.
func TestBlockCommitMetricsFailureAndRetry(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			h := newChainHarness(t, &Config{NumWorkers: 1, PipelineMetrics: enabled}, nil)
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			failure := errors.New("test durability failure")
			var attempts atomic.Int32
			h.f.txr = observedTransactor{h.f.txr, func() error {
				if attempts.Add(1) == 1 {
					close(entered)
					<-release
					return failure
				}
				return nil
			}}
			done := make(chan error, 1)
			go func() { done <- h.f.FetchRange(t.Context(), 0, 0) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("writer did not reach Commit")
			}
			require.Zero(t, h.f.GetPipelineMetrics().Commit.Count, "pending Commit is not a completed observation")
			started := time.Now()
			time.Sleep(10 * time.Millisecond)
			waitMS := float64(time.Since(started)) / float64(time.Millisecond)
			releaseOnce.Do(func() { close(release) })
			require.ErrorIs(t, <-done, failure)
			_, err := h.db.GetLatestHeight(t.Context())
			require.ErrorIs(t, err, port.ErrNotFound, "failed Commit must not advance the cursor")
			seq, err := h.db.LastOutboxSeq(t.Context())
			require.NoError(t, err)
			require.Zero(t, seq, "failed Commit must not publish an outbox entry")
			stats := h.f.GetPipelineMetrics().Commit
			if enabled {
				require.EqualValues(t, 1, stats.Count)
				require.EqualValues(t, 1, stats.Errors)
				require.GreaterOrEqual(t, stats.TotalMS, waitMS)
			} else {
				require.Equal(t, StageMetrics{}, stats)
			}
			require.NoError(t, h.f.FetchRange(t.Context(), 0, 0))
			h.requireIndexed(t, 0, 0)
			stats = h.f.GetPipelineMetrics().Commit
			if enabled {
				require.EqualValues(t, 2, stats.Count)
				require.EqualValues(t, 1, stats.Errors)
			} else {
				require.Equal(t, StageMetrics{}, stats)
			}
		})
	}
}
