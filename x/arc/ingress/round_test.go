package arcingress

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/exporter"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundWriterFixture struct {
	ready       nativeReady
	closed      atomic.Bool
	importBlock func(context.Context, importMeta, []byte, []byte, execHash, execHash) (importReceipt, error)
}

func (w *roundWriterFixture) Initial() nativeReady { return w.ready }
func (w *roundWriterFixture) Close()               { w.closed.Store(true) }
func (w *roundWriterFixture) Import(ctx context.Context, m importMeta, b, r []byte, root, receiptsRoot execHash) (importReceipt, error) {
	if w.importBlock == nil {
		return importReceipt{}, errors.New("unexpected import")
	}
	return w.importBlock(ctx, m, b, r, root, receiptsRoot)
}

type roundPeerFixture struct {
	input        chan []chainfeed.Envelope
	closed       atomic.Bool
	streamClosed chan struct{}
	closeOnce    sync.Once
	observed     atomic.Int32
}

func (p *roundPeerFixture) Subscribe(context.Context, model.FeedBlock, chainfeed.ReadLimits) (chainfeed.Stream, error) {
	return &roundStreamFixture{p: p}, nil
}
func (p *roundPeerFixture) ObserveCaptured(*window.Store, []window.Reference) error {
	if p.closed.Load() {
		return errors.New("peer closed before active native input observed")
	}
	p.observed.Add(1)
	return nil
}
func (p *roundPeerFixture) Close() error { p.closed.Store(true); return nil }

type roundStreamFixture struct{ p *roundPeerFixture }

