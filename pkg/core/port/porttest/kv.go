package porttest

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// kvPrefix is the keyspace the KV contract writes under.
const kvPrefix = "/porttest/kv/"

// kvKey returns the key of name under kvPrefix.
func kvKey(name string) []byte { return []byte(kvPrefix + name) }

// Keys just outside kvPrefix, before and after it in key order.
var (
	kvBefore = []byte("/porttest/ku")
	kvAfter  = []byte("/porttest/kv0")
)

// testKV checks KVStore, KV and Cursor: values come back as put, a missing
// key is port.ErrNotFound, prefix iteration and range scans visit keys in
// order (or reverse) within their bounds and stop when the callback says so,
// callbacks get copies, cursors move over a bounded range, and KV writes made
// with a block transaction's context belong to that transaction.
func testKV(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("PutGetDeleteHas", func(t *testing.T) {
		s := open[port.KV](t, newStore)
		k := kvKey("a")
		_, err := s.Get(ctx, k)
		assert.ErrorIs(t, err, port.ErrNotFound)
		ok, err := s.Has(ctx, k)
		require.NoError(t, err)
		assert.False(t, ok)

		require.NoError(t, s.Put(ctx, k, []byte("one")))
		v, err := s.Get(ctx, k)
		require.NoError(t, err)
		assert.Equal(t, []byte("one"), v)
		ok, err = s.Has(ctx, k)
		require.NoError(t, err)
		assert.True(t, ok)

		require.NoError(t, s.Put(ctx, k, []byte("two")))
		v, err = s.Get(ctx, k)
		require.NoError(t, err)
		assert.Equal(t, []byte("two"), v, "Put replaces")

		v[0] = 'X'
		v, err = s.Get(ctx, k)
		require.NoError(t, err)
		assert.Equal(t, []byte("two"), v, "the value Get returns is a copy")

		require.NoError(t, s.Delete(ctx, k))
		_, err = s.Get(ctx, k)
		assert.ErrorIs(t, err, port.ErrNotFound)
		ok, err = s.Has(ctx, k)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.NoError(t, s.Delete(ctx, k), "deleting a missing key is not an error")
	})

	t.Run("Iterate", func(t *testing.T) {
		s := open[port.KV](t, newStore)
		kvFill(t, s)
		var keys []string
		require.NoError(t, s.Iterate(ctx, []byte(kvPrefix), func(k, v []byte) bool {
			keys = append(keys, string(k))
			assert.Equal(t, "v:"+string(k), string(v))
			return true
		}))
		assert.Equal(t, kvStrings(kvKey("a"), kvKey("b"), kvKey("c"), kvKey("d"), kvKey("e")), keys,
			"key order, only keys with the prefix")

		keys = nil
		require.NoError(t, s.Iterate(ctx, []byte(kvPrefix), func(k, _ []byte) bool {
			keys = append(keys, string(k))
			return len(keys) < 2
		}))
		assert.Equal(t, kvStrings(kvKey("a"), kvKey("b")), keys, "stops when fn returns false")

		keys = nil
		require.NoError(t, s.Iterate(ctx, kvKey("zz"), func(k, _ []byte) bool {
			keys = append(keys, string(k))
			return true
		}))
		assert.Empty(t, keys, "no key has the prefix")
	})

	t.Run("Scan", func(t *testing.T) {
		s := open[port.KV](t, newStore)
		kvFill(t, s)
		scan := func(lower, upper []byte, reverse bool, max int) []string {
			t.Helper()
			var keys []string
			require.NoError(t, s.Scan(ctx, lower, upper, reverse, func(k, v []byte) bool {
				if bytes.HasPrefix(k, []byte("/porttest/")) {
					assert.Equal(t, "v:"+string(k), string(v))
					keys = append(keys, string(k))
				}
				return max == 0 || len(keys) < max
			}))
			return keys
		}
		assert.Equal(t, kvStrings(kvKey("b"), kvKey("c")), scan(kvKey("b"), kvKey("d"), false, 0), "[lower, upper)")
		assert.Equal(t, kvStrings(kvKey("c"), kvKey("b")), scan(kvKey("b"), kvKey("d"), true, 0), "reverse")
		assert.Equal(t, kvStrings(kvKey("b")), scan(kvKey("b"), kvKey("d"), false, 1), "stops when fn returns false")
		assert.Equal(t, kvStrings(kvKey("c")), scan(kvKey("b"), kvKey("d"), true, 1), "reverse stops too")
		assert.Equal(t, kvStrings(kvKey("d"), kvKey("e"), kvAfter), scan(kvKey("d"), nil, false, 0), "nil upper is unbounded")
		assert.Equal(t, kvStrings(kvAfter, kvKey("e"), kvKey("d")), scan(kvKey("d"), nil, true, 0), "reverse from the end")
		assert.Empty(t, scan(kvKey("x"), kvKey("y"), false, 0), "empty range")
	})

	t.Run("CallbacksGetCopies", func(t *testing.T) {
		s := open[port.KV](t, newStore)
		kvFill(t, s)
		mutate := func(k, v []byte) bool {
			k[len(k)-1] = 'Z'
			v[0] = 'Z'
			return true
		}
		require.NoError(t, s.Iterate(ctx, []byte(kvPrefix), mutate))
		require.NoError(t, s.Scan(ctx, kvKey("a"), kvKey("f"), false, mutate))
		require.NoError(t, s.Scan(ctx, kvKey("a"), kvKey("f"), true, mutate))
		for _, n := range []string{"a", "b", "c", "d", "e"} {
			v, err := s.Get(ctx, kvKey(n))
			require.NoError(t, err)
			assert.Equal(t, "v:"+string(kvKey(n)), string(v))
		}
		_, err := s.Get(ctx, kvKey("Z"))
		assert.ErrorIs(t, err, port.ErrNotFound)
	})

	t.Run("Cursor", func(t *testing.T) {
		s := open[port.KV](t, newStore)
		kvFill(t, s)
		c, err := s.NewCursor(ctx, kvKey("b"), kvKey("e"))
		require.NoError(t, err)
		at := func(name string) {
			t.Helper()
			require.True(t, c.Valid())
			assert.Equal(t, string(kvKey(name)), string(c.Key()))
			assert.Equal(t, "v:"+string(kvKey(name)), string(c.Value()))
		}
		require.True(t, c.First())
		at("b")
		require.True(t, c.Next())
		at("c")
		require.True(t, c.Next())
		at("d")
		assert.False(t, c.Next(), "upper bound is exclusive")
		assert.False(t, c.Valid())

		require.True(t, c.Last())
		at("d")
		require.True(t, c.Prev())
		at("c")
		require.True(t, c.Prev())
		at("b")
		assert.False(t, c.Prev(), "lower bound is inclusive")
		assert.False(t, c.Valid())
		assert.NoError(t, c.Error())
		assert.NoError(t, c.Close())

		c, err = s.NewCursor(ctx, kvKey("d"), nil)
		require.NoError(t, err)
		require.True(t, c.First())
		at("d")
		require.True(t, c.Next())
		at("e")
		require.True(t, c.Next(), "nil upper is unbounded")
		assert.Equal(t, string(kvAfter), string(c.Key()))
		assert.NoError(t, c.Close())

		c, err = s.NewCursor(ctx, kvKey("x"), kvKey("y"))
		require.NoError(t, err)
		assert.False(t, c.First(), "empty range")
		assert.False(t, c.Last())
		assert.False(t, c.Valid())
		assert.NoError(t, c.Error())
		assert.NoError(t, c.Close())
	})

	t.Run("BlockTransaction", func(t *testing.T) {
		s := open[port.KV](t, newStore)
		bt, ok := s.(port.BlockTransactor)
		if !ok {
			t.Skip("store has no block transactions")
		}
		kept, gone, added := kvKey("kept"), kvKey("gone"), kvKey("added")
		require.NoError(t, s.Put(ctx, kept, []byte("old")))
		require.NoError(t, s.Put(ctx, gone, []byte("old")))

		txCtx, tx, err := bt.BeginBlock(ctx)
		require.NoError(t, err)
		require.NoError(t, s.Put(txCtx, kept, []byte("new")))
		require.NoError(t, s.Put(txCtx, added, []byte("new")))
		require.NoError(t, s.Delete(txCtx, gone))

		// Inside: the transaction's writes.
		kvAssertValue(t, txCtx, s, kept, "new")
		kvAssertValue(t, txCtx, s, added, "new")
		kvAssertValue(t, txCtx, s, gone, "")
		assert.Equal(t, kvStrings(added, kept), kvScanAll(t, txCtx, s), "Scan sees the transaction")
		var iterated []string
		require.NoError(t, s.Iterate(txCtx, []byte(kvPrefix), func(k, _ []byte) bool {
			iterated = append(iterated, string(k))
			return true
		}))
		assert.Equal(t, kvStrings(added, kept), iterated, "Iterate sees the transaction")
		c, err := s.NewCursor(txCtx, []byte(kvPrefix), []byte("/porttest/kv0"))
		require.NoError(t, err)
		var cursored []string
		for ok := c.First(); ok; ok = c.Next() {
			cursored = append(cursored, string(c.Key()))
		}
		require.NoError(t, c.Close())
		assert.Equal(t, kvStrings(added, kept), cursored, "a cursor sees the transaction")

		// Outside: the committed state.
		kvAssertValue(t, ctx, s, kept, "old")
		kvAssertValue(t, ctx, s, added, "")
		kvAssertValue(t, ctx, s, gone, "old")
		assert.Equal(t, kvStrings(gone, kept), kvScanAll(t, ctx, s))

		require.NoError(t, tx.Commit())
		kvAssertValue(t, ctx, s, kept, "new")
		kvAssertValue(t, ctx, s, added, "new")
		kvAssertValue(t, ctx, s, gone, "")

		txCtx, tx, err = bt.BeginBlock(ctx)
		require.NoError(t, err)
		require.NoError(t, s.Put(txCtx, kept, []byte("rolled back")))
		require.NoError(t, s.Put(txCtx, gone, []byte("rolled back")))
		require.NoError(t, s.Delete(txCtx, added))
		tx.Rollback()
		kvAssertValue(t, ctx, s, kept, "new")
		kvAssertValue(t, ctx, s, added, "new")
		kvAssertValue(t, ctx, s, gone, "")
	})

	t.Run("RolledBackWithBlock", func(t *testing.T) {
		s := open[port.KV](t, newStore)
		os, ok := s.(orphanStore)
		if !ok {
			t.Skip("store does not roll back blocks")
		}
		c := newChain(3)
		kept, added := kvKey("kept"), kvKey("added")
		for _, b := range c.Blocks {
			txCtx, tx, err := os.BeginBlock(ctx)
			require.NoError(t, err)
			tx.SetHeight(b.Number)
			require.NoError(t, os.SetBlock(txCtx, b))
			kvWriteReceipts(t, txCtx, os, c, b)
			require.NoError(t, os.SetLatestHeight(txCtx, b.Number))
			switch b.Number {
			case 1:
				require.NoError(t, s.Put(txCtx, kept, []byte("block 1")))
			case 2:
				require.NoError(t, s.Put(txCtx, kept, []byte("block 2")))
				require.NoError(t, s.Put(txCtx, added, []byte("block 2")))
			}
			require.NoError(t, tx.Commit())
		}
		_, err := os.RollbackTo(ctx, 1)
		require.NoError(t, err)
		kvAssertValue(t, ctx, s, kept, "block 1")
		kvAssertValue(t, ctx, s, added, "")
	})
}

