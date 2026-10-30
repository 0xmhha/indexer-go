package arcingress

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"github.com/ethereum/go-ethereum/common"
)

type originOwnerStore interface {
	ingress.StatusReader
	ExecutionIngressOwner(context.Context) (port.ExecutionIngressOwner, error)
	BindExecutionIngress(context.Context, port.ExecutionIngressBinding) error
}
type originSnapshot struct {
	store    originOwnerStore
	policy   port.ExecutionIngressPolicy
	control  port.ExecutionControl
	identity nativeIdentity
	raw      *window.Store
	rawEpoch uint64
}

func sourceOwner(ctx context.Context, s ingress.StatusReader, policy *port.ExecutionIngressPolicy, c port.ExecutionConfig, bound bool) (port.ExecutionIngressOwner, error) {
	var empty port.ExecutionIngressOwner
	store, ok := s.(originOwnerStore)
	if !ok || policy == nil || policy.Validate() != nil || policy.Module != ModuleName || policy.Chain != c.Chain || policy.Anchor != c.Anchor || policy.AnchorRoot != c.AnchorRoot {
		return empty, port.ErrExecutionConflict
	}
	o, err := store.ExecutionIngressOwner(ctx)
	if err != nil {
		return empty, err
	}
	digest, err := policy.Digest()
	if err != nil || o.Policy != *policy || o.PolicyDigest != digest || bound && o.Binding == nil {
		return empty, port.ErrExecutionConflict
	}
	return o, nil
}
func (p *program) originSnapshotLocked() (originSnapshot, error) {
	store, ok := p.deps.Store.(originOwnerStore)
	if !ok || p.deps.Policy == nil || p.raw == nil {
		return originSnapshot{}, port.ErrExecutionConflict
	}
	return originSnapshot{store: store, policy: *p.deps.Policy, control: p.deps.Control, identity: p.identity, raw: p.raw, rawEpoch: p.control.RawEpoch}, nil
}
func (p *program) actualSourceBinding(ctx context.Context, s originSnapshot) (port.ExecutionIngressBinding, error) {
	raw, err := s.raw.ReadOwner(ctx)
	if err != nil {
		return port.ExecutionIngressBinding{}, err
	}
	identity, err := json.Marshal(s.identity)
	if err != nil {
		return port.ExecutionIngressBinding{}, err
	}
	admission, err := collector.ManagedAdmissionBinding(collector.Options{Managed: &s.identity, Chain: s.control.Chain, StateProofs: p.deps.StateProofs})
	if err != nil {
		return port.ExecutionIngressBinding{}, err
	}
	digest, err := s.policy.Digest()
	if err != nil {
		return port.ExecutionIngressBinding{}, err
	}
	return port.ExecutionIngressBinding{Version: 1, PolicyDigest: digest, NativeInstance: s.identity.Instance, BootstrapDigest: s.identity.BootstrapDigest, RawStoreID: execwire.Hash(raw.StoreID), SourcePolicyDigest: admission, NativeIdentity: identity}, nil
}
func (p *program) bindSourceOwner(parent context.Context, ready nativeReady) error {
	ctx, cancel := context.WithTimeout(parent, p.cfg.Timeout)
	defer cancel()
	p.mu.Lock()
	s, err := p.originSnapshotLocked()
	p.mu.Unlock()
	if err != nil {
		return err
	}
	if ready.Identity != s.identity {
		return ErrNative
	}
	if _, err = sourceOwner(ctx, s.store, &s.policy, s.control.ExecutionConfig, false); err != nil {
		return err
	}
	binding, err := p.actualSourceBinding(ctx, s)
	if err != nil {
		return err
	}
	return s.store.BindExecutionIngress(ctx, binding)
}

