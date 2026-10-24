package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/stretchr/testify/require"
)

const e4First = "e4.writer.first"
const e4Second = "e4.writer.second"

var errE4Writer = errors.New("second writer failed")

type e4Feature string

func (f e4Feature) Name() string { return string(f) }
func (f e4Feature) Requires() []string {
	if f == e4Second {
		return []string{e4First}
	}
	return nil
}
func (f e4Feature) Register(r feature.Registrar) error {
	d := r.Deps()
	var fail bool
	if err := d.DecodeSettings(string(f), &fail); err != nil {
		return err
	}
	r.OnBlock(feature.BlockHandlerFunc(func(ctx context.Context, b *feature.Block) error {
		kv := d.Storage.(port.KVStore)
		if f == e4Second {
			data, err := kv.Get(ctx, []byte(ExecutionFeaturePrefix+e4First))
			if err != nil {
				return err
			}
			var checkpoint ExecutionFeatureCheckpoint
			if json.Unmarshal(data, &checkpoint) != nil || checkpoint.Ref.Height != b.Model.Number || checkpoint.Ref.Hash != execwire.Hash(b.Model.Hash) {
				return port.ErrExecutionConflict
			}
		}
		if err := kv.Put(ctx, []byte("writer-data:"+f), []byte("applied")); err != nil {
			return err
		}
		if fail {
			return errE4Writer
		}
		return nil
	}))
	return nil
}
func init() { feature.Register(e4Feature(e4First)); feature.Register(e4Feature(e4Second)) }

func TestExecutionUnitCheckpointsAreOrderedAtomicAndRetryable(t *testing.T) {
	h := newChainHarness(t, &Config{StatePrefetch: true}, nil)
	b, rs, err := h.src.BlockWithReceipts(t.Context(), 1)
	require.NoError(t, err)
	store := &executionFeatureStore{PebbleStorage: h.db}
	h.f.storage = store
	fresh := &executionCanonicalProbe{hash: b.Hash}
	h.f.SetExecutionCanonical(fresh)
	set := func(fail bool) {
		p, e := feature.Build([]string{e4Second, e4First}, feature.Deps{Storage: h.db, Settings: func(name string, into any) error { *into.(*bool) = fail && name == e4Second; return nil }})
		require.NoError(t, e)
		h.f.SetFeatures(p)
	}
	r := execwire.Result{Ref: execwire.Ref{Generation: 1, Height: b.Number, Hash: execwire.Hash(b.Hash)}, Parent: execwire.Hash(b.ParentHash), StateRoot: execwire.Hash(b.StateRoot)}
	set(true)
	require.ErrorIs(t, h.f.PublishExecution(t.Context(), r, b, rs, execwire.Hash{1}), errE4Writer)
	require.Zero(t, fresh.calls)
	require.False(t, store.published)
	for _, name := range []string{e4First, e4Second} {
		_, e := h.db.Get(t.Context(), []byte(ExecutionFeaturePrefix+name))
		require.ErrorIs(t, e, port.ErrNotFound)
		_, e = h.db.Get(t.Context(), []byte("writer-data:"+name))
		require.ErrorIs(t, e, port.ErrNotFound)
	}
	set(false)
	require.NoError(t, h.f.PublishExecution(t.Context(), r, b, rs, execwire.Hash{1}))
	require.Equal(t, 1, fresh.calls)
	for _, name := range []string{e4First, e4Second} {
		data, e := h.db.Get(t.Context(), []byte(ExecutionFeaturePrefix+name))
		require.NoError(t, e)
		var checkpoint ExecutionFeatureCheckpoint
		require.NoError(t, json.Unmarshal(data, &checkpoint))
		require.Equal(t, r.Ref, checkpoint.Ref)
		require.Equal(t, execwire.Hash{1}, checkpoint.Digest)
	}
	require.NoError(t, h.f.PublishExecution(t.Context(), r, b, rs, execwire.Hash{1}))
	require.Equal(t, 2, fresh.calls)
	require.Equal(t, 1, store.applied)
}
