package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestProjectionDurability(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	require.ErrorIs(t, s.RequireBlockDurability(ctx), port.ErrInvalidData)
	c, tx, e := s.BeginBlock(ctx)
	require.NoError(t, e)
	defer tx.Rollback()
	var fsync string
	require.NoError(t, s.q(c).QueryRow(c, "SHOW fsync").Scan(&fsync))
	_, e = s.q(c).Exec(c, "SET LOCAL synchronous_commit = off")
	require.NoError(t, e)
	e = s.RequireBlockDurability(c)
	if fsync != "on" {
		require.ErrorContains(t, e, "fsync must be on")
		return
	}
	require.NoError(t, e)
	var mode string
	require.NoError(t, s.q(c).QueryRow(c, "SHOW synchronous_commit").Scan(&mode))
	require.Equal(t, "on", mode)
}
func TestCaptureViewUsesProjectionTransaction(t *testing.T) {
	ctx := context.Background()
	schema := newTestSchema(t)
	s, e := Open(ctx, Options{DSN: testDSN(t), Schema: schema, MaxConns: 1})
	require.NoError(t, e)
	defer func() { require.NoError(t, s.Close()) }()
	var fsync string
	require.NoError(t, s.pool.QueryRow(ctx, "SHOW fsync").Scan(&fsync))
	if fsync != "on" {
		t.Skip("capture requires fsync=on")
	}
	ch := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	require.NoError(t, s.CaptureUpdate(ctx, ch, func(w port.CaptureWriter) error { return w.Put([]byte("version"), []byte("one")) }))
	other := openTestStore(t, schema, false)
	c, tx, e := s.BeginBlock(ctx)
	require.NoError(t, e)
	defer tx.Rollback()
	deadline, cancel := context.WithTimeout(c, time.Second)
	defer cancel()
	require.NoError(t, s.CaptureView(deadline, ch, func(r port.CaptureReader) error {
		v, e := r.Get([]byte("version"))
		require.Equal(t, []byte("one"), v)
		return e
	}), "one connection must not nest a pool transaction")
	// The shared capture row lock survives the read callback until the writer
	// commits/rolls back, so CaptureUpdate cannot reset this generation midway.
	timeout, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	require.Error(t, other.CaptureUpdate(timeout, ch, func(port.CaptureWriter) error { t.Error("entered through active projection read fence"); return nil }))
	tx.Rollback()
	require.NoError(t, other.CaptureUpdate(ctx, ch, func(w port.CaptureWriter) error { return w.Put([]byte("version"), []byte("two")) }))
}
