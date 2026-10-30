package arcingress

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"path/filepath"
)

type drainStore struct {
	*mutableStatus
	parts []port.ExecutionPart
	bad   atomic.Bool
}

func TestNativeDrainFinishesActiveImportThenWaitsForItsPublication(t *testing.T) {
	p, s, w, peer, h := setupDrain(t)
	batch := rawFixture(t, p.control.Chain, p.head.Tip, 2)
	require.NoError(t, p.persist(t.Context(), batch))
	oracle := p.deps.Canonical.(roundOracle)
	oracle.hashes[2] = batch[0].Block.Hash
	oracle.head = 2
	p.deps.Canonical = oracle
	entered, finish := make(chan struct{}), make(chan struct{})
	var once sync.Once
	complete := func() { once.Do(func() { close(finish) }) }
	w.importBlock = func(ctx context.Context, m importMeta, b, rs []byte, root, receiptsRoot execHash) (importReceipt, error) {
		close(entered)
		select {
		case <-finish:
		case <-ctx.Done():
			return importReceipt{}, ctx.Err()
		}
		digest, err := nativeinput.Digest(m, b, rs)
		if err != nil {
			return importReceipt{}, err
		}
		r := importReceipt{Version: 1, Identity: p.identity, RequestID: m.RequestID, ImportEpoch: m.ImportEpoch, ReadEpoch: w.ready.Head.ReadEpoch + 1, Height: m.Height, Hash: m.Hash, Parent: m.Parent, ParentRoot: m.ParentRoot, Root: root, ReceiptsRoot: receiptsRoot, InputDigest: digest}
		if err = nativeArtifactFixture(completionPath(p.cfg.Native.DataDir, m.ImportEpoch, m.Height), r); err != nil {
			return r, err
		}
		next := nativeHead{Version: 1, Instance: p.identity.Instance, ImportEpoch: r.ImportEpoch, ReadEpoch: r.ReadEpoch, RequestID: r.RequestID, Tip: nativeRef{Height: r.Height, Hash: r.Hash, Root: r.Root}}
		return r, nativeArtifactFixture(filepath.Join(p.cfg.Native.DataDir, headName), next)
	}
	ready := make(chan collector.Config, 1)
	p.collectorRun = func(ctx context.Context, c collector.Config) error { ready <- c; <-ctx.Done(); return ctx.Err() }
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	run := make(chan error, 1)
	go func() { run <- p.runOwnedLoop(ctx, p.runRound) }()
	defer p.Stop()
	defer complete()
	c := <-ready
	select {
	case <-entered:
	case err := <-run:
		t.Fatalf("import did not start: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	c.OnCommitted(h) // the previous block's real ACK cannot drain active block2
	drained := make(chan drainResult, 1)
	go func() { point, err := p.Drain(ctx); drained <- drainResult{point, err} }()
	select {
	case <-peer.streamClosed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.False(t, w.closed.Load(), "active import canceled by admission stop")
	select {
	case result := <-drained:
		t.Fatalf("drain overtook active import: %v", result.err)
	default:
	}
	complete()
	require.Eventually(t, w.closed.Load, time.Second, time.Millisecond)
	select {
	case result := <-drained:
		t.Fatalf("drain overtook publication: %v", result.err)
	default:
	}
	p.mu.Lock()
	n := p.head.Tip
	p.mu.Unlock()
	require.Equal(t, uint64(2), n.Height)
	s.mu.Lock()
	s.control.Visible = execwire.Ref{Generation: 1, Height: 2, Hash: n.Hash}
	s.control.Root = n.Root
	for i := range s.parts {
		s.parts[i].Ref = s.control.Visible
		s.parts[i].Parent = execwire.Hash(common.Hash(w.ready.Head.Tip.Hash))
	}
	h.Ref = s.control.Visible
	s.mu.Unlock()
	c.OnCommitted(h)
	result := <-drained
	require.NoError(t, result.err)
	require.Equal(t, uint64(2), result.point.Ref.Height)
	p.Stop()
	require.Error(t, <-run)
}

func (s *drainStore) ExecutionParts(context.Context, uint64) ([]port.ExecutionPart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]port.ExecutionPart(nil), s.parts...)
	if s.bad.Load() {
		out[0].Digest[0] ^= 1
	}
	return out, nil
}
func (s *drainStore) ExecutionCursor(_ context.Context, w port.ExecutionWriter) (port.ExecutionPart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.parts {
		if p.Writer == w {
			return p, nil
		}
	}
	return port.ExecutionPart{}, port.ErrNotFound
}
func setupDrain(t *testing.T) (*program, *drainStore, *roundWriterFixture, *roundPeerFixture, execwire.Header) {
	t.Helper()
	p, w, peer := roundFixture(t)
	s := &drainStore{mutableStatus: p.deps.Store.(*mutableStatus)}
	s.control.Visible = execwire.Ref{Generation: s.control.Generation, Height: p.head.Tip.Height, Hash: p.head.Tip.Hash}
	s.control.Root = p.head.Tip.Root
	h := execwire.Header{Version: execwire.Version, Chain: s.control.Chain, Ref: s.control.Visible, Digest: execwire.Hash{99}}
	for _, writer := range port.ExecutionWriters() {
		s.parts = append(s.parts, port.ExecutionPart{Writer: writer, Ref: h.Ref, Parent: p.identity.Anchor.Hash, Digest: h.Digest})
	}
	p.deps.Store = s
	return p, s, w, peer, h
}
func TestNativeDrainWaitsForExactDurableACKAndJoinsBeforePark(t *testing.T) {
	p, s, w, peer, h := setupDrain(t)
	ready := make(chan collector.Config, 1)
	joined := make(chan struct{})
	var joinedOnce sync.Once
	p.collectorRun = func(ctx context.Context, c collector.Config) error {
		ready <- c
		<-ctx.Done()
		joinedOnce.Do(func() { close(joined) })
		return ctx.Err()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	run := make(chan error, 1)
	go func() { run <- p.runOwnedLoop(ctx, p.runRound) }()
	defer p.Stop()
	c := <-ready
	drained := make(chan struct {
		point ingress.DrainPoint
		err   error
	}, 1)
	go func() {
		point, err := p.Drain(ctx)
		drained <- struct {
			point ingress.DrainPoint
			err   error
		}{point, err}
	}()
	select {
	case <-peer.streamClosed:
	case result := <-drained:
		t.Fatalf("drain ended before stopping admission: %v", result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Eventually(t, w.closed.Load, time.Second, time.Millisecond)
	select {
	case <-joined:
		t.Fatal("collector canceled before ACK")
	default:
	}
	select {
	case result := <-drained:
		t.Fatalf("drain inferred ACK: %v", result.err)
	default:
	}
	c.OnCommitted(execwire.Header{Version: execwire.Version, Chain: h.Chain, Ref: h.Ref})
	s.bad.Store(true)
	c.OnCommitted(h)
	// A mismatching part cannot authorize parking; it must fail closed.
	result := <-drained
	require.ErrorIs(t, result.err, port.ErrExecutionConflict)
	require.Zero(t, result.point)
	require.True(t, w.closed.Load())
	select {
	case <-joined:
	default:
		t.Fatal("failed drain did not join collector")
	}
	require.Error(t, <-run)
}

func TestNativeDrainParksRunUntilExplicitVerifiedResumeOrStop(t *testing.T) {
	p, _, w, peer, h := setupDrain(t)
	ready := make(chan collector.Config, 1)
	joined := make(chan struct{})
	var joinedOnce sync.Once
	p.collectorRun = func(ctx context.Context, c collector.Config) error {
		ready <- c
		<-ctx.Done()
		joinedOnce.Do(func() { close(joined) })
		return ctx.Err()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	run := make(chan error, 1)
	go func() { run <- p.runOwnedLoop(ctx, p.runRound) }()
	defer p.Stop()
	c := <-ready
	c.OnCommitted(h)
	point, err := p.Drain(ctx)
	require.NoError(t, err)
	require.Equal(t, ingress.DrainPoint{Chain: h.Chain, Ref: h.Ref, Root: p.head.Tip.Root, Digest: h.Digest}, point)
	require.True(t, w.closed.Load())
	require.True(t, peer.closed.Load())
	select {
	case <-joined:
	default:
		t.Fatal("collector not joined")
	}
	p.mu.Lock()
	raw := p.raw
	p.mu.Unlock()
	require.Nil(t, raw)
	select {
	case err := <-run:
		t.Fatalf("parked ingress terminated App: %v", err)
	default:
	}
	// Resume must recheck actual storage. A pending SQL fence stays parked.
	s := p.deps.Store.(*drainStore)
	s.mu.Lock()
	s.control.Maintenance = true
	s.mu.Unlock()
	require.ErrorIs(t, p.Resume(ctx), port.ErrExecutionMaintenance)
	select {
	case err := <-run:
		t.Fatalf("failed resume terminated parked ingress: %v", err)
	default:
	}
	s.mu.Lock()
	s.control.Maintenance = false
	s.mu.Unlock()
	require.NoError(t, p.Resume(ctx))
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("verified Resume did not restart round")
	}
	p.Stop()
	require.ErrorIs(t, <-run, context.Canceled)
}

func TestNativeDrainCancellationJoinsAndDoesNotWaitForRecoveryMutex(t *testing.T) {
	p, _, w, _, _ := setupDrain(t)
	ready := make(chan struct{})
	p.collectorRun = func(ctx context.Context, _ collector.Config) error { close(ready); <-ctx.Done(); return ctx.Err() }
	runCtx, stop := context.WithCancel(t.Context())
	defer stop()
	run := make(chan error, 1)
	go func() { run <- p.runOwnedLoop(runCtx, p.runRound) }()
	defer p.Stop()
	<-ready
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := p.Drain(ctx); done <- err }()
	p.mu.Lock()
	round := p.round
	p.mu.Unlock()
	select {
	case <-round.stop:
	case <-time.After(time.Second):
		t.Fatal("drain did not stop admission")
	}
	cancel()
	select {
	case err := <-done:
		require.True(t, errors.Is(err, context.Canceled))
	case <-time.After(time.Second):
		t.Fatal("drain/Run defer deadlocked on recovery mutex")
	}
	require.True(t, w.closed.Load())
	require.Error(t, <-run)
}

func TestNativeDrainCoverageRejectsPartialOrMixedWriterEvidence(t *testing.T) {
	for _, name := range []string{"missing", "duplicate", "digest", "parent", "generation", "unknown-writer", "ahead-public", "root"} {
		t.Run(name, func(t *testing.T) {
			p, s, _, _, ack := setupDrain(t)
			n := p.head.Tip
			switch name {
			case "missing":
				s.parts = s.parts[:6]
			case "duplicate":
				s.parts[1].Writer = s.parts[0].Writer
			case "digest":
				s.parts[4].Digest[1] ^= 1
			case "parent":
				s.parts[6].Parent[1] ^= 1
			case "generation":
				s.parts[3].Ref.Generation++
			case "unknown-writer":
				s.parts[2].Writer = "unknown"
			case "ahead-public":
				s.control.Visible.Height++
			case "root":
				s.control.Root[1] ^= 1
			}
			ok, err := p.drainCoverage(t.Context(), n, ack)
			require.Error(t, err)
			require.False(t, ok)
		})
	}
}
