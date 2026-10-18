package capture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"
)

type immutableTestReader struct {
	mu                     sync.Mutex
	data                   map[string][]byte
	gets, batches, maxKeys int
	batchErr               error
	extra                  bool
}

func (r *immutableTestReader) Get(k []byte) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gets++
	v, ok := r.data[string(k)]
	if !ok {
		return nil, port.ErrNotFound
	}
	return bytes.Clone(v), nil
}
func (r *immutableTestReader) GetMany(keys [][]byte) (map[string][]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches++
	if r.batchErr != nil {
		return nil, r.batchErr
	}
	if len(keys) > r.maxKeys {
		r.maxKeys = len(keys)
	}
	out := map[string][]byte{}
	for _, k := range keys {
		if v, ok := r.data[string(k)]; ok {
			out[string(k)] = bytes.Clone(v)
		}
	}
	if r.extra {
		out["unrequested"] = []byte{1}
	}
	return out, nil
}
func immutableFixture(t *testing.T) (*immutableTestReader, common.Hash) {
	t.Helper()
	tree := trie.NewEmpty(nil)
	for i := 0; i < 16; i++ {
		require.NoError(t, tree.Update([]byte{byte(i << 4)}, bytes.Repeat([]byte{byte(i + 1)}, 80)))
	}
	root, nodes := tree.Commit(false)
	r := &immutableTestReader{data: map[string][]byte{}}
	for _, node := range nodes.Nodes {
		r.data[string(immutableNodeKey("search/v1", node.Hash))] = bytes.Clone(node.Blob)
	}
	return r, root
}
func readImmutableFixture(tx port.CaptureReader, root common.Hash) error {
	tree, e := immutableTrie(tx, root, "search/v1")
	if e != nil {
		return e
	}
	for i := 0; i < 16; i++ {
		got, e := tree.Get([]byte{byte(i << 4)})
		if e != nil {
			return e
		}
		if !bytes.Equal(got, bytes.Repeat([]byte{byte(i + 1)}, 80)) {
			return fmt.Errorf("wrong immutable value %d", i)
		}
	}
	return nil
}

func TestImmutableReadBatchAndSnapshotCache(t *testing.T) {
	r, root := immutableFixture(t)
	// A Get-only reader remains supported and caches repeated path reads.
	onlyGet := struct{ port.CaptureReader }{r}
	require.NoError(t, readImmutableFixture(newImmutableReads(t.Context(), onlyGet, true), root))
	singles := r.gets
	r.gets = 0
	cache := newImmutableReads(t.Context(), r, true)
	require.NoError(t, readImmutableFixture(cache, root))
	require.NoError(t, readImmutableFixture(cache, root))
	require.Less(t, r.gets+r.batches, singles)
	require.Equal(t, 1, r.gets)
	require.Equal(t, 1, r.batches)
	require.Equal(t, 16, r.maxKeys)
	r2, root2 := immutableFixture(t)
	point := newImmutableReads(t.Context(), r2, false)
	require.NoError(t, readImmutableFixture(point, root2))
	require.NoError(t, readImmutableFixture(point, root2))
	require.Zero(t, r2.batches, "point lookups must not prefetch unrelated sibling paths")
	require.Equal(t, singles, r2.gets)
	first, e := cache.node("search/v1", root)
	require.NoError(t, e)
	first[0] ^= 1
	second, e := cache.node("search/v1", root)
	require.NoError(t, e)
	require.Equal(t, root, crypto.Keccak256Hash(second), "returned bytes must not mutate the cache")
	r.data["cursor"] = []byte{1}
	_, e = cache.Get([]byte("cursor"))
	require.NoError(t, e)
	r.data["cursor"] = []byte{2}
	v, e := cache.Get([]byte("cursor"))
	require.NoError(t, e)
	require.Equal(t, []byte{2}, v, "metadata is never cached")
	ctx, cancel := context.WithCancel(t.Context())
	cache = newImmutableReads(ctx, r, true)
	_, e = cache.node("search/v1", root)
	require.NoError(t, e)
	cancel()
	_, e = cache.node("search/v1", root)
	require.ErrorIs(t, e, context.Canceled)
}

