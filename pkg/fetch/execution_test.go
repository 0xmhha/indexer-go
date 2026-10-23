package fetch

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/source"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type executionFeatureStore struct {
	*storagepkg.PebbleStorage
	published           bool
	coreWrites, applied int
}

func (s *executionFeatureStore) SetBlock(context.Context, *model.Block) error {
	s.coreWrites++
	return errors.New("duplicate core write")
}
func (s *executionFeatureStore) SetReceipt(context.Context, *model.Receipt) error {
	s.coreWrites++
	return errors.New("duplicate receipt write")
}
func (s *executionFeatureStore) CommitExecution(ctx context.Context, p port.ExecutionPublication, apply, fresh func(context.Context) error) (bool, error) {
	if s.published {
		return false, fresh(ctx)
	}
	txCtx, tx, err := s.BeginBlock(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	tx.SetHeight(p.Ref.Height)
	if err := apply(txCtx); err != nil {
		return false, err
	}
	if err := fresh(txCtx); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	s.published = true
	s.applied++
	return true, nil
}

type executionCanonicalProbe struct {
	source.CanonicalReader
	calls int
	hash  common.Hash
	fail  error
}

func (p *executionCanonicalProbe) HashAt(context.Context, uint64) (common.Hash, error) {
	p.calls++
	return p.hash, p.fail
}
func TestExecutionFeaturePublicationDoesNotSkipExistingCoreOrDuplicateWrites(t *testing.T) {
	h := newChainHarness(t, &Config{StatePrefetch: true}, nil)
	ctx := t.Context()
	b, rs, err := h.src.BlockWithReceipts(ctx, 1)
	require.NoError(t, err)
	require.NoError(t, h.db.SetBlock(ctx, b))
	store := &executionFeatureStore{PebbleStorage: h.db}
	h.f.storage = store
	fresh := &executionCanonicalProbe{hash: b.Hash}
	h.f.SetExecutionCanonical(fresh)
	handled := 0
	setPreparingFeature(t, h, func(context.Context, *feature.Block) (feature.PreparedHandler, error) {
		return feature.PreparedHandler{Handler: feature.BlockHandlerFunc(func(ctx context.Context, _ *feature.Block) error {
			handled++
			return h.db.Put(ctx, []byte("execution-feature"), []byte("done"))
		})}, nil
	})
	r := execwire.Result{Ref: execwire.Ref{Generation: 1, Height: b.Number, Hash: execwire.Hash(b.Hash)}, Parent: execwire.Hash(b.ParentHash), StateRoot: execwire.Hash(b.StateRoot)}
	require.NoError(t, h.f.PublishExecution(ctx, r, b, rs, execwire.Hash{1}))
	require.Equal(t, 1, handled)
	require.Zero(t, store.coreWrites)
	require.Equal(t, 1, store.applied)
	require.Equal(t, 1, fresh.calls)
	seq, err := h.db.LastOutboxSeq(ctx)
	require.NoError(t, err)
	require.Positive(t, seq)
	require.NoError(t, h.f.PublishExecution(ctx, r, b, rs, execwire.Hash{1}))
	require.Equal(t, 1, handled)
	require.Equal(t, 2, fresh.calls)
	next, err := h.db.LastOutboxSeq(ctx)
	require.NoError(t, err)
	require.Equal(t, seq, next)
}
func TestExecutionFeatureFreshFailureAndMissingReaderFailClosed(t *testing.T) {
	h := newChainHarness(t, &Config{}, nil)
	b, rs, err := h.src.BlockWithReceipts(t.Context(), 1)
	require.NoError(t, err)
	store := &executionFeatureStore{PebbleStorage: h.db}
	h.f.storage = store
	r := execwire.Result{Ref: execwire.Ref{Generation: 1, Height: b.Number, Hash: execwire.Hash(b.Hash)}, Parent: execwire.Hash(b.ParentHash), StateRoot: execwire.Hash(b.StateRoot)}
	require.Error(t, h.f.PublishExecution(t.Context(), r, b, rs, execwire.Hash{1}))
	h.f.SetExecutionCanonical(&executionCanonicalProbe{hash: common.Hash{99}})
	require.ErrorIs(t, h.f.PublishExecution(t.Context(), r, b, rs, execwire.Hash{1}), ErrReorg)
	require.False(t, store.published)
	seq, err := h.db.LastOutboxSeq(t.Context())
	require.NoError(t, err)
	require.Zero(t, seq)
}

type executionRecoveryStore struct {
	*executionFeatureStore
	port.ExecutionStore
	point  execwire.Ref
	status port.ExecutionControl
	calls  int
}

func (s *executionRecoveryStore) ResumeExecutionRewind(ctx context.Context, _ port.UndoHook, fresh func(context.Context, execwire.Ref) error) (*port.Reorg, error) {
	if ctx.Value(inWriterKey{}) != true {
		return nil, errors.New("recovery outside feature writer")
	}
	s.calls++
	return nil, fresh(ctx, s.point)
}
func (*executionRecoveryStore) BeginExecutionRewind(context.Context, execwire.Ref, execwire.Hash) (port.ExecutionRewind, error) {
	return port.ExecutionRewind{}, port.ErrInvalidData
}
func (*executionRecoveryStore) ExecutionRewindStatus(context.Context) (port.ExecutionRewind, error) {
	return port.ExecutionRewind{}, port.ErrNotFound
}
func (s *executionRecoveryStore) ExecutionStatus(context.Context) (port.ExecutionControl, error) {
	return s.status, nil
}
func TestExecutionFeatureRecoveryUsesWriterAndFreshCanonical(t *testing.T) {
	h := newChainHarness(t, &Config{}, nil)
	store := &executionRecoveryStore{executionFeatureStore: &executionFeatureStore{PebbleStorage: h.db}, point: execwire.Ref{Generation: 2, Height: 3, Hash: execwire.Hash{7}}}
	h.f.storage = store
	fresh := &executionCanonicalProbe{hash: common.Hash(store.point.Hash)}
	h.f.SetExecutionCanonical(fresh)
	require.NoError(t, h.f.RecoverExecution(t.Context()))
	require.Equal(t, 1, store.calls)
	require.Equal(t, 1, fresh.calls)
	fresh.hash = common.Hash{9}
	require.ErrorIs(t, h.f.RecoverExecution(t.Context()), ErrReorg)
	h.f.SetExecutionCanonical(nil)
	require.Error(t, h.f.RecoverExecution(t.Context()))
	require.Equal(t, 2, store.calls)
}

type executionForkProbe struct {
	hashes map[uint64]common.Hash
	calls  []uint64
}

func (p *executionForkProbe) HashAt(_ context.Context, h uint64) (common.Hash, error) {
	p.calls = append(p.calls, h)
	hash, ok := p.hashes[h]
	if !ok {
		return common.Hash{}, source.ErrNotFound
	}
	return hash, nil
}
func TestExecutionRecoveryTargetFindsNewestPublicCommonParent(t *testing.T) {
	h := newChainHarness(t, &Config{}, nil)
	ctx := t.Context()
	hashes := map[uint64]common.Hash{}
	for n := uint64(0); n <= 2; n++ {
		b, _, err := h.src.BlockWithReceipts(ctx, n)
		require.NoError(t, err)
		b.StateRoot = common.Hash{byte(n + 5)}
		require.NoError(t, h.db.SetBlock(ctx, b))
		hashes[n] = b.Hash
	}
	store := &executionRecoveryStore{executionFeatureStore: &executionFeatureStore{PebbleStorage: h.db}, status: port.ExecutionControl{ExecutionConfig: port.ExecutionConfig{Anchor: execwire.Ref{Generation: 1, Height: 0, Hash: execwire.Hash(hashes[0])}}, Generation: 2, Visible: execwire.Ref{Generation: 2, Height: 2, Hash: execwire.Hash(hashes[2])}, Rewinding: true, BaselineReady: true}}
	h.f.storage = store
	canonical := &executionForkProbe{hashes: map[uint64]common.Hash{0: hashes[0], 1: hashes[1], 2: common.Hash{99}}}
	h.f.SetExecutionCanonical(canonical)
	target, root, err := h.f.ExecutionRecoveryTarget(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), target.Height)
	require.Equal(t, uint64(2), target.Generation)
	require.Equal(t, execwire.Hash(hashes[1]), target.Hash)
	require.Equal(t, execwire.Hash{6}, root)
	require.Equal(t, []uint64{2, 1}, canonical.calls)
	canonical.hashes = map[uint64]common.Hash{0: common.Hash{98}}
	_, _, err = h.f.ExecutionRecoveryTarget(ctx)
	require.ErrorIs(t, err, ErrReorgTooDeep)
}

