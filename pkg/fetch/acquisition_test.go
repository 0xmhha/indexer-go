package fetch

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func holdAcquiredCommit(t *testing.T, h *chainHarness, ctx context.Context) (chan struct{}, func()) {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	h.f.SetBeforeCommitHook(func(n uint64) error {
		if n != 0 {
			return nil
		}
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	return entered, unblock
}

func TestAcquisitionQueuePreservesSparseLivePath(t *testing.T) {
	h := newChainHarness(t, &Config{StatePrefetch: true, AcquisitionQueue: true, RetryDelay: time.Hour}, nil)
	h.f.SetSparse(true)
	// This source has no log-range capability: sparse ingest must fail on
	// that path, rather than silently replacing it with full block fetches.
	spy := &gatedSource{Source: h.src}
	h.f.SetSource(spy)
	core, logs := observer.New(zap.ErrorLevel)
	h.f.logger = zap.New(core)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	done := make(chan error, 1)
	go func() { done <- h.f.Run(ctx) }()
	defer func() { cancel(); <-done }()
	require.Eventually(t, func() bool { return logs.Len() > 0 }, 4*time.Second, time.Millisecond)
	require.Zero(t, spy.started.Load(), "sparse range ingest must not fetch full blocks")
}

func TestAcquisitionContinuesAcrossLiveHeadsWhileWriterBlocked(t *testing.T) {
	h := newChainHarness(t, &Config{NumWorkers: 2, StatePrefetch: true, AcquisitionQueue: true, QueueBlocks: 16, BatchSize: 1, PollInterval: time.Hour}, nil)
	heads := make(chan struct{}, 1)
	h.f.SetHeadNotifier(heads)
	h.chain.SetHead(0)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	entered, unblock := holdAcquiredCommit(t, h, ctx)
	done := make(chan error, 1)
	defer func() { cancel(); unblock(); <-done }()
	go func() { done <- h.f.Run(ctx) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("commit did not start")
	}
	h.chain.SetHead(8)
	heads <- struct{}{}
	require.Eventually(t, func() bool {
		stats := h.f.GetPipelineMetrics()
		return stats.AcquiredHeight != nil && *stats.AcquiredHeight == 8 && stats.QueueBlocks == 9
	}, 4*time.Second, time.Millisecond, "acquisition must cross chunk_size=1 and new heads without a commit")
	stats := h.f.GetPipelineMetrics()
	require.Nil(t, stats.CommittedHeight)
	require.LessOrEqual(t, stats.QueueBytes, stats.QueueMaxBytes)
	unblock()
	require.Eventually(t, func() bool {
		n, err := h.db.GetLatestHeight(ctx)
		return err == nil && n == 8
	}, 4*time.Second, time.Millisecond)
	h.requireIndexed(t, 0, 8)
}

func TestAcquisitionQueueCapacityAndCancellation(t *testing.T) {
	h := newChainHarness(t, &Config{NumWorkers: 2, StatePrefetch: true, AcquisitionQueue: true, QueueBlocks: 3}, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	entered, unblock := holdAcquiredCommit(t, h, ctx)
	done := make(chan error, 1)
	go func() { done <- h.f.FetchRange(ctx, 0, 8) }()
	defer unblock()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("commit did not start")
	}
	require.Eventually(t, func() bool { return h.f.GetPipelineMetrics().InFlight == 7 }, 4*time.Second, time.Millisecond)
	stats := h.f.GetPipelineMetrics()
	require.Equal(t, int64(3), stats.QueueBlocks)
	require.Equal(t, int64(3), stats.PeakQueueBlocks)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	stats = h.f.GetPipelineMetrics()
	require.Zero(t, stats.InFlight)
	require.Zero(t, stats.QueueBlocks)
	require.Zero(t, stats.QueueBytes)
	require.Zero(t, stats.StateBytes)
	_, err := h.db.GetLatestHeight(t.Context())
	require.ErrorIs(t, err, port.ErrNotFound)
}

func TestAcquisitionQueueByteBudgetAndOrderedFailure(t *testing.T) {
	for _, mode := range []string{"bytes", "fetch", "prepare", "fork"} {
		t.Run(mode, func(t *testing.T) {
			cfg := &Config{NumWorkers: 2, StatePrefetch: true, AcquisitionQueue: true, QueueBlocks: 16, MaxRetries: 1}
			if mode == "bytes" {
				cfg.QueueMaxBytes = 1
			}
			h := newChainHarness(t, cfg, nil)
			if mode == "fetch" {
				h.f.SetSource(&failingAt{Source: h.src, at: 2})
			}
			setPreparingFeature(t, h, func(_ context.Context, b *feature.Block) (feature.PreparedHandler, error) {
				if mode == "prepare" && b.Model.Number == 2 {
					return feature.PreparedHandler{}, errFlaky
				}
				return feature.PreparedHandler{Bytes: 128, ReadState: mode == "fork"}, nil
			})
			if mode == "fork" {
				h.f.SetStateSource(&changedHashSource{Source: h.src, changeAt: 1})
			}
			err := h.f.FetchRange(t.Context(), 0, 8)
			require.Error(t, err)
			if mode == "fetch" || mode == "prepare" {
				require.ErrorContains(t, err, "failed to fetch block 2")
				h.requireIndexed(t, 0, 1)
			} else {
				_, storedErr := h.db.GetLatestHeight(t.Context())
				require.ErrorIs(t, storedErr, port.ErrNotFound)
			}
			if mode == "bytes" {
				require.ErrorContains(t, err, "exceeds queue_max_bytes")
			}
			if mode == "fork" {
				require.ErrorIs(t, err, ErrReorg)
			}
			stats := h.f.GetPipelineMetrics()
			require.Zero(t, stats.InFlight)
			require.Zero(t, stats.QueueBytes)
			require.Zero(t, stats.StateBytes)
			// A fresh run acquires inputs again, with no stale fork/error queue.
			cfg.QueueMaxBytes = 0
			h.f.SetSource(h.src)
			h.f.SetStateSource(h.src)
			h.f.SetFeatures(nil)
			require.NoError(t, h.f.FetchRange(t.Context(), h.f.GetNextHeight(t.Context()), 8))
			h.requireIndexed(t, 0, 8)
		})
	}
}

func TestAcquisitionQueueByteBackpressureDoesNotDeadlock(t *testing.T) {
	h := newChainHarness(t, &Config{NumWorkers: 2, StatePrefetch: true, AcquisitionQueue: true, QueueBlocks: 16}, nil)
	var largest int64
	for n := uint64(0); n <= 2; n++ {
		fb, err := h.f.fetchOnce(t.Context(), n)
		require.NoError(t, err)
		size, err := acquiredPayloadBytes(fb)
		require.NoError(t, err)
		largest = max(largest, size)
	}
	h.f.config.QueueMaxBytes = largest + 16384
	setPreparingFeature(t, h, func(context.Context, *feature.Block) (feature.PreparedHandler, error) {
		return feature.PreparedHandler{Bytes: 16384}, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	entered, unblock := holdAcquiredCommit(t, h, ctx)
	done := make(chan error, 1)
	go func() { done <- h.f.FetchRange(ctx, 0, 2) }()
	defer unblock()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("commit did not start")
	}
	require.Eventually(t, func() bool { return h.f.GetPipelineMetrics().Acquire.Count == 3 }, 4*time.Second, time.Millisecond)
	stats := h.f.GetPipelineMetrics()
	require.Equal(t, int64(1), stats.QueueBlocks, "byte budget, not block capacity, must stop admission")
	require.LessOrEqual(t, stats.PeakQueueBytes, h.f.config.QueueMaxBytes)
	unblock()
	require.NoError(t, <-done)
	h.requireIndexed(t, 0, 2)
}