func TestImmutablePrefetchMissingOrCorruptSibling(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		r, root := immutableFixture(t)
		children := immutableChildHashes(r.data[string(immutableNodeKey("search/v1", root))])
		require.Len(t, children, 16)
		key := string(immutableNodeKey("search/v1", children[0]))
		original := r.data[key]
		if corrupt {
			r.data[key] = []byte{0xc0}
		} else {
			delete(r.data, key)
		}
		cache := newImmutableReads(t.Context(), r, true)
		tree, e := immutableTrie(cache, root, "search/v1")
		require.NoError(t, e)
		_, e = tree.Get([]byte{0xf0})
		require.NoError(t, e, "an unrelated sibling is not required")
		_, e = tree.Get([]byte{0})
		require.ErrorIs(t, e, ErrConflict)
		if !corrupt {
			require.ErrorIs(t, e, port.ErrNotFound)
		}
		r.data[key] = original
		_, e = cache.node("search/v1", children[0])
		require.ErrorIs(t, e, ErrConflict, "do not mix snapshots after repair")
		require.NoError(t, readImmutableFixture(newImmutableReads(t.Context(), r, true), root))
	}
	for _, extra := range []bool{false, true} {
		r, root := immutableFixture(t)
		injected := errors.New("batch I/O failed")
		if extra {
			r.extra = true
		} else {
			r.batchErr = injected
		}
		_, e := immutableTrie(newImmutableReads(t.Context(), r, true), root, "search/v1")
		require.ErrorIs(t, e, ErrConflict)
		if !extra {
			require.ErrorIs(t, e, injected)
		}
		require.Equal(t, 1, r.gets, "batch failure must not become single-read fallback")
	}
}

func TestImmutableReadBudgetAndLeafReferences(t *testing.T) {
	r, root := immutableFixture(t)
	cache := newImmutableReads(t.Context(), r, true)
	for i := 0; i <= immutableReadEntries; i++ {
		cache.remember(fmt.Sprint(i), nil, port.ErrNotFound)
	}
	require.Len(t, cache.nodes, immutableReadEntries)
	require.LessOrEqual(t, cache.used, immutableReadBytes)
	require.NoError(t, readImmutableFixture(cache, root), "full cache still performs validated on-demand reads")
	require.Zero(t, r.batches)
	cache = newImmutableReads(t.Context(), r, true)
	for i := 0; i < 100; i++ {
		cache.remember(fmt.Sprint(i), make([]byte, 64<<10), nil)
	}
	require.LessOrEqual(t, cache.used, immutableReadBytes)
	require.Less(t, len(cache.nodes), 100)
	h := common.Hash{1}
	leaf, e := rlp.EncodeToBytes([]any{[]byte{0x20}, h[:]})
	require.NoError(t, e)
	require.Empty(t, immutableChildHashes(leaf), "a 32-byte leaf value is not a hash reference")
	extension, e := rlp.EncodeToBytes([]any{[]byte{0x10}, h[:]})
	require.NoError(t, e)
	require.Equal(t, []common.Hash{h}, immutableChildHashes(extension))
	branch := make([]any, 17)
	for i := range branch {
		branch[i] = []byte{}
	}
	branch[16] = h[:]
	b, e := rlp.EncodeToBytes(branch)
	require.NoError(t, e)
	require.Empty(t, immutableChildHashes(b), "branch terminal values are not children")
	for _, bad := range [][]byte{{}, {0xc1}, {0xff}} {
		require.Empty(t, immutableChildHashes(bad))
	}
}

func TestImmutableReadViewsConcurrent(t *testing.T) {
	r, root := immutableFixture(t)
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- readImmutableFixture(newImmutableReads(t.Context(), r, true), root) }()
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, <-done)
	}
}
