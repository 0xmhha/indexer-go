package arcingress

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type rewindStatus struct {
	*mutableStatus
	marker port.ExecutionRewind
}

func (s *rewindStatus) ExecutionRewindStatus(context.Context) (port.ExecutionRewind, error) {
	return s.marker, nil
}

func recoveryFixture(t *testing.T) (*program, *rewindStatus) {
	t.Helper()
	p := rawProgram(t)
	parent := t.TempDir()
	require.NoError(t, os.Chmod(parent, 0700))
	p.cfg.Native.DataDir = filepath.Join(parent, "native")
	require.NoError(t, os.Mkdir(p.cfg.Native.DataDir, 0700))
	for _, name := range []string{"db", "rocksdb", "static_files", ".arc-native-import-receipts"} {
		require.NoError(t, os.Mkdir(filepath.Join(p.cfg.Native.DataDir, name), 0700))
	}
	p.cfg.Native.Executable = filepath.Join(t.TempDir(), "import")
	require.NoError(t, os.WriteFile(p.cfg.Native.Executable, []byte("owned unit fixture; never executed"), 0700))
	p.cfg.Collector.Executable = filepath.Join(t.TempDir(), "replay")
	require.NoError(t, os.WriteFile(p.cfg.Collector.Executable, []byte("owned unit fixture; never executed"), 0700))
	p.cfg.Collector.SpoolDir = filepath.Join(t.TempDir(), "spool")
	require.NoError(t, os.Chmod(filepath.Dir(p.cfg.Collector.SpoolDir), 0700))
	p.deps.Socket = filepath.Join(t.TempDir(), "execution.sock")
	require.NoError(t, os.Chmod(filepath.Dir(p.deps.Socket), 0700))
	c := p.deps.Control
	boot := bootstrapReceipt{Version: 1, ArcRevision: ArcRevision, RethRevision: RethRevision, Anchor: checkpoint.Manifest{Version: 1, Mode: "snapshot", Coverage: "full", Chain: c.Chain, Ref: c.Anchor, Root: c.AnchorRoot, Header: []byte{0xc0}}, Floor: 0, Next: 1, StateDigest: execwire.Hash{8}}
	encoded, err := json.Marshal(boot)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(p.cfg.Native.DataDir, nativeinput.BootstrapName), encoded, 0600))
	p.bootstrap = execwire.Hash(sha256.Sum256(encoded))
	p.identity.BootstrapDigest = p.bootstrap
	batch := rawFixture(t, p.control.Chain, p.identity.Anchor, 1)
	require.NoError(t, p.persist(t.Context(), batch))
	decoded, err := verifyBatch(p.control.Chain, p.identity.Anchor, batch)
	require.NoError(t, err)
	m := p.meta(p.head, batch[0].Block)
	_, digest, err := encodeInput(m, decoded.encoded, decoded.receipts)
	require.NoError(t, err)
	r := importReceipt{Version: 1, Identity: p.identity, RequestID: 1, ImportEpoch: 1, ReadEpoch: 2, Height: 1, Hash: m.Hash, Parent: m.Parent, ParentRoot: m.ParentRoot, Root: execwire.Hash(decoded.block.StateRoot), ReceiptsRoot: execwire.Hash(decoded.block.ReceiptRoot), InputDigest: digest}
	p.head = nativeHead{Version: 1, Instance: p.identity.Instance, ImportEpoch: 1, ReadEpoch: 2, RequestID: 1, Tip: nativeRef{Height: 1, Hash: m.Hash, Root: r.Root}}
	writeNativeArtifact(t, completionPath(p.cfg.Native.DataDir, 1, 1), r)
	writeNativeArtifact(t, filepath.Join(p.cfg.Native.DataDir, identityName), p.identity)
	writeNativeArtifact(t, filepath.Join(p.cfg.Native.DataDir, headName), p.head)
	marker := port.ExecutionRewind{RequestGeneration: 1, From: c.Visible, Target: c.Anchor, FromRoot: c.Root, Root: c.AnchorRoot}
	marker.Target.Generation = 2
	s := &rewindStatus{mutableStatus: p.deps.Store.(*mutableStatus), marker: marker}
	s.control.Generation = 2
	s.control.Visible.Generation = 2
	s.control.Rewinding = true
	p.deps.Store = s
	p.deps.Canonical = canonicalFunc(func(_ context.Context, h uint64) (common.Hash, error) {
		if h != 0 {
			return common.Hash{}, errors.New("unexpected lookup")
		}
		return common.Hash(marker.Target.Hash), nil
	})
	return p, s
}
func publishTransitionFixture(t *testing.T, p *program, r nativeinput.TransitionRequest) nativeinput.Transition {
	t.Helper()
	tr, err := nativeinput.ExpectedTransition(r)
	require.NoError(t, err)
	dir := filepath.Join(p.cfg.Native.DataDir, ".arc-native-import-epochs")
	require.NoError(t, os.MkdirAll(dir, 0700))
	writeNativeArtifact(t, filepath.Join(dir, "2.json"), tr)
	writeNativeArtifact(t, filepath.Join(p.cfg.Native.DataDir, headName), tr.Head)
	return tr
}
func TestNativeRecoveryWaitsForJoinedRoundAndReusesExactCompletedMarkerBeforeSQLResume(t *testing.T) {
	p, s := recoveryFixture(t)
	round := &roundRun{stop: make(chan struct{}), done: make(chan struct{})}
	p.round = round
	joined := make(chan struct{})
	go func() { <-round.stop; close(joined); close(round.done) }()
	defer func() { round.pause(); <-round.done }()
	calls := 0
	p.transitionNative = func(_ context.Context, r nativeinput.TransitionRequest) (nativeinput.Transition, error) {
		select {
		case <-joined:
		default:
			t.Error("native unwind overtook child/operation joins")
		}
		calls++
		require.Equal(t, s.marker, r.Marker)
		return publishTransitionFixture(t, p, r), nil
	}
	require.NoError(t, p.Recover(t.Context(), s.marker))
	first, err := p.raw.ReadOwner(t.Context())
	require.NoError(t, err)
	var c rawControl
	require.NoError(t, strictJSON(first.State, &c))
	require.Equal(t, uint8(2), c.Version)
	require.Equal(t, uint64(2), c.RawEpoch)
	require.Equal(t, uint64(2), c.NativeEpoch)
	require.Equal(t, uint64(2), c.Generation)
	require.Equal(t, p.identity.Anchor, c.Received)
	require.Equal(t, 3, p.raw.Stats().Records, "rewind must retain original raw data")
	require.NoError(t, p.Recover(t.Context(), s.marker))
	again, err := p.raw.ReadOwner(t.Context())
	require.NoError(t, err)
	require.Equal(t, first.State, again.State, "same marker cannot increment raw/native epochs twice")
	require.Equal(t, 2, calls)
	require.Error(t, p.Recovered(t.Context(), s.control), "worker cannot resume before SQL fresh/COMMIT")
	s.control.Rewinding = false
	s.control.Visible = s.marker.Target
	s.control.Root = s.marker.Root
	require.NoError(t, p.Recovered(t.Context(), s.control))
	require.Equal(t, uint64(2), p.deps.Control.Generation)
	require.NoError(t, p.Check(t.Context()), "restart check must accept verified later epoch after SQL resume")
}
func TestNativeRecoveryFailurePreservesRawAndSQLFence(t *testing.T) {
	p, s := recoveryFixture(t)
	before, err := p.raw.ReadOwner(t.Context())
	require.NoError(t, err)
	failure := errors.New("native partial store stays HOLD")
	p.transitionNative = func(context.Context, nativeinput.TransitionRequest) (nativeinput.Transition, error) {
		return nativeinput.Transition{}, failure
	}
	require.ErrorIs(t, p.Recover(t.Context(), s.marker), failure)
	after, err := p.raw.ReadOwner(t.Context())
	require.NoError(t, err)
	require.Equal(t, before.State, after.State)
	require.True(t, s.control.Rewinding)
}

