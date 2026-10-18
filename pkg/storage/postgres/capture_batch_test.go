package postgres

import (
	"bytes"
	"context"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestCaptureBatchSnapshotBindingAndOwnership(t *testing.T) {
	s, e := Open(t.Context(), Options{DSN: testDSN(t), Schema: newTestSchema(t), MaxConns: 1})
	require.NoError(t, e)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	chain := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	other := model.FeedChain{ID: "1", GenesisHash: common.Hash{2}}
	key := []byte("x'); DROP TABLE capture_records; --\x00")
	require.NoError(t, s.CaptureUpdate(t.Context(), chain, func(w port.CaptureWriter) error {
		if e := w.Put(key, []byte("first")); e != nil {
			return e
		}
		return w.Put([]byte("empty"), []byte{})
	}))
	require.NoError(t, s.CaptureUpdate(t.Context(), other, func(w port.CaptureWriter) error { return w.Put(key, []byte("other-genesis")) }))
	w := openTestStore(t, s.schema, false)
	ro := openTestStore(t, s.schema, true)
	var escaped port.CaptureBatchReader
	require.NoError(t, ro.CaptureView(t.Context(), chain, func(r port.CaptureReader) error {
		batch := r.(port.CaptureBatchReader)
		escaped = batch
		keys := [][]byte{[]byte("missing"), key, []byte("empty"), key}
		got, e := batch.GetMany(keys)
		require.NoError(t, e)
		require.Len(t, got, 2)
		require.Equal(t, []byte("first"), got[string(key)])
		_, present := got["empty"]
		require.True(t, present)
		got[string(key)][0] = 'X'
		require.NoError(t, w.CaptureUpdate(t.Context(), chain, func(w port.CaptureWriter) error { return w.Put(key, []byte("second")) }))
		got, e = batch.GetMany([][]byte{key})
		require.NoError(t, e)
		require.Equal(t, []byte("first"), got[string(key)], "same repeatable-read snapshot and caller-owned bytes")
		return nil
	}))
	_, e = escaped.GetMany(nil)
	require.ErrorIs(t, e, port.ErrClosed)
	require.NoError(t, ro.CaptureView(t.Context(), chain, func(r port.CaptureReader) error {
		got, e := r.(port.CaptureBatchReader).GetMany([][]byte{key})
		require.NoError(t, e)
		require.Equal(t, []byte("second"), got[string(key)], "new view sees the committed version")
		return nil
	}))
	// A bound block transaction reuses its connection, including pool-one use.
	txCtx, tx, e := s.BeginBlock(t.Context())
	require.NoError(t, e)
	require.NoError(t, s.CaptureView(txCtx, chain, func(r port.CaptureReader) error {
		got, e := r.(port.CaptureBatchReader).GetMany([][]byte{key})
		require.NoError(t, e)
		require.Equal(t, []byte("second"), got[string(key)])
		return nil
	}))
	tx.Rollback()
}

func TestCaptureBatchBoundsAndStickyFailure(t *testing.T) {
	s := newTestStore(t)
	chain := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	for name, keys := range map[string][][]byte{
		"count": make([][]byte, port.CaptureBatchKeyLimit+1), "empty-key": {{}}, "long-key": {make([]byte, port.CaptureKeyLimit+1)},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, s.CaptureUpdate(t.Context(), chain, func(w port.CaptureWriter) error {
				_, e := w.(port.CaptureBatchReader).GetMany(keys)
				require.ErrorIs(t, e, port.ErrInvalidData)
				_ = w.Put([]byte(name), []byte("must rollback"))
				return nil
			}), port.ErrInvalidData)
			require.NoError(t, s.CaptureView(t.Context(), chain, func(r port.CaptureReader) error {
				_, e := r.Get([]byte(name))
				require.ErrorIs(t, e, port.ErrNotFound)
				return nil
			}))
		})
	}
	require.NoError(t, s.CaptureUpdate(t.Context(), chain, func(w port.CaptureWriter) error {
		for _, key := range []string{"a", "b", "c", "d"} {
			if e := w.Put([]byte(key), bytes.Repeat([]byte{1}, port.CaptureBatchValueLimit)); e != nil {
				return e
			}
		}
		return w.Put([]byte("oversize"), make([]byte, port.CaptureBatchValueLimit+1))
	}))
	require.NoError(t, s.CaptureView(t.Context(), chain, func(r port.CaptureReader) error {
		keys := make([][]byte, port.CaptureBatchKeyLimit)
		for i := range keys {
			keys[i] = []byte("a")
		}
		got, e := r.(port.CaptureBatchReader).GetMany(keys)
		require.NoError(t, e)
		require.Len(t, got, 1)
		require.Len(t, got["a"], port.CaptureBatchValueLimit)
		return nil
	}))
	for _, keys := range [][][]byte{{[]byte("oversize")}, {[]byte("a"), []byte("b"), []byte("c"), []byte("d")}} {
		require.ErrorIs(t, s.CaptureView(t.Context(), chain, func(r port.CaptureReader) error {
			_, e := r.(port.CaptureBatchReader).GetMany(keys)
			require.ErrorIs(t, e, port.ErrInvalidData)
			return nil
		}), port.ErrInvalidData)
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.ErrorIs(t, s.CaptureView(ctx, chain, func(r port.CaptureReader) error {
		cancel()
		_, e := r.(port.CaptureBatchReader).GetMany(nil)
		require.ErrorIs(t, e, context.Canceled)
		return nil
	}), context.Canceled)
}
