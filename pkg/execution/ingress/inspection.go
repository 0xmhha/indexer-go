package ingress

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/configfile"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

const InspectionScope = "read_only_repeatable_sql_snapshot_current_owner_not_live_filesystem"

// Inspector is optional; callers borrow an existing prepared Store, not a new pool.
type Inspector interface {
	InspectExecutionIngress(context.Context) (Inspection, error)
}

// Inspection is a bounded SQL metadata export, not authentication or GC authority.
// Explicit nulls mean absent in this SQL snapshot, never absent on disk.
type Inspection struct {
	Version    uint8
	Scope      string
	Control    port.ExecutionControl
	Owner      *port.ExecutionIngressOwner
	Retention  *RetentionObservation
	Raw        *RawDeletionObservation
	RawPending bool
}
type RetentionObservation struct {
	Permit  retention.Permit
	Pending bool
	Receipt *retention.Receipt
}
type RawDeletionObservation struct {
	StoreID, OwnerDigest, PermitDigest, EvidenceDigest execwire.Hash
	Segment, RawEpoch, NativeEpoch, NativeReadEpoch    uint64
	Completed                                          bool
}

func (s Inspection) Validate() error {
	c := s.Control
	valid := func(ref execwire.Ref) bool {
		return execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: c.Chain, Ref: ref}}) == nil
	}
	if s.Version != 1 || s.Scope != InspectionScope || !valid(c.Anchor) || !valid(c.Visible) || c.Anchor.Generation != 1 || c.Visible.Generation != c.Generation || c.Visible.Height < c.Anchor.Height || c.Root == (execwire.Hash{}) || c.AnchorRoot == (execwire.Hash{}) || c.PartitionBlocks == 0 || c.PartitionBlocks > 1_000_000_000 {
		return port.ErrInvalidData
	}
	if c.Visible.Height == c.Anchor.Height && (c.Visible.Hash != c.Anchor.Hash || c.Root != c.AnchorRoot) {
		return port.ErrExecutionConflict
	}
	if s.Owner != nil {
		o := s.Owner
		d, err := o.Policy.Digest()
		if err != nil || d != o.PolicyDigest || o.Policy.Chain != c.Chain || o.Policy.Anchor != c.Anchor || o.Policy.AnchorRoot != c.AnchorRoot {
			return port.ErrExecutionConflict
		}
		if o.Binding != nil {
			d, err = o.Binding.Digest()
			if err != nil || d != o.BindingDigest || o.Binding.PolicyDigest != o.PolicyDigest {
				return port.ErrExecutionConflict
			}
		} else if o.BindingDigest != (execwire.Hash{}) {
			return port.ErrExecutionConflict
		}
	}
	if s.Retention == nil {
		if c.Maintenance || s.RawPending {
			return port.ErrExecutionConflict
		}
	} else {
		r := s.Retention
		p := r.Permit
		if p.Validate() != nil || p.Request.Chain != c.Chain || p.Request.Anchor != c.Anchor || r.Pending != c.Maintenance || r.Pending && r.Receipt != nil || !r.Pending && (r.Receipt == nil || r.Receipt.Match(p) != nil) {
			return port.ErrExecutionConflict
		}
		if r.Pending && (p.Visible != c.Visible || p.Root != c.Root) {
			return port.ErrExecutionConflict
		}
	}
	if s.Raw != nil {
		r := s.Raw
		if s.Owner == nil || s.Owner.Binding == nil || r.StoreID != s.Owner.Binding.RawStoreID || r.OwnerDigest != s.Owner.BindingDigest || r.PermitDigest == (execwire.Hash{}) || r.EvidenceDigest == (execwire.Hash{}) || r.Segment > math.MaxInt64 || r.RawEpoch == 0 || r.RawEpoch > math.MaxInt64 || r.NativeEpoch == 0 || r.NativeEpoch > math.MaxInt64 || r.NativeReadEpoch == 0 || r.NativeReadEpoch > math.MaxInt64 || !r.Completed != s.RawPending {
			return port.ErrExecutionConflict
		}
		if !r.Completed {
			if s.Retention == nil || !s.Retention.Pending {
				return port.ErrExecutionConflict
			}
			d, err := retention.Digest(s.Retention.Permit)
			if err != nil || d != r.PermitDigest {
				return port.ErrExecutionConflict
			}
		}
	} else if s.RawPending {
		return port.ErrExecutionConflict
	}
	return nil
}

// DecodeInspection requires complete canonical output. Dropping an explicit
// null/false field cannot fabricate a negative observation.
func DecodeInspection(data []byte) (s Inspection, err error) {
	if len(data) > 64<<10 || configfile.JSON(data, &s) != nil {
		return s, port.ErrInvalidData
	}
	canonical, err := json.Marshal(s)
	if err != nil || !bytes.Equal(bytes.TrimSpace(data), canonical) {
		return s, port.ErrInvalidData
	}
	return s, s.Validate()
}