// kvFill puts keys a..e under kvPrefix and one key on each side of it; each
// value is "v:" followed by the key.
func kvFill(t *testing.T, s port.KV) {
	t.Helper()
	ctx := context.Background()
	// Out of order, to check that order comes from the keys.
	for _, k := range [][]byte{kvKey("d"), kvAfter, kvKey("b"), kvKey("e"), kvBefore, kvKey("a"), kvKey("c")} {
		require.NoError(t, s.Put(ctx, k, append([]byte("v:"), k...)))
	}
}

// kvAssertValue checks the value of key under ctx; "" means missing.
func kvAssertValue(t *testing.T, ctx context.Context, s port.KV, key []byte, want string) {
	t.Helper()
	v, err := s.Get(ctx, key)
	has, herr := s.Has(ctx, key)
	require.NoError(t, herr)
	if want == "" {
		assert.ErrorIs(t, err, port.ErrNotFound, "%s", key)
		assert.False(t, has, "%s", key)
		return
	}
	require.NoError(t, err, "%s", key)
	assert.Equal(t, want, string(v), "%s", key)
	assert.True(t, has, "%s", key)
}

// kvScanAll returns the keys under kvPrefix that Scan visits under ctx.
func kvScanAll(t *testing.T, ctx context.Context, s port.KV) []string {
	t.Helper()
	var keys []string
	require.NoError(t, s.Scan(ctx, []byte(kvPrefix), kvAfter, false, func(k, _ []byte) bool {
		keys = append(keys, string(k))
		return true
	}))
	return keys
}

// kvWriteReceipts stores the fixture receipts of b.
func kvWriteReceipts(t *testing.T, ctx context.Context, s orphanStore, c *chain, b *model.Block) {
	t.Helper()
	for _, tx := range b.Transactions {
		require.NoError(t, s.SetReceipt(ctx, c.receipt(tx.Hash)))
	}
}

func kvStrings(keys ...[]byte) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, string(k))
	}
	return out
}
