package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

func TestCapturePostgresDurabilityAndIsolation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	chain := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	var fsync string
	require.NoError(t, s.pool.QueryRow(ctx, "SHOW fsync").Scan(&fsync))
	key := []byte("'\"; DROP TABLE capture_records; --")
	value := []byte("source\x00bytes")
	err := s.CaptureUpdate(ctx, chain, func(w port.CaptureWriter) error {
		var syncCommit string
		require.NoError(t, w.(*captureKV).q.QueryRow(ctx, "SHOW synchronous_commit").Scan(&syncCommit))
		require.Equal(t, "on", syncCommit)
		return w.Put(key, value)
	})
	if fsync != "on" {
		require.ErrorContains(t, err, "fsync must be on")
		return
	}
	require.NoError(t, err)
	r := openTestStore(t, s.schema, true)
	require.ErrorIs(t, r.CaptureUpdate(ctx, chain, func(port.CaptureWriter) error { t.Fatal("read-only callback"); return nil }), port.ErrReadOnly)
	// A second pool sees durable source data and shares the same chain fence.
	w := openTestStore(t, s.schema, false)
	require.NoError(t, w.CaptureView(ctx, chain, func(r port.CaptureReader) error {
		got, e := r.Get(key)
		require.NoError(t, e)
		require.Equal(t, value, got)
		return nil
	}))
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.CaptureUpdate(ctx, chain, func(port.CaptureWriter) error { close(entered); <-release; return nil })
	}()
	<-entered
	blocked, cancel := context.WithTimeout(ctx, time.Second)
	// It must wait for the OTHER pool's chain lock, then time out, rather
	// than entering its callback while the first writer is still active.
	require.Error(t, w.CaptureUpdate(blocked, chain, func(port.CaptureWriter) error { t.Error("cancelled callback"); return nil }))
	cancel()
	close(release)
	require.NoError(t, <-done)
	txCtx, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	tx.SetHeight(1)
	require.ErrorIs(t, s.CaptureUpdate(txCtx, chain, func(port.CaptureWriter) error { t.Fatal("nested callback"); return nil }), port.ErrInvalidData)
	require.NoError(t, s.SetBlock(txCtx, &model.Block{Number: 1, Hash: common.Hash{2}, Time: 1}))
	require.NoError(t, s.SetLatestHeight(txCtx, 1))
	require.NoError(t, tx.Commit())
	_, err = s.RollbackTo(ctx, 0, nil)
	require.NoError(t, err)
	_, err = s.ClearChainData(ctx, nil, false)
	require.NoError(t, err)
	require.NoError(t, r.CaptureView(ctx, chain, func(r port.CaptureReader) error {
		got, e := r.Get(key)
		require.NoError(t, e)
		require.Equal(t, value, got)
		return nil
	}))
}
