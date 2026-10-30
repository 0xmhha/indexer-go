package arcingress

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
)

// RawRetentionOptions names existing storage only. It provisions nothing and
// grants no E3 or native ownership. Operational invocation remains explicit.
type RawRetentionOptions struct {
	NativeDataDir string
	Window        window.Options
	Source        spool.Options
	Permit        retention.Permit
	StateProofs   *stateproof.Selection
}
type RawRetentionStore interface {
	ingress.RawDeletionStore
	ExecutionIngressOwner(context.Context) (port.ExecutionIngressOwner, error)
}

func CollectRaw(ctx context.Context, s RawRetentionStore, o RawRetentionOptions) (int, error) {
	removed := 0
	err := withRawRetention(ctx, s, o, func(owner port.ExecutionIngressOwner, proof nativeinput.Proof, ack execwire.Hash) (err error) {
		raw, err := window.OpenOwnedChecked(o.Window, ownerDomain, func(window.DeletionRecord) error { return window.ErrUnsafeGC })
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, raw.Close()) }()
		observed, err := raw.ReadOwner(ctx)
		if err != nil {
			return err
		}
		control, err := rawRetentionOwner(observed, owner, proof, o.Permit)
		if err != nil {
			return err
		}
		e := ingress.RawDeletion{Version: 1, OwnerDigest: owner.BindingDigest, SourceDigest: o.Permit.Request.SourceDigest, ACKDigest: ack, VolumeID: o.Window.Volume.ID, Reservation: o.Window.Reservation, NativeEpoch: proof.Head.ImportEpoch, NativeReadEpoch: proof.Head.ReadEpoch, RawEpoch: control.RawEpoch}
		removed, err = raw.Collect(ctx, rawRetentionGuard{store: s, permit: o.Permit, evidence: e})
		return err
	})
	return removed, err
}
func RecoverRaw(ctx context.Context, s RawRetentionStore, o RawRetentionOptions) error {
	return withRawRetention(ctx, s, o, func(owner port.ExecutionIngressOwner, proof nativeinput.Proof, ack execwire.Hash) (err error) {
		ticket, err := s.ExecutionRawDeletion(ctx, owner.Binding.RawStoreID)
		if err != nil {
			return err
		}
		e := ticket.Evidence
		if ticket.Permit != o.Permit || e.OwnerDigest != owner.BindingDigest || e.ACKDigest != ack || e.NativeEpoch != proof.Head.ImportEpoch || e.NativeReadEpoch != proof.Head.ReadEpoch || e.VolumeID != o.Window.Volume.ID || e.Reservation != o.Window.Reservation {
			return port.ErrExecutionConflict
		}
		checkOwner := func(observed window.Owner) error {
			control, err := rawRetentionOwner(observed, owner, proof, o.Permit)
			if err != nil {
				return err
			}
			if control.RawEpoch != e.RawEpoch {
				return port.ErrExecutionConflict
			}
			return nil
		}
		if ticket.Completed {
			// Historical completion grants no new unlink. Unknown intents refuse.
			raw, err := window.OpenOwnedChecked(o.Window, ownerDomain, func(window.DeletionRecord) error { return window.ErrUnsafeGC })
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, raw.Close()) }()
			observed, err := raw.ReadOwner(ctx)
			if err != nil {
				return err
			}
			return checkOwner(observed)
		}
		return s.WithExecutionRawDeletion(ctx, o.Permit, e, func(bound context.Context) (err error) {
			check := func(record window.DeletionRecord) error {
				return s.CheckExecutionRawDeletion(bound, o.Permit, e, record)
			}
			raw, err := window.OpenOwnedAuthorized(o.Window, ownerDomain, func(observed window.Owner, record window.DeletionRecord) error {
				if err := checkOwner(observed); err != nil {
					return err
				}
				return check(record)
			})
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, raw.Close()) }()
			observed, err := raw.ReadOwner(bound)
			if err != nil {
				return err
			}
			if err = checkOwner(observed); err != nil {
				return err
			}
			return raw.RecoverDeletion(bound, e.References, func() error { return check(e.Record) }, func() error { return check(e.Record) })
		})
	})
}

type rawRetentionGuard struct {
	store    RawRetentionStore
	permit   retention.Permit
	evidence ingress.RawDeletion
}

func (rawRetentionGuard) WithDeletePermit(context.Context, []window.Reference, func() error) error {
	return window.ErrUnsafeGC
}
func (g rawRetentionGuard) WithDeleteRecord(ctx context.Context, record window.DeletionRecord, refs []window.Reference, remove func() error) error {
	e := g.evidence
	e.Record = record
	e.References = refs
	return g.store.WithExecutionRawDeletion(ctx, g.permit, e, func(bound context.Context) error {
		if err := g.store.CheckExecutionRawDeletion(bound, g.permit, e, record); err != nil {
			return err
		}
		return remove()
	})
}

