package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// TestMigrateIsIdempotentAndSerialized: stores opened together on a new
// schema migrate it once; opening again changes nothing.
func TestMigrateIsIdempotentAndSerialized(t *testing.T) {
	schema := newTestSchema(t)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := Open(context.Background(), Options{DSN: testDSN(t), Schema: schema, MaxConns: 2})
			if err == nil {
				_ = s.Close()
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	s := openTestStore(t, schema, false)
	require.NoError(t, s.Migrate(context.Background()))
	var applied int
	require.NoError(t, s.pool.QueryRow(context.Background(), "SELECT count(*) FROM schema_migrations").Scan(&applied))
	assert.Equal(t, SchemaVersion(), applied, "each migration applied once")
}

// TestReadOnlyStore: a read-only store needs a migrated schema, refuses
// writes, and sees what a writer commits (the API processes of R4-1).
func TestReadOnlyStore(t *testing.T) {
	ctx := context.Background()
	schema := newTestSchema(t)
	_, err := Open(ctx, Options{DSN: testDSN(t), Schema: schema, ReadOnly: true})
	require.Error(t, err, "the schema is not migrated yet")

	w := openTestStore(t, schema, false)
	r := openTestStore(t, schema, true)
	assert.ErrorIs(t, r.SetLatestHeight(ctx, 1), port.ErrReadOnly)
	assert.ErrorIs(t, r.Put(ctx, []byte("k"), []byte("v")), port.ErrReadOnly)
	_, _, err = r.BeginBlock(ctx)
	assert.ErrorIs(t, err, port.ErrReadOnly)

	b := &model.Block{Number: 7, Hash: common.Hash{7}, BaseFee: nil}
	txCtx, tx, err := w.BeginBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, w.SetBlock(txCtx, b))
	require.NoError(t, w.SetLatestHeight(txCtx, 7))
	_, err = r.GetBlock(ctx, 7)
	assert.ErrorIs(t, err, port.ErrNotFound, "uncommitted writes are invisible to other processes")
	require.NoError(t, tx.Commit())
	got, err := r.GetBlock(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, b.Hash, got.Hash)
	h, err := r.GetLatestHeight(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(7), h)
}

// TestNewerSchemaRefused: a build does not open a schema migrated by a
// newer build.
func TestNewerSchemaRefused(t *testing.T) {
	ctx := context.Background()
	schema := newTestSchema(t)
	s := openTestStore(t, schema, false)
	_, err := s.pool.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, 'future')", SchemaVersion()+1)
	require.NoError(t, err)
	_, err = Open(ctx, Options{DSN: testDSN(t), Schema: schema})
	assert.ErrorContains(t, err, "newer than this build")
	_, err = Open(ctx, Options{DSN: testDSN(t), Schema: schema, ReadOnly: true})
	assert.Error(t, err)
}

// TestKVAcrossBatches: scans and cursors read in batches; they must cross
// batch boundaries in both directions, also inside a block transaction.
func TestKVAcrossBatches(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	const n = 3*kvBatch + 17
	key := func(i int) []byte { return []byte(fmt.Sprintf("/x/%05d", i)) }
	txCtx, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	for i := 0; i < n; i++ {
		require.NoError(t, s.Put(txCtx, key(i), []byte{byte(i)}))
	}
	for _, c := range []context.Context{txCtx, nil} {
		if c == nil {
			require.NoError(t, tx.Commit())
			c = ctx
		}
		var fwd, rev []string
		require.NoError(t, s.Scan(c, []byte("/x/"), []byte("/x0"), false, func(k, _ []byte) bool { fwd = append(fwd, string(k)); return true }))
		require.NoError(t, s.Scan(c, []byte("/x/"), nil, true, func(k, _ []byte) bool { rev = append(rev, string(k)); return true }))
		require.Len(t, fwd, n)
		require.Len(t, rev, n)
		for i := 0; i < n; i++ {
			require.Equal(t, string(key(i)), fwd[i])
			require.Equal(t, string(key(n-1-i)), rev[i])
		}

		cur, err := s.NewCursor(c, key(10), key(n-10))
		require.NoError(t, err)
		require.True(t, cur.First())
		for i := 10; i < kvBatch+20; i++ {
			require.Equal(t, string(key(i)), string(cur.Key()))
			require.True(t, cur.Next())
		}
		for i := kvBatch + 20; i > 10; i-- {
			require.Equal(t, string(key(i)), string(cur.Key()))
			require.True(t, cur.Prev(), "back across the batch boundary at %d", i)
		}
		require.Equal(t, string(key(10)), string(cur.Key()))
		require.False(t, cur.Prev())
		require.True(t, cur.Last())
		require.Equal(t, string(key(n-11)), string(cur.Key()))
		require.NoError(t, cur.Close())
	}
}

// TestCallbacksMayUseTheTransaction: a scan callback can read and write
// through the same block transaction.
func TestCallbacksMayUseTheTransaction(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for i := 0; i < 5; i++ {
		require.NoError(t, s.Put(ctx, []byte(fmt.Sprintf("/a/%d", i)), []byte("v")))
	}
	txCtx, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, s.Iterate(txCtx, []byte("/a/"), func(k, v []byte) bool {
		require.NoError(t, s.Put(txCtx, append([]byte("/b/"), k[3:]...), v))
		_, err := s.Get(txCtx, k)
		require.NoError(t, err)
		return true
	}))
	require.NoError(t, tx.Commit())
	var n int
	require.NoError(t, s.Iterate(ctx, []byte("/b/"), func(_, _ []byte) bool { n++; return true }))
	assert.Equal(t, 5, n)
}
