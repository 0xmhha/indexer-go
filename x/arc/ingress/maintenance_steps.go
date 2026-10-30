package arcingress

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativeops"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
)

func (c *maintenanceCoordinator) normalize(ctx context.Context) (maintenanceScope, execwire.Hash, error) {
	var scope maintenanceScope
	var digest execwire.Hash
	if c.cfg.Module != ModuleName || c.deps.Canonical == nil {
		return scope, digest, port.ErrInvalidData
	}
	var ok bool
	c.store, ok = c.deps.Store.(MaintenanceStore)
	if !ok || c.deps.Policy == nil {
		return scope, digest, port.ErrInvalidData
	}
	policy := *c.deps.Policy
	c.deps.Policy = &policy
	c.cfg = c.cfg.Normalize()
	if c.options.KeepBlocks == 0 {
		c.options.KeepBlocks = retention.DefaultKeep
	}
	if c.options.ReserveBytes == 0 {
		c.options.ReserveBytes = 1 << 30
	}
	if c.options.Timeout == 0 {
		c.options.Timeout = time.Hour
	}
	if c.options.KeepBlocks <= 128 || c.options.KeepBlocks > math.MaxInt64 || c.options.Timeout <= 0 || c.options.Timeout > time.Hour {
		return scope, digest, port.ErrInvalidData
	}
	var err error
	for _, p := range []*string{&c.cfg.Native.DataDir, &c.cfg.Collector.SpoolDir, &c.raw.Path, &c.raw.Volume.Root, &c.options.Executable} {
		if !filepath.IsAbs(*p) {
			return scope, digest, port.ErrInvalidData
		}
		*p, err = resolvePlanned(*p)
		if err != nil {
			return scope, digest, err
		}
	}
	paths := []string{c.cfg.Native.DataDir, c.cfg.Collector.SpoolDir, c.raw.Path, c.options.Executable}
	for n, path := range paths {
		for _, previous := range paths[:n] {
			if ingress.Overlap(path, previous) {
				return scope, digest, ErrNative
			}
		}
	}
	if c.raw, err = c.raw.Normalize(); err != nil {
		return scope, digest, err
	}
	i, err := os.Lstat(c.options.Executable)
	if err != nil {
		return scope, digest, err
	}
	if !ownedExecutable(i) || i.Size() > 1<<30 {
		return scope, digest, ErrNative
	}
	f, err := os.Open(c.options.Executable)
	if err != nil {
		return scope, digest, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	buffer := make([]byte, 64<<10)
	for {
		if err = ctx.Err(); err != nil {
			return scope, digest, err
		}
		n, e := f.Read(buffer)
		if n > 0 {
			_, _ = h.Write(buffer[:n])
		}
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return scope, digest, e
		}
	}
	after, err := os.Lstat(c.options.Executable)
	if err != nil {
		return scope, digest, err
	}
	if !os.SameFile(i, after) || i.Size() != after.Size() || !i.ModTime().Equal(after.ModTime()) {
		return scope, digest, ErrNative
	}
	copy(digest[:], h.Sum(nil))
	scope = maintenanceScope{Native: c.cfg.Native.DataDir, Raw: c.raw.Path, Source: c.cfg.Collector.SpoolDir, VolumeRoot: c.raw.Volume.Root, VolumeID: c.raw.Volume.ID, Reservation: c.raw.Reservation}
	return scope, digest, nil
}
func (c *maintenanceCoordinator) owner(ctx context.Context) (port.ExecutionIngressOwner, error) {
	return sourceOwner(ctx, c.store, c.deps.Policy, c.deps.Control.ExecutionConfig, true)
}
func (c *maintenanceCoordinator) source(owner port.ExecutionIngressOwner) spool.Options {
	a := owner.Binding.SourcePolicyDigest
	return spool.Options{Path: c.cfg.Collector.SpoolDir, Chain: owner.Policy.Chain, Anchor: owner.Policy.Anchor, Admission: &a, MaxBytes: c.cfg.Collector.MaxBytes, MaxRecords: c.cfg.Collector.MaxRecords}
}
func (c *maintenanceCoordinator) node(s maintenanceRecord) nativestore.Options {
	return nativestore.Options{DataDir: c.cfg.Native.DataDir, ReserveBytes: c.options.ReserveBytes, Adapter: nativeops.Native{Executable: c.options.Executable, Chain: s.Point.Chain, Timeout: c.options.Timeout, AnchorRoot: s.Proof.Identity.Anchor.Root, Managed: &nativeops.ManagedContext{Version: 1, OriginalDataDir: c.cfg.Native.DataDir, Proof: s.Proof}}}
}
func (c *maintenanceCoordinator) checkOwner(ctx context.Context, s maintenanceRecord) (port.ExecutionIngressOwner, error) {
	o, err := c.owner(ctx)
	if err != nil {
		return o, err
	}
	var i nativeIdentity
	if o.Binding == nil || strictJSON(o.Binding.NativeIdentity, &i) != nil || i != s.Proof.Identity || o.PolicyDigest != s.PolicyDigest || o.BindingDigest != s.OwnerDigest {
		return o, port.ErrExecutionConflict
	}
	a, err := collector.ManagedAdmissionBinding(collector.Options{Chain: o.Policy.Chain, Managed: &i, StateProofs: c.deps.StateProofs})
	if err != nil || a != o.Binding.SourcePolicyDigest {
		return o, port.ErrExecutionConflict
	}
	return o, nil
}
func (c *maintenanceCoordinator) checkPoint(ctx context.Context, s maintenanceRecord) error {
	v, err := c.store.ExecutionStatus(ctx)
	if err != nil {
		return err
	}
	if v.ExecutionConfig != c.deps.Control.ExecutionConfig || !v.BaselineReady || v.Rewinding || v.ReorgPending || v.Visible != s.Point.Ref || v.Generation != s.Point.Ref.Generation || v.Root != s.Point.Root {
		return port.ErrExecutionConflict
	}
	return nil
}
func (c *maintenanceCoordinator) fresh(point ingress.DrainPoint) func(context.Context) error {
	return func(ctx context.Context) error {
		h, err := c.deps.Canonical.HashAt(ctx, point.Ref.Height)
		if err != nil {
			return err
		}
		if execwire.Hash(h) != point.Ref.Hash {
			return port.ErrExecutionConflict
		}
		return ctx.Err()
	}
}
func (c *maintenanceCoordinator) observe(ctx context.Context, point ingress.DrainPoint) (s maintenanceRecord, err error) {
	o, err := c.owner(ctx)
	if err != nil {
		return s, err
	}
	s.Version = 1
	s.Phase = "prepared"
	s.Point = point
	s.PolicyDigest = o.PolicyDigest
	s.OwnerDigest = o.BindingDigest
	if err = c.checkPoint(ctx, s); err != nil {
		return s, err
	}
	v, err := c.store.ExecutionStatus(ctx)
	if err != nil {
		return s, err
	}
	if v.Maintenance {
		return s, port.ErrExecutionMaintenance
	}
	lease, err := nativestore.AcquireExclusiveRun(c.cfg.Native.DataDir)
	if err != nil {
		return s, err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	var identity nativeIdentity
	if strictJSON(o.Binding.NativeIdentity, &identity) != nil {
		return s, ErrNative
	}
	s.Proof, err = nativeinput.ReadHeadProof(c.cfg.Native.DataDir, identity)
	if err != nil {
		return s, err
	}
	if s.Proof.Head.Tip.Height != point.Ref.Height || s.Proof.Head.Tip.Hash != point.Ref.Hash || s.Proof.Head.Tip.Root != point.Root {
		return s, port.ErrExecutionConflict
	}
	if _, err = c.checkOwner(ctx, s); err != nil {
		return s, err
	}
	n := c.node(s).Adapter.(nativeops.Native)
	n, err = n.WithRunLease(lease)
	if err != nil {
		return s, err
	}
	_, err = n.Inspect(ctx, c.cfg.Native.DataDir, retention.Request{Version: 1, Chain: point.Chain, Anchor: o.Policy.Anchor})
	if err != nil {
		return s, err
	}
	err = spool.WithRetentionEvidence(ctx, c.source(o), execwire.Ref{Height: point.Ref.Height, Hash: point.Ref.Hash}, point.Root, c.options.KeepBlocks, func(r retention.Request, d execwire.Hash) error {
		if r.ACK != point.Ref || r.Tail != point.Ref || d != point.Digest {
			return port.ErrExecutionConflict
		}
		s.Request = r
		return nil
	})
	if err != nil {
		return s, err
	}
	previous, pending, _, e := c.store.ExecutionRetentionStatus(ctx)
	if e != nil && !errors.Is(e, port.ErrNotFound) {
		return s, e
	}
	if e == nil {
		if pending {
			return s, port.ErrExecutionMaintenance
		}
		s.PreviousToken = previous.Token
	}
	current, e := c.store.ExecutionStatus(ctx)
	if e != nil {
		return s, e
	}
	if current != v {
		return s, port.ErrExecutionStale
	}
	return s, nil
}
func (c *maintenanceCoordinator) at(phase string) error {
	if c.after != nil {
		return c.after(phase)
	}
	return nil
}
func (c *maintenanceCoordinator) save(ctx context.Context, j *maintenanceJournal, s *maintenanceRecord, phase string) error {
	next := *s
	next.Phase = phase
	if err := j.save(ctx, next); err != nil {
		return err
	}
	*s = next
	return c.at(phase)
}
func (c *maintenanceCoordinator) run(parent context.Context, point *ingress.DrainPoint) (result ingress.MaintenanceResult, err error) {
	scope, digest, err := c.normalize(parent)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(parent, c.options.Timeout)
	defer cancel()
	j, err := openMaintenanceJournal(c.cfg.Native.DataDir)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, j.close()) }()
	var s maintenanceRecord
	if point != nil {
		if j.current != nil && j.current.Phase != "complete" {
			return result, port.ErrExecutionMaintenance
		}
		s, err = c.observe(ctx, *point)
		if err != nil {
			return result, err
		}
		s.Scope = scope
		s.Options = c.options
		s.BinaryDigest = digest
		if j.current != nil && j.current.Request == s.Request && j.current.Point == s.Point && j.current.Options == s.Options && j.current.BinaryDigest == digest && j.current.OwnerDigest == s.OwnerDigest && j.current.Scope == scope {
			s = *j.current
		} else if err = c.save(ctx, j, &s, "prepared"); err != nil {
			return s.result(), err
		}
	} else {
		if j.current == nil {
			return result, port.ErrNotFound
		}
		s = *j.current
	}
	if s.Scope != scope || s.Options != c.options || s.BinaryDigest != digest {
		return s.result(), port.ErrExecutionConflict
	}
	if _, err = c.checkOwner(ctx, s); err != nil {
		return s.result(), err
	}
	for s.Phase != "complete" {
		if err = ctx.Err(); err != nil {
			return s.result(), err
		}
		if _, err = c.checkOwner(ctx, s); err != nil {
			return s.result(), err
		}
		if err = c.checkPoint(ctx, s); err != nil {
			return s.result(), err
		}
		switch s.Phase {
		case "prepared":
			err = c.authorize(ctx, j, &s)
		case "authorized":
			err = c.rawPhase(ctx, j, &s)
		case "raw_complete":
			err = c.cowPhase(ctx, j, &s)
		case "cow_complete":
			err = c.store.CompleteExecutionRetention(ctx, *s.Permit, *s.Receipt, c.fresh(s.Point))
			if err == nil {
				err = c.at("db_effects")
			}
			if err == nil {
				err = c.save(ctx, j, &s, "db_complete")
			}
		case "db_complete":
			err = c.releasePhase(ctx, j, &s)
		default:
			err = port.ErrExecutionConflict
		}
		if err != nil {
			return s.result(), err
		}
	}
	// A completed record is historical evidence, never permission to replay
	// deletion after the native/public head advances.
	p, pending, r, err := c.store.ExecutionRetentionStatus(ctx)
	if err != nil {
		return s.result(), err
	}
	if pending || p != *s.Permit || r == nil || *r != *s.Receipt {
		return s.result(), port.ErrExecutionMaintenance
	}
	return s.result(), nil
}
func (c *maintenanceCoordinator) authorize(ctx context.Context, j *maintenanceJournal, s *maintenanceRecord) error {
	p, pending, _, err := c.store.ExecutionRetentionStatus(ctx)
	if err != nil && !errors.Is(err, port.ErrNotFound) {
		return err
	}
	if err == nil && pending {
		if p.Token == s.PreviousToken || p.Request != s.Request || p.Visible != s.Point.Ref || p.Root != s.Point.Root {
			return port.ErrExecutionConflict
		}
	} else {
		if err == nil && p.Token != s.PreviousToken {
			return port.ErrExecutionConflict
		}
		if errors.Is(err, port.ErrNotFound) && s.PreviousToken != (execwire.Hash{}) {
			return port.ErrExecutionConflict
		}
		p, err = c.store.BeginExecutionRetention(ctx, s.Request, c.fresh(s.Point))
		if err != nil {
			return err
		}
	}
	if err = c.at("authorization_effects"); err != nil {
		return err
	}
	s.Permit = &p
	return c.save(ctx, j, s, "authorized")
}
func (c *maintenanceCoordinator) rawPhase(ctx context.Context, j *maintenanceJournal, s *maintenanceRecord) error {
	o, err := c.checkOwner(ctx, *s)
	if err != nil {
		return err
	}
	raw := RawRetentionOptions{NativeDataDir: c.cfg.Native.DataDir, Window: c.raw, Source: c.source(o), Permit: *s.Permit, StateProofs: c.deps.StateProofs}
	ticket, err := c.store.ExecutionRawDeletion(ctx, o.Binding.RawStoreID)
	if err != nil && !errors.Is(err, port.ErrNotFound) {
		return err
	}
	if err == nil && !ticket.Completed {
		if ticket.Permit != *s.Permit {
			return port.ErrExecutionConflict
		}
		if err = RecoverRaw(ctx, c.store, raw); err != nil {
			return err
		}
	}
	_, err = CollectRaw(ctx, c.store, raw)
	if err != nil {
		return err
	}
	if err = c.at("raw_effects"); err != nil {
		return err
	}
	return c.store.WithExecutionRetention(ctx, *s.Permit, func(bound context.Context) error { return c.save(bound, j, s, "raw_complete") })
}
func (c *maintenanceCoordinator) cowPhase(ctx context.Context, j *maintenanceJournal, s *maintenanceRecord) error {
	return c.store.WithExecutionRetention(ctx, *s.Permit, func(bound context.Context) (err error) {
		o, err := c.checkOwner(bound, *s)
		if err != nil {
			return err
		}
		source, err := spool.OpenMaintenance(bound, c.source(o), *s.Permit)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, source.Close()) }()
		status, err := source.Status(bound)
		if err != nil {
			return err
		}
		if status.Committed != s.Point.Ref || status.Digest != s.Point.Digest {
			return port.ErrExecutionConflict
		}
		r, err := source.CollectRetention(bound, *s.Permit, func(work context.Context) error { return nativestore.Compact(work, c.node(*s), *s.Permit) })
		if err != nil {
			return err
		}
		if err = c.at("cow_effects"); err != nil {
			return err
		}
		s.Receipt = &r
		return c.save(bound, j, s, "cow_complete")
	})
}
func (c *maintenanceCoordinator) releasePhase(ctx context.Context, j *maintenanceJournal, s *maintenanceRecord) error {
	return c.store.WithExecutionRetentionRelease(ctx, *s.Permit, *s.Receipt, func(bound context.Context) (err error) {
		o, err := c.checkOwner(bound, *s)
		if err != nil {
			return err
		}
		source, err := spool.OpenMaintenance(bound, c.source(o), *s.Permit)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, source.Close()) }()
		r, err := source.RetentionCompletion(bound, *s.Permit)
		if err != nil {
			return err
		}
		if r != *s.Receipt {
			return port.ErrExecutionConflict
		}
		pd, _ := retention.Digest(*s.Permit)
		rd, _ := retention.Digest(r)
		release := retention.Release{Version: 1, PermitDigest: pd, ReceiptDigest: rd}
		if err = nativestore.Release(bound, c.node(*s), *s.Permit, r, release); err != nil {
			return err
		}
		if err = source.ReleaseRetention(bound, *s.Permit, release); err != nil {
			return err
		}
		if err = c.at("files_effects"); err != nil {
			return err
		}
		return c.save(bound, j, s, "complete")
	})
}