func rawRetentionOwner(observed window.Owner, owner port.ExecutionIngressOwner, proof nativeinput.Proof, p retention.Permit) (control rawControl, err error) {
	if owner.Binding == nil || execwire.Hash(observed.StoreID) != owner.Binding.RawStoreID || observed.Domain != ownerDomain || strictJSON(observed.State, &control) != nil {
		return control, ErrNative
	}
	i, h := proof.Identity, proof.Head
	if control.Version != 2 || control.Instance != i.Instance || control.Chain.ID != p.Request.Chain.ID || execwire.Hash(control.Chain.GenesisHash) != p.Request.Chain.Genesis || control.Anchor != i.Anchor || !positive(control.RawEpoch) || control.NativeEpoch != h.ImportEpoch || control.Generation != p.Visible.Generation || control.Received.Hash == (execwire.Hash{}) || control.Received.Root == (execwire.Hash{}) || control.Received.Height < h.Tip.Height || control.Received.Height == h.Tip.Height && control.Received != h.Tip {
		return control, ErrNative
	}
	return control, nil
}

// Run EX is acquired before SQL and excludes both DB children. Source ownership
// stays held through the exact E3 callback; no RPC, SDK child or new pool is used.
func withRawRetention(ctx context.Context, s RawRetentionStore, o RawRetentionOptions, apply func(port.ExecutionIngressOwner, nativeinput.Proof, execwire.Hash) error) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || o.Permit.Validate() != nil {
		return retention.ErrUnsafe
	}
	if _, err := o.Window.Normalize(); err != nil {
		return err
	}
	var paths []string
	for _, path := range []string{o.NativeDataDir, o.Window.Path, o.Source.Path} {
		if !filepath.IsAbs(path) {
			return ErrNative
		}
		resolved, err := resolvePlanned(path)
		if err != nil {
			return err
		}
		for _, prior := range paths {
			if ingress.Overlap(prior, resolved) {
				return ErrNative
			}
		}
		paths = append(paths, resolved)
	}
	for _, path := range []string{o.NativeDataDir, filepath.Dir(o.NativeDataDir), o.Source.Path, filepath.Dir(o.Source.Path)} {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !privateDirectory(info) {
			return ErrNative
		}
	}
	lease, err := nativestore.AcquireExclusiveRun(o.NativeDataDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	for _, name := range []string{".arc-native-import.pending", ".arc-native-bootstrap.pending"} {
		if _, err := os.Lstat(filepath.Join(o.NativeDataDir, name)); !errors.Is(err, os.ErrNotExist) {
			return ErrNative
		}
	}
	owner, err := s.ExecutionIngressOwner(ctx)
	if err != nil {
		return err
	}
	var identity nativeIdentity
	if owner.Binding == nil || strictJSON(owner.Binding.NativeIdentity, &identity) != nil || nativeinput.ValidIdentity(identity) != nil || owner.Policy.Module != ModuleName || owner.Policy.Chain != o.Permit.Request.Chain || owner.Policy.Anchor != o.Permit.Request.Anchor || owner.Policy.AnchorRoot != identity.Anchor.Root || owner.Binding.NativeInstance != identity.Instance || owner.Binding.BootstrapDigest != identity.BootstrapDigest {
		return port.ErrExecutionConflict
	}
	policyDigest, err := owner.Policy.Digest()
	if err != nil || policyDigest != owner.PolicyDigest || owner.Binding.PolicyDigest != policyDigest || identity.Anchor.Height != owner.Policy.Anchor.Height || identity.Anchor.Hash != owner.Policy.Anchor.Hash {
		return port.ErrExecutionConflict
	}
	digest, err := owner.Binding.Digest()
	if err != nil || digest != owner.BindingDigest {
		return port.ErrExecutionConflict
	}
	proof, err := nativeinput.ReadHeadProof(o.NativeDataDir, identity)
	if err != nil {
		return err
	}
	if proof.Head.Tip.Height != o.Permit.Request.Head.Height || proof.Head.Tip.Hash != o.Permit.Request.Head.Hash || proof.Head.Tip.Root != o.Permit.Request.HeadRoot {
		return port.ErrExecutionConflict
	}
	admission, err := collector.ManagedAdmissionBinding(collector.Options{Managed: &identity, Chain: owner.Policy.Chain, StateProofs: o.StateProofs})
	if err != nil || admission != owner.Binding.SourcePolicyDigest {
		return port.ErrExecutionConflict
	}
	if o.Source.Admission != nil && *o.Source.Admission != admission {
		return port.ErrExecutionConflict
	}
	o.Source.Admission = &admission
	if o.Source.Chain != owner.Policy.Chain || o.Source.Anchor != owner.Policy.Anchor {
		return port.ErrExecutionConflict
	}
	return spool.WithRetentionEvidence(ctx, o.Source, o.Permit.Request.Head, o.Permit.Request.HeadRoot, o.Permit.Request.KeepBlocks, func(request retention.Request, ack execwire.Hash) error {
		if request != o.Permit.Request {
			return port.ErrExecutionConflict
		}
		return apply(owner, proof, ack)
	})
}