func (s *roundStreamFixture) Next(context.Context) (chainfeed.Envelope, error) {
	return chainfeed.Envelope{}, errors.New("batch required")
}
func (s *roundStreamFixture) NextBatch(ctx context.Context) ([]chainfeed.Envelope, error) {
	select {
	case b := <-s.p.input:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *roundStreamFixture) Close() error {
	s.p.closeOnce.Do(func() { close(s.p.streamClosed) })
	return nil
}

type roundOracle struct {
	hashes map[uint64]common.Hash
	head   uint64
}

func (o roundOracle) HashAt(ctx context.Context, h uint64) (common.Hash, error) {
	if err := ctx.Err(); err != nil {
		return common.Hash{}, err
	}
	v, ok := o.hashes[h]
	if !ok {
		return common.Hash{}, source.ErrNotFound
	}
	return v, nil
}
func (o roundOracle) Head(context.Context) (uint64, error) { return o.head, nil }
func roundFixture(t *testing.T) (*program, *roundWriterFixture, *roundPeerFixture) {
	t.Helper()
	p, s := recoveryFixture(t)
	s.control = p.deps.Control
	p.deps.Store = s.mutableStatus
	w := &roundWriterFixture{ready: nativeReady{Version: 1, Identity: p.identity, Head: p.head}}
	peer := &roundPeerFixture{input: make(chan []chainfeed.Envelope, 1), streamClosed: make(chan struct{})}
	p.nativeStart = func(context.Context) (nativeProcess, error) { return w, nil }
	p.peerStart = func(context.Context, model.FeedBlock) (peerProcess, error) { return peer, nil }
	p.collectorRun = func(ctx context.Context, _ collector.Config) error { <-ctx.Done(); return ctx.Err() }
	p.deps.Canonical = roundOracle{hashes: map[uint64]common.Hash{0: common.Hash(p.identity.Anchor.Hash), 1: common.Hash(p.head.Tip.Hash)}, head: 1}
	return p, w, peer
}
func TestNativeRoundNativeAheadOrShorterRPCProposesOnExistingCollectorWithoutAssigningGeneration(t *testing.T) {
	for _, shorter := range []bool{false, true} {
		t.Run(map[bool]string{false: "native-ahead", true: "shorter-rpc"}[shorter], func(t *testing.T) {
			p, w, peer := roundFixture(t)
			want := execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{42}}
			hashes := map[uint64]common.Hash{0: common.Hash(p.identity.Anchor.Hash), 1: common.Hash(want.Hash)}
			head := uint64(1)
			if shorter {
				delete(hashes, 1)
				head = 0
				want.Height = 0
				want.Hash = p.identity.Anchor.Hash
			}
			p.deps.Canonical = roundOracle{hashes: hashes, head: head}
			var fenced atomic.Bool
			p.deps.FenceReorg = func(_ context.Context, ref execwire.Ref) error {
				assert.Equal(t, want, ref)
				assert.True(t, w.closed.Load())
				assert.True(t, peer.closed.Load())
				s := p.deps.Store.(*mutableStatus)
				s.mu.Lock()
				s.control.ReorgPending = true
				s.mu.Unlock()
				fenced.Store(true)
				return nil
			}
			p.collectorRun = func(ctx context.Context, c collector.Config) error {
				if !assert.NotNil(t, c.Native.Managed) {
					return ErrNative
				}
				assert.Equal(t, p.identity, *c.Native.Managed)
				assert.Equal(t, p.cfg.Collector.SpoolDir, c.SpoolDir)
				select {
				case ref := <-c.Proposals:
					assert.True(t, fenced.Load(), "read fence must precede proposal transport")
					assert.True(t, w.closed.Load(), "first writer must join before proposal")
					assert.True(t, peer.closed.Load())
					assert.Equal(t, want, ref)
					return &exporter.CanonicalChange{Ref: ref}
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := p.runRound(ctx, &roundRun{stop: make(chan struct{}), done: make(chan struct{})})
			require.ErrorIs(t, err, errAwaitCoordinator)
			status, err := p.deps.Store.ExecutionStatus(t.Context())
			require.NoError(t, err)
			require.Equal(t, uint64(1), status.Generation)
			require.False(t, status.Rewinding)
			require.Equal(t, uint64(1), p.head.ImportEpoch)
			require.Equal(t, 3, p.raw.Stats().Records)
		})
	}
}
func TestNativeRoundPauseCancelsPeerPreparationAndJoinsBeforeReturn(t *testing.T) {
	p, w, _ := roundFixture(t)
	entered := make(chan struct{})
	p.peerStart = func(ctx context.Context, _ model.FeedBlock) (peerProcess, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	r := &roundRun{stop: make(chan struct{}), done: make(chan struct{})}
	p.round = r
	done := make(chan error, 1)
	go func() { err := p.runRound(ctx, r); close(r.done); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("round ended before peer prepare: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, p.quiesce(ctx))
	require.ErrorIs(t, <-done, port.ErrExecutionRewinding)
	require.True(t, w.closed.Load())
}
func TestNativeFrozenCanonicalBelowAnchorIsOnlyProposalAndPreservesOriginalAnchor(t *testing.T) {
	p := rawProgram(t)
	p.identity.Anchor.Height = 5
	p.head.Tip = nativeRef{Height: 10, Hash: execwire.Hash{10}, Root: p.identity.Anchor.Root}
	p.deps.Control.Anchor.Height = 5
	p.deps.Control.Visible.Height = 5
	s := p.deps.Store.(*mutableStatus)
	s.control = p.deps.Control
	p.deps.Canonical = roundOracle{hashes: map[uint64]common.Hash{4: {4}}, head: 4}
	proposal, same, err := p.frozenCanonical(t.Context(), p.head.Tip)
	require.NoError(t, err)
	require.False(t, same)
	require.Equal(t, execwire.Ref{Generation: 1, Height: 4, Hash: execwire.Hash{4}}, proposal, "actual borrowed head is a proposal; existing finder must keep deep recovery HOLD")
	require.Equal(t, uint64(5), p.identity.Anchor.Height)
	require.Equal(t, uint64(1), s.control.Generation)
	require.False(t, s.control.Rewinding)
}
func TestNativeRoundRawOnlyForkRotatesRawEpochAfterAllChildrenJoin(t *testing.T) {
	p, w, peer := roundFixture(t)
	closed := make(chan struct{})
	p.collectorRun = func(ctx context.Context, _ collector.Config) error { <-ctx.Done(); close(closed); return ctx.Err() }
	wrong := p.head.Tip
	wrong.Hash = execwire.Hash{99}
	peer.input <- rawFixture(t, p.control.Chain, wrong, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := p.runRound(ctx, &roundRun{stop: make(chan struct{}), done: make(chan struct{})})
	require.ErrorIs(t, err, errRestartRound)
	require.True(t, w.closed.Load())
	require.True(t, peer.closed.Load())
	select {
	case <-closed:
	default:
		t.Fatal("collector remained live during raw rotation")
	}
	require.Equal(t, uint64(2), p.control.RawEpoch)
	require.Equal(t, uint64(1), p.control.NativeEpoch)
	require.Equal(t, uint64(1), p.control.Generation)
	require.Equal(t, p.head.Tip, p.control.Received)
	require.Equal(t, 3, p.raw.Stats().Records)
}
func TestNativeRoundPauseFinishesActiveImportBeforeClosingPeerAndNative(t *testing.T) {
	p, w, peer := roundFixture(t)
	batch := rawFixture(t, p.control.Chain, p.head.Tip, 2)
	require.NoError(t, p.persist(t.Context(), batch))
	oracle := p.deps.Canonical.(roundOracle)
	oracle.hashes[2] = batch[0].Block.Hash
	oracle.head = 2
	p.deps.Canonical = oracle
	entered, finish := make(chan struct{}), make(chan struct{})
	var finishOnce sync.Once
	complete := func() { finishOnce.Do(func() { close(finish) }) }
	var cancelled atomic.Bool
	w.importBlock = func(ctx context.Context, m importMeta, b, rs []byte, root, receiptsRoot execHash) (importReceipt, error) {
		close(entered)
		select {
		case <-finish:
		case <-ctx.Done():
			cancelled.Store(true)
			return importReceipt{}, ctx.Err()
		}
		digest, err := nativeinput.Digest(m, b, rs)
		if err != nil {
			return importReceipt{}, err
		}
		r := importReceipt{Version: 1, Identity: p.identity, RequestID: m.RequestID, ImportEpoch: m.ImportEpoch, ReadEpoch: w.ready.Head.ReadEpoch + 1, Height: m.Height, Hash: m.Hash, Parent: m.Parent, ParentRoot: m.ParentRoot, Root: root, ReceiptsRoot: receiptsRoot, InputDigest: digest}
		if err = nativeArtifactFixture(completionPath(p.cfg.Native.DataDir, m.ImportEpoch, m.Height), r); err != nil {
			return importReceipt{}, err
		}
		h := nativeHead{Version: 1, Instance: p.identity.Instance, ImportEpoch: r.ImportEpoch, ReadEpoch: r.ReadEpoch, RequestID: r.RequestID, Tip: nativeRef{Height: r.Height, Hash: r.Hash, Root: r.Root}}
		if err = nativeArtifactFixture(filepath.Join(p.cfg.Native.DataDir, headName), h); err != nil {
			return importReceipt{}, err
		}
		return r, nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	round := &roundRun{stop: make(chan struct{}), done: make(chan struct{})}
	p.round = round
	done := make(chan error, 1)
	go func() { err := p.runRound(ctx, round); close(round.done); done <- err }()
	defer func() { cancel(); complete(); <-round.done }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("round ended before active import: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	paused := make(chan error, 1)
	go func() { paused <- p.quiesce(ctx) }()
	select {
	case <-peer.streamClosed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.False(t, w.closed.Load())
	require.False(t, peer.closed.Load())
	select {
	case err := <-paused:
		t.Fatalf("pause overtook active import: %v", err)
	default:
	}
	complete()
	require.NoError(t, <-paused)
	require.ErrorIs(t, <-done, port.ErrExecutionRewinding)
	require.False(t, cancelled.Load())
	require.True(t, w.closed.Load())
	require.True(t, peer.closed.Load())
	require.Equal(t, int32(1), peer.observed.Load())
}
func TestNativeRoundMissingCanonicalRawWithMatchingTipStaysHold(t *testing.T) {
	p, _, _ := roundFixture(t)
	two := rawFixture(t, p.control.Chain, p.control.Received, 2)
	require.NoError(t, p.persist(t.Context(), two))
	three := rawFixture(t, p.control.Chain, p.control.Received, 3)
	require.NoError(t, p.persist(t.Context(), three))
	original := p.control.Received
	p.deps.Canonical = roundOracle{hashes: map[uint64]common.Hash{0: common.Hash(p.identity.Anchor.Hash), 1: common.Hash(p.head.Tip.Hash), 2: {99}, 3: three[0].Block.Hash}, head: 3}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := p.runRound(ctx, &roundRun{stop: make(chan struct{}), done: make(chan struct{})})
	require.ErrorIs(t, err, ErrNative, "matching raw tip does not prove an unavailable intermediate canonical input; preserve for inspection")
	require.Equal(t, uint64(1), p.control.RawEpoch)
	require.Equal(t, original, p.control.Received)
	require.Equal(t, 9, p.raw.Stats().Records)
}
func TestNativeProgramRunUsesOwnedRoundAndStopJoinsAllWorkers(t *testing.T) {
	p, w, peer := roundFixture(t)
	entered := make(chan struct{})
	p.collectorRun = func(ctx context.Context, _ collector.Config) error { close(entered); <-ctx.Done(); return ctx.Err() }
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("production Run bypassed managed round: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	p.Stop()
	require.ErrorIs(t, <-done, context.Canceled)
	require.True(t, w.closed.Load())
	require.True(t, peer.closed.Load())
}