// Registration precedes bounded admission so Stop also cancels an operation
// waiting for memory. Existing E1 part admission bounds these callers to two.
func (p *program) beginOrigin(parent context.Context) (context.Context, originSnapshot, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := parent.Err(); err != nil {
		return nil, originSnapshot{}, nil, err
	}
	if p.stopped || p.originsClosing {
		return nil, originSnapshot{}, nil, port.ErrClosed
	}
	if p.recovering {
		return nil, originSnapshot{}, nil, port.ErrExecutionRewinding
	}
	s, err := p.originSnapshotLocked()
	if err != nil {
		return nil, s, nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	if p.originCancels == nil {
		p.originCancels = make(map[uint64]context.CancelFunc)
	}
	p.originSequence++
	id := p.originSequence
	p.originCancels[id] = cancel
	p.originWG.Add(1)
	finish := sync.OnceFunc(func() { cancel(); p.mu.Lock(); delete(p.originCancels, id); p.mu.Unlock(); p.originWG.Done() })
	return ctx, s, finish, nil
}
func (p *program) joinOrigins(closing bool) {
	p.mu.Lock()
	if closing {
		p.originsClosing = true
	}
	cancels := make([]context.CancelFunc, 0, len(p.originCancels))
	for _, cancel := range p.originCancels {
		cancels = append(cancels, cancel)
	}
	p.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	p.originWG.Wait()
}

func (p *program) resolveOriginIO(ctx context.Context, s originSnapshot, in port.ExecutionIngressInput, owner port.ExecutionIngressOwner) (a port.ExecutionIngressArchive, err error) {
	view, err := nativestore.OpenReadView(ctx, p.cfg.Native.DataDir)
	if err != nil {
		return a, err
	}
	defer func() { err = errors.Join(err, view.Close()) }()
	lease, err := view.Acquire(ctx)
	if err != nil {
		return a, err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	proof, err := nativeinput.ReadReferenceProof(p.cfg.Native.DataDir, s.identity, nativeRef{Height: in.Ref.Height, Hash: in.Ref.Hash, Root: in.Root})
	if err != nil {
		return a, err
	}
	result := execwire.Result{Ref: in.Ref, Parent: in.Parent, StateRoot: in.Root, ReceiptsRoot: in.ReceiptsRoot, BlockRLP: in.BlockRLP, ReceiptsRLP: in.ReceiptsRLP}
	if nativeinput.ValidatePayload(proof, result) != nil {
		return a, ErrNative
	}
	batch, refs, err := p.readBatch(ctx, model.FeedBlock{Chain: s.controlChain(), Number: in.Ref.Height, Hash: common.Hash(in.Ref.Hash)})
	if err != nil {
		return a, err
	}
	decoded, err := decodeBatch(s.controlChain(), batch)
	if err != nil {
		return a, err
	}
	if !bytes.Equal(decoded.encoded, in.BlockRLP) || !bytes.Equal(decoded.receipts, in.ReceiptsRLP) {
		return a, ErrNative
	}
	provenance, err := json.Marshal(struct {
		Identity nativeIdentity
		Receipt  nativeinput.Receipt
		Meta     nativeinput.Meta
	}{s.identity, *proof.Receipt, nativeinput.ReceiptMeta(*proof.Receipt)})
	if err != nil {
		return a, err
	}
	a = port.ExecutionIngressArchive{Version: 1, OwnerDigest: owner.BindingDigest, Ref: in.Ref, Parent: in.Parent, Digest: in.Digest, InputDigest: proof.Receipt.InputDigest, NativeEpoch: proof.Receipt.ImportEpoch, NativeReadEpoch: proof.Receipt.ReadEpoch, NativeRequestID: proof.Receipt.RequestID, ObservedRawEpoch: s.rawEpoch, Provenance: provenance}
	for i, e := range batch {
		reference, e2 := json.Marshal(refs[i])
		if e2 != nil {
			return a, e2
		}
		a.Datasets = append(a.Datasets, port.ExecutionIngressDataset{Kind: e.Kind, Codec: e.Codec, Reference: reference, Digest: execwire.Hash(sha256.Sum256(e.Payload)), PayloadBytes: uint64(len(e.Payload)), Payload: e.Payload})
	}
	return a, a.Validate()
}
func (s originSnapshot) controlChain() model.FeedChain {
	return model.FeedChain{ID: s.control.Chain.ID, GenesisHash: common.Hash(s.control.Chain.Genesis)}
}

func (p *program) ResolveExecutionOrigin(parent context.Context, in port.ExecutionIngressInput) (port.ExecutionIngressArchive, func(), error) {
	var empty port.ExecutionIngressArchive
	ctx, s, finish, err := p.beginOrigin(parent)
	if err != nil {
		return empty, nil, err
	}
	defer finish()
	memory, err := p.active(ctx)
	if err != nil {
		return empty, nil, err
	}
	release := sync.OnceFunc(memory)
	success := false
	defer func() {
		if !success {
			release()
		}
	}()
	if in.Chain != s.control.Chain || in.Ref.Generation != s.control.Generation {
		return empty, nil, port.ErrExecutionStale
	}
	owner, err := sourceOwner(ctx, s.store, &s.policy, s.control.ExecutionConfig, true)
	if err != nil {
		return empty, nil, err
	}
	binding, err := p.actualSourceBinding(ctx, s)
	if err != nil {
		return empty, nil, err
	}
	digest, err := binding.Digest()
	if err != nil || owner.BindingDigest != digest {
		return empty, nil, port.ErrExecutionConflict
	}
	a, err := p.resolveOriginIO(ctx, s, in, owner)
	if err != nil {
		return empty, nil, err
	}
	// Every native/raw lease is closed before this SQL read or the caller's tx.
	c, err := s.store.ExecutionStatus(ctx)
	if err != nil {
		return empty, nil, err
	}
	if !c.BaselineReady || c.Generation != s.control.Generation || c.ExecutionConfig != s.control.ExecutionConfig || c.Rewinding || c.ReorgPending || c.Maintenance {
		return empty, nil, port.ErrExecutionStale
	}
	if err = ctx.Err(); err != nil {
		return empty, nil, err
	}
	success = true
	return a, release, nil
}
