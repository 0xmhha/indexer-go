package arcingress

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type originStatus struct {
	*rewindStatus
	owner    port.ExecutionIngressOwner
	observed chan struct{}
	once     sync.Once
	ownerMu  sync.Mutex
}

func (s *originStatus) ExecutionIngressOwner(context.Context) (port.ExecutionIngressOwner, error) {
	if s.observed != nil {
		s.once.Do(func() { close(s.observed) })
	}
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	o := s.owner
	if o.Binding != nil {
		b := *o.Binding
		b.NativeIdentity = bytes.Clone(b.NativeIdentity)
		o.Binding = &b
	}
	return o, nil
}
func (s *originStatus) BindExecutionIngress(_ context.Context, b port.ExecutionIngressBinding) error {
	d, err := b.Digest()
	if err != nil {
		return err
	}
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	if s.owner.Binding != nil && s.owner.BindingDigest != d {
		return port.ErrExecutionConflict
	}
	s.owner.Binding = &b
	s.owner.BindingDigest = d
	return nil
}

func TestArcOriginReadyAndResolveSharePreparedState(t *testing.T) {
	p, _, in := originFixture(t)
	ready := nativeReady{Version: 1, Identity: p.identity, Head: p.head}
	start, done := make(chan struct{}), make(chan error, 2)
	go func() { <-start; done <- p.initializeRaw(t.Context(), ready) }()
	go func() {
		<-start
		_, release, err := p.ResolveExecutionOrigin(t.Context(), in)
		if release != nil {
			release()
		}
		done <- err
	}()
	close(start)
	for range 2 {
		require.NoError(t, <-done)
	}
}

func originFixture(t *testing.T) (*program, *originStatus, port.ExecutionIngressInput) {
	t.Helper()
	p, old := recoveryFixture(t)
	old.control = p.deps.Control
	old.control.Generation = 7
	old.control.Visible.Generation = 7
	p.deps.Control = old.control
	previous, _ := json.Marshal(p.control)
	p.control.Version = 2
	p.control.NativeEpoch = p.head.ImportEpoch
	p.control.Generation = 7
	p.control.RawEpoch = 17
	next, _ := json.Marshal(p.control)
	require.NoError(t, p.raw.CompareOwner(t.Context(), previous, next))
	policy := port.ExecutionIngressPolicy{Version: 1, Module: ModuleName, Chain: p.deps.Control.Chain, Anchor: p.deps.Control.Anchor, AnchorRoot: p.deps.Control.AnchorRoot, Requirements: execwire.Hash{9}}
	digest, err := policy.Digest()
	require.NoError(t, err)
	p.deps.Policy = &policy
	s := &originStatus{rewindStatus: old, owner: port.ExecutionIngressOwner{Policy: policy, PolicyDigest: digest}}
	p.deps.Store = s
	rawOwner, err := p.raw.ReadOwner(t.Context())
	require.NoError(t, err)
	identity, err := json.Marshal(p.identity)
	require.NoError(t, err)
	admission, err := collector.ManagedAdmissionBinding(collector.Options{Managed: &p.identity, Chain: p.deps.Control.Chain, StateProofs: p.deps.StateProofs})
	require.NoError(t, err)
	require.NoError(t, s.BindExecutionIngress(t.Context(), port.ExecutionIngressBinding{Version: 1, PolicyDigest: digest, NativeInstance: p.identity.Instance, BootstrapDigest: p.identity.BootstrapDigest, RawStoreID: execwire.Hash(rawOwner.StoreID), SourcePolicyDigest: admission, NativeIdentity: identity}))
	id := p.head.Tip
	proof, err := nativeinput.ReadReferenceProof(p.cfg.Native.DataDir, p.identity, id)
	require.NoError(t, err)
	batch, _, err := p.readBatch(t.Context(), model.FeedBlock{Chain: p.control.Chain, Number: id.Height, Hash: common.Hash(id.Hash)})
	require.NoError(t, err)
	decoded, err := decodeBatch(p.control.Chain, batch)
	require.NoError(t, err)
	in := port.ExecutionIngressInput{Chain: p.deps.Control.Chain, Ref: execwire.Ref{Generation: 7, Height: id.Height, Hash: id.Hash}, Parent: proof.Receipt.Parent, Digest: execwire.Hash{42}, Root: id.Root, ReceiptsRoot: proof.Receipt.ReceiptsRoot, BlockRLP: decoded.encoded, ReceiptsRLP: decoded.receipts}
	return p, s, in
}

func TestArcOriginModuleRequiresThePersistentOriginContract(t *testing.T) {
	require.True(t, ingress.OwnsOrigins(ModuleName))
}

type unreadyOriginStatus struct{ *originStatus }

func (s unreadyOriginStatus) ExecutionStatus(ctx context.Context) (port.ExecutionControl, error) {
	c, err := s.originStatus.ExecutionStatus(ctx)
	c.BaselineReady = false
	return c, err
}