func TestExecutionFeatureCannotRetargetFreshByMutatingModel(t *testing.T) {
	h := newChainHarness(t, &Config{StatePrefetch: true}, nil)
	b, rs, err := h.src.BlockWithReceipts(t.Context(), 1)
	require.NoError(t, err)
	store := &executionFeatureStore{PebbleStorage: h.db}
	h.f.storage = store
	r := execwire.Result{Ref: execwire.Ref{Generation: 1, Height: b.Number, Hash: execwire.Hash(b.Hash)}, Parent: execwire.Hash(b.ParentHash), StateRoot: execwire.Hash(b.StateRoot)}
	other := h.chain.Block(2).Block.Hash()
	fresh := &executionCanonicalProbe{hash: other}
	h.f.SetExecutionCanonical(fresh)
	setPreparingFeature(t, h, func(context.Context, *feature.Block) (feature.PreparedHandler, error) {
		return feature.PreparedHandler{Handler: feature.BlockHandlerFunc(func(_ context.Context, b *feature.Block) error { b.Model.Number = 2; b.Model.Hash = other; return nil })}, nil
	})
	err = h.f.PublishExecution(t.Context(), r, b, rs, execwire.Hash{1})
	require.ErrorIs(t, err, port.ErrExecutionConflict)
	require.False(t, store.published)
	require.Zero(t, fresh.calls, "immutable identity mismatch fails before fresh")
}