func TestNativeRecoveryStopCancelsAndJoinsBeforeClosingRaw(t *testing.T) {
	p, s := recoveryFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	raw := p.raw
	entered := make(chan struct{})
	joined := make(chan struct{})
	p.transitionNative = func(ctx context.Context, _ nativeinput.TransitionRequest) (nativeinput.Transition, error) {
		close(entered)
		<-ctx.Done()
		_, err := raw.ReadOwner(t.Context())
		close(joined)
		if err != nil {
			return nativeinput.Transition{}, errors.New("raw closed before transition joined")
		}
		return nativeinput.Transition{}, ctx.Err()
	}
	recovered := make(chan error, 1)
	go func() { recovered <- p.Recover(ctx, s.marker) }()
	defer func() { cancel(); <-joined }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	p.Stop()
	select {
	case <-joined:
	default:
		t.Fatal("Stop returned without cancelling/joining the owned transition")
	}
	require.ErrorIs(t, <-recovered, context.Canceled)
}

func TestNativeRecoveryCheckCannotInspectWhileExclusiveTransitionIsActive(t *testing.T) {
	p, s := recoveryFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	entered := make(chan struct{})
	p.transitionNative = func(ctx context.Context, _ nativeinput.TransitionRequest) (nativeinput.Transition, error) {
		close(entered)
		<-ctx.Done()
		return nativeinput.Transition{}, ctx.Err()
	}
	done := make(chan error, 1)
	go func() { done <- p.Recover(ctx, s.marker) }()
	defer func() { cancel(); require.ErrorIs(t, <-done, context.Canceled) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.ErrorIs(t, p.Check(ctx), port.ErrExecutionRewinding, "startup inspection must not use cached normal control or acquire native/raw readers during recovery")
}