func TestArcOriginRejectsUnreadyBaselineAfterIO(t *testing.T) {
	p, s, in := originFixture(t)
	p.deps.Store = unreadyOriginStatus{s}
	_, release, err := p.ResolveExecutionOrigin(t.Context(), in)
	if release != nil {
		release()
	}
	require.ErrorIs(t, err, port.ErrExecutionStale)
}

func TestArcOriginPreparedCheckRejectsChangedOwnerBeforeChildren(t *testing.T) {
	p, s, _ := originFixture(t)
	require.NoError(t, p.Check(t.Context()))
	s.owner.Policy.Requirements[0]++
	require.ErrorIs(t, p.Check(t.Context()), port.ErrExecutionConflict)
}

func TestArcOriginReadyBindsNativeRawAndCollectorIdentityExactlyOnce(t *testing.T) {
	p, s, _ := originFixture(t)
	s.owner.Binding = nil
	s.owner.BindingDigest = execwire.Hash{}
	ready := nativeReady{Version: 1, Identity: p.identity, Head: p.head}
	require.NoError(t, p.bindSourceOwner(t.Context(), ready))
	require.NotNil(t, s.owner.Binding)
	require.Equal(t, p.identity.Instance, s.owner.Binding.NativeInstance)
	require.NoError(t, p.bindSourceOwner(t.Context(), ready))
	s.owner.Binding.RawStoreID[0]++
	s.owner.BindingDigest, _ = s.owner.Binding.Digest()
	require.ErrorIs(t, p.bindSourceOwner(t.Context(), ready), port.ErrExecutionConflict)
}

func TestArcOriginResolvesExactNativeAndRawBeforeMemoryOnlyHandoff(t *testing.T) {
	p, _, in := originFixture(t)
	a, release, err := p.ResolveExecutionOrigin(t.Context(), in)
	require.NoError(t, err)
	require.NotNil(t, release)
	defer release()
	require.NoError(t, a.Validate())
	require.Equal(t, in.Ref, a.Ref)
	require.EqualValues(t, 1, a.NativeEpoch)
	require.EqualValues(t, 17, a.ObservedRawEpoch)
	require.EqualValues(t, 0, p.raw.Stats().Readers)
	for _, d := range a.Datasets {
		require.Equal(t, execwire.Hash(sha256.Sum256(d.Payload)), d.Digest)
	}
	// A consumer's SQL/memory loan may retain no native read gate.
	view := filepath.Join(filepath.Dir(p.cfg.Native.DataDir), "."+filepath.Base(p.cfg.Native.DataDir)+".arc-native-import-view")
	f, err := os.OpenFile(view, os.O_CREATE|os.O_RDWR, 0600)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.NoError(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	require.NoError(t, syscall.Flock(int(f.Fd()), syscall.LOCK_UN))
	p.Stop()
	release()
	release()
	_, _, err = p.ResolveExecutionOrigin(t.Context(), in)
	require.ErrorIs(t, err, port.ErrClosed)
}

func TestArcOriginRejectsChangedInputsAndKeepsPendingEvidence(t *testing.T) {
	p, _, in := originFixture(t)
	in.ReceiptsRLP = append(bytes.Clone(in.ReceiptsRLP), 0)
	_, release, err := p.ResolveExecutionOrigin(t.Context(), in)
	if release != nil {
		release()
	}
	require.Error(t, err)
	in.ReceiptsRLP = []byte{0xc0}
	pending := filepath.Join(p.cfg.Native.DataDir, ".arc-native-import.pending")
	require.NoError(t, os.WriteFile(pending, []byte("incomplete native import"), 0600))
	_, release, err = p.ResolveExecutionOrigin(t.Context(), in)
	if release != nil {
		release()
	}
	require.Error(t, err)
	_, err = os.Stat(pending)
	require.NoError(t, err)
}

func TestArcOriginStopCancelsAndJoinsBlockedNativeView(t *testing.T) {
	p, s, in := originFixture(t)
	view := filepath.Join(filepath.Dir(p.cfg.Native.DataDir), "."+filepath.Base(p.cfg.Native.DataDir)+".arc-native-import-view")
	f, err := os.OpenFile(view, os.O_CREATE|os.O_RDWR, 0600)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.NoError(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	s.observed = make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, release, err := p.ResolveExecutionOrigin(ctx, in)
		if release != nil {
			release()
		}
		done <- err
	}()
	select {
	case <-s.observed:
	case err := <-done:
		t.Fatalf("origin ended before read gate: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stopped := make(chan struct{})
	go func() { p.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("Stop did not join blocked origin")
	}
	select {
	case err := <-done:
		require.Error(t, err)
	case <-ctx.Done():
		t.Fatal("origin did not finish")
	}
}
