// Package nativediagnostics composes read-only production metadata observations.
// No runtime configuration loader, DB opener or maintenance coordinator is called.
package nativediagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"reflect"

	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/execwire/configfile"
	"github.com/0xmhha/indexer-go/execwire/metadata"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/internal/executionops"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/checkpoint"
)

const (
	Pass = "PASS"
	Hold = "HOLD"
	Fail = "FAIL"
)

// Product supplies the isolated profile's metadata reader and supported chain boundary.
type Product struct {
	Name          string
	InspectNative func(context.Context, string) (retention.NativeMetadata, error)
	ChainAllowed  func(execwire.Chain) bool
}

type Finding struct {
	Field  string `json:"field"`
	Status string `json:"status"`
	Code   string `json:"code"`
}

// Observation contains only validated chain/ref/hash/numeric metadata. Permits
// describe their recorded public boundary, never the native DB's current head.
type Observation struct {
	Kind               string         `json:"kind"`
	Basis              string         `json:"basis"`
	Chain              execwire.Chain `json:"chain"`
	Anchor             execwire.Ref   `json:"original_anchor"`
	ACK                *execwire.Ref  `json:"ack,omitempty"`
	Base               *execwire.Ref  `json:"retained_base,omitempty"`
	Visible            *execwire.Ref  `json:"public_ref,omitempty"`
	Parent             *execwire.Ref  `json:"parent_header,omitempty"`
	Root               *execwire.Hash `json:"root,omitempty"`
	Generation         *uint64        `json:"generation,omitempty"`
	Floor              *uint64        `json:"retained_through,omitempty"`
	Phase              string         `json:"phase,omitempty"`
	TransitionPending  *bool          `json:"transition_pending,omitempty"`
	MaintenancePending *bool          `json:"maintenance_pending,omitempty"`
	HeaderAvailable    *bool          `json:"header_available,omitempty"`
	StateAvailable     *bool          `json:"state_available,omitempty"`
}
type Report struct {
	chainAllowed  func(execwire.Chain) bool
	Version       int           `json:"version"`
	Profile       string        `json:"profile"`
	Status        string        `json:"status"`
	Scope         string        `json:"scope"`
	Authority     string        `json:"start_ack_deletion_gc_recovery_restore_activation"`
	LiveFreshness string        `json:"live_canonical_freshness"`
	FullState     string        `json:"full_state_reconstruction"`
	Consistency   string        `json:"cross_store_consistency"`
	Findings      []Finding     `json:"findings"`
	Observations  []Observation `json:"observations"`
}

func (r *Report) observe(o Observation) {
	if !validRef(o.Chain, o.Anchor) || o.Anchor.Generation != 1 || !r.chainAllowed(o.Chain) {
		return
	}
	for _, ref := range []**execwire.Ref{&o.ACK, &o.Base, &o.Visible, &o.Parent} {
		if *ref != nil && !validRef(o.Chain, **ref) {
			*ref = nil
		}
	}
	if o.Root != nil && *o.Root == (execwire.Hash{}) {
		o.Root = nil
	}
	if len(r.Observations) >= 12 {
		r.add("report", Fail, "observation_limit_exceeded")
		return
	}
	r.Observations = append(r.Observations, o)
}
func (r *Report) observeControl(kind string, c port.ExecutionControl) {
	r.observe(Observation{Kind: kind, Basis: "exported_artifact_unauthenticated_not_live", Chain: c.Chain, Anchor: c.Anchor, Visible: &c.Visible, Root: &c.Root, Generation: &c.Generation, MaintenancePending: &c.Maintenance})
}
func (r *Report) observePermit(kind, phase string, p retention.Permit) {
	r.observe(Observation{Kind: kind, Basis: "recorded_permit_metadata_not_current_native_head", Chain: p.Request.Chain, Anchor: p.Request.Anchor, ACK: &p.Request.ACK, Visible: &p.Visible, Root: &p.Root, Floor: &p.Request.Through, Phase: phase})
}
func (r *Report) add(field, status, code string) {
	if len(r.Findings) >= 128 {
		r.Findings[127] = Finding{"report", Fail, "finding_limit_exceeded"}
		r.Status = Fail
		return
	}
	r.Findings = append(r.Findings, Finding{field, status, code})
	if status == Fail || status == Hold && r.Status == Pass {
		r.Status = status
	}
}
func (r *Report) match(field string, a, b any) {
	if reflect.DeepEqual(a, b) {
		r.add(field, Pass, "compatible_observation")
	} else {
		r.add(field, Fail, "conflicting_observations")
	}
}
func (r *Report) inputError(field string, e error, artifact bool) {
	switch {
	case errors.Is(e, context.Canceled), errors.Is(e, context.DeadlineExceeded):
		r.add(field, Fail, "cancelled_or_deadline")
	case errors.Is(e, metadata.ErrUnsafe):
		r.add(field, Fail, "unsafe_metadata_input")
	case errors.Is(e, configfile.ErrInput):
		r.add(field, Fail, "invalid_explicit_metadata")
	case errors.Is(e, spool.ErrCorrupt):
		r.add(field, Fail, "corrupt_source_metadata")
	case errors.Is(e, spool.ErrConflict):
		r.add(field, Fail, "conflicting_source_metadata")
	case errors.Is(e, metadata.ErrChanged), errors.Is(e, spool.ErrRecovery):
		r.add(field, Hold, "changed_during_read")
	case errors.Is(e, os.ErrNotExist):
		if artifact {
			r.add(field, Fail, "artifact_missing")
		} else {
			r.add(field, Hold, "store_missing_or_uninitialized")
		}
	default:
		r.add(field, Hold, "io_uninspected")
	}
}
func validRef(chain execwire.Chain, ref execwire.Ref) bool {
	return ref.Height <= math.MaxInt64 && execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: ref}}) == nil
}
func (r *Report) identity(field string, c execwire.Chain, a execwire.Ref) {
	if !validRef(c, a) || a.Generation != 1 {
		r.add(field, Fail, "invalid_original_identity")
	}
	if r.chainAllowed != nil && !r.chainAllowed(c) {
		r.add(field, Fail, "unsupported_chain")
	}
}
func (r *Report) control(field string, c port.ExecutionControl) {
	r.identity(field, c.Chain, c.Anchor)
	if !validRef(c.Chain, c.Visible) || c.Generation == 0 || c.Visible.Generation != c.Generation || c.Generation < c.Anchor.Generation || c.Visible.Height < c.Anchor.Height || c.Root == (execwire.Hash{}) || c.AnchorRoot == (execwire.Hash{}) || c.Visible.Height == c.Anchor.Height && (c.Visible.Hash != c.Anchor.Hash || c.Root != c.AnchorRoot) || c.PartitionBlocks == 0 || c.PartitionBlocks > 1_000_000_000 {
		r.add(field, Fail, "invalid_checkpoint_control")
	}
	if c.Maintenance || c.Rewinding || !c.BaselineReady {
		r.add(field, Hold, "checkpoint_pending_or_incomplete")
	}
}

type inputs struct{ spool, native, source, checkpoint, retention string }

func parse(args []string) (in inputs, ok bool) {
	if len(args) > 10 {
		return in, false
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) || seen[args[i]] || len(args[i+1]) > 4096 {
			return in, false
		}
		seen[args[i]] = true
		value := args[i+1]
		if value == "HOLD" {
			value = ""
		}
		switch args[i] {
		case "--spool":
			in.spool = value
		case "--native":
			in.native = value
		case "--source-artifact":
			in.source = value
		case "--checkpoint-artifact":
			in.checkpoint = value
		case "--retention-artifact":
			in.retention = value
		default:
			return in, false
		}
	}
	return in, true
}

// Check observes only fixed source/native metadata and explicit supported exports.
// Unknown observations never suppress independently known corruption/conflicts.
func Check(ctx context.Context, args []string, product Product) Report {
	profile := product.Name
	r := Report{Version: 1, Profile: profile, Status: Pass, Scope: "explicit_metadata_and_exported_artifacts_only", Authority: "not_authorized", LiveFreshness: "not_performed", FullState: "not_performed", Consistency: "separate_samples_not_atomic_not_a_lease", Findings: []Finding{}, Observations: []Observation{}}
	in, ok := parse(args)
	if !ok {
		r.add("cli", Fail, "invalid_arguments")
		return r
	}
	if product.InspectNative == nil || product.ChainAllowed == nil || profile == "" {
		r.Profile = "unsupported"
		r.add("profile", Fail, "unsupported_profile")
		return r
	}
	r.chainAllowed = product.ChainAllowed
	r.add("native.head_state_indexes", Hold, "actual_native_db_uninspected")
	r.add("indexer.live", Hold, "operating_db_uninspected")
	reader := metadata.New(ctx)
	artifact := func(field, path string, target any) bool {
		if path == "" {
			r.add(field, Hold, "not_selected")
			return false
		}
		b, e := reader.File(path)
		if e != nil {
			r.inputError(field, e, true)
			return false
		}
		if configfile.JSON(b, target) != nil || !completeArtifact(b, reflect.TypeOf(target)) {
			r.add(field, Fail, "invalid_artifact_schema")
			return false
		}
		r.add(field, Pass, "artifact_observation_unauthenticated_not_live")
		return true
	}
	var source *spool.MetadataObservation
	if in.spool == "" {
		r.add("source.store", Hold, "not_selected")
	} else {
		v, e := spool.InspectMetadata(ctx, in.spool)
		// Retain any independently validated manifest even when later sampling changes.
		if v.Chain.ID != "" {
			source = &v
			r.identity("source.identity", v.Chain, v.Anchor)
			o := Observation{Kind: "source.store", Basis: "separate_metadata_samples", Chain: v.Chain, Anchor: v.Anchor, ACK: &v.Committed, Base: &v.Base}
			if e == nil {
				o.TransitionPending = &v.TransitionPending
				o.MaintenancePending = &v.MaintenancePending
			}
			r.observe(o)
		}
		if e != nil {
			r.inputError("source.store", e, false)
		} else {
			r.add("source.store", Pass, "metadata_observed_payloads_uninspected")
		}
	}
	var native *retention.NativeMetadata
	if in.native == "" {
		r.add("native.maintenance", Hold, "not_selected")
	} else {
		v, e := product.InspectNative(ctx, in.native)
		native = &v
		if e != nil {
			r.inputError("native.maintenance", e, false)
		} else {
			r.add("native.directory", Hold, "directory_only_db_uninitialized_or_uninspected")
			if v.Fence == nil {
				r.add("native.fence", Pass, "optional_marker_absent")
			}
			if v.Floor == nil {
				r.add("native.floor", Hold, "retained_floor_uninspected")
			}
		}
	}
	var src format.SourceObservation
	srcOK := artifact("source.artifact", in.source, &src)
	if srcOK {
		r.identity("source.artifact.identity", src.Chain, src.Anchor)
		parent := (*execwire.Ref)(nil)
		root := (*execwire.Hash)(nil)
		if src.HeaderAvailable {
			parent = &src.Parent
			root = &src.ParentRoot
		}
		r.observe(Observation{Kind: "source.artifact", Basis: "exported_artifact_unauthenticated_not_live", Chain: src.Chain, Anchor: src.Anchor, ACK: &src.Committed, Parent: parent, Root: root, TransitionPending: &src.TransitionPending, MaintenancePending: &src.MaintenancePending, HeaderAvailable: &src.HeaderAvailable, StateAvailable: &src.StateAvailable})
		if !validRef(src.Chain, src.Committed) || src.Committed.Generation < src.Anchor.Generation || src.Committed.Height < src.Anchor.Height {
			r.add("source.artifact", Fail, "invalid_ack")
		}
		if src.HeaderAvailable && (!validRef(src.Chain, src.Parent) || src.ParentRoot == (execwire.Hash{})) {
			r.add("source.artifact", Fail, "invalid_parent")
		}
		if !src.HeaderAvailable || !src.StateAvailable || src.TransitionPending || src.MaintenancePending {
			r.add("source.artifact", Hold, "pending_or_incomplete")
		}
	}
	var cp executionops.CheckpointInspection
	cpOK := artifact("checkpoint.artifact", in.checkpoint, &cp)
	if cpOK {
		r.control("checkpoint.control", cp.Control)
		r.observeControl("checkpoint.public", cp.Control)
		b := cp.Bootstrap
		if b.Coverage != "full" && b.Coverage != "partial" || b.Mode != "prepared" && b.Mode != "genesis" && b.Mode != "checkpoint" || len(b.Digest) > 0 && (len(b.Digest) != 32 || bytes.Equal(b.Digest, make([]byte, 32))) || b.Mode == "genesis" && cp.Control.Anchor.Height != 0 {
			r.add("checkpoint.bootstrap", Fail, "invalid_full_baseline_metadata")
		}
		if b.Coverage == "partial" || b.Mode == "prepared" {
			r.add("checkpoint.bootstrap", Hold, "partial_baseline_metadata")
		}
		if b.Coverage == "full" && b.Mode == "prepared" {
			r.add("checkpoint.bootstrap", Fail, "conflicting_baseline_mode")
		}
		if b.Coverage == "full" && len(b.Digest) == 0 {
			r.add("checkpoint.bootstrap", Hold, "bootstrap_digest_uninspected")
		}

	}
	var rt executionops.RetentionInspection
	rtOK := artifact("retention.artifact", in.retention, &rt)
	if rtOK {
		r.control("retention.control", rt.Control)
		r.observeControl("retention.public", rt.Control)
		if rt.Pending != rt.Control.Maintenance {
			r.add("retention.pending", Fail, "conflicting_phase")
		}
		if rt.Pending {
			r.add("retention.pending", Hold, "maintenance_pending")
		}
		if rt.Permit.Version != 0 {
			if rt.Permit.Validate() != nil {
				r.add("retention.permit", Fail, "invalid_permit")
			} else {
				r.match("retention.permit.chain", rt.Control.Chain, rt.Permit.Request.Chain)
				r.match("retention.permit.anchor", rt.Control.Anchor, rt.Permit.Request.Anchor)
				r.match("retention.permit.visible", rt.Control.Visible, rt.Permit.Visible)
				r.match("retention.permit.root", rt.Control.Root, rt.Permit.Root)
			}
		} else if rt.Pending || rt.Receipt != nil || rt.Permit != (retention.Permit{}) {
			r.add("retention.permit", Fail, "missing_or_partial_permit")
		}
		if rt.Receipt != nil && rt.Receipt.Match(rt.Permit) != nil {
			r.add("retention.receipt", Fail, "invalid_receipt")
		}
	}
	if cpOK && rtOK {
		r.match("indexer.artifacts.control", cp.Control, rt.Control)
	}
	controls := []port.ExecutionControl{}
	if cpOK {
		controls = append(controls, cp.Control)
	}
	if rtOK {
		controls = append(controls, rt.Control)
	}
	if source != nil {
		if source.TransitionPending || source.MaintenancePending {
			r.add("source.store", Hold, "transition_or_maintenance_pending")
		}
		if srcOK {
			r.match("source.artifact.chain", source.Chain, src.Chain)
			r.match("source.artifact.anchor", source.Anchor, src.Anchor)
			r.match("source.artifact.ack", source.Committed, src.Committed)
		}
		for _, c := range controls {
			compareSource(&r, c, source.Chain, source.Anchor, source.Committed)
			if source.Base.Generation > c.Generation || source.Base.Height > c.Visible.Height {
				r.add("source.floor", Fail, "floor_ahead_of_public_checkpoint")
			}
		}
	}
	for _, c := range controls {
		if srcOK {
			compareSource(&r, c, src.Chain, src.Anchor, src.Committed)
			// Compose the production pure recovery diagnosis; its advice grants no authority.
			d := checkpoint.Diagnose(c, src)
			if d.Status == "compatible_observation" {
				r.add("checkpoint.source", Pass, "compatible_observation")
			} else {
				r.add("checkpoint.source", Hold, "production_diagnosis_incomplete")
			}
			if src.HeaderAvailable {
				r.match("checkpoint.parent.height", c.Visible.Height, src.Parent.Height)
				r.match("checkpoint.parent.hash", c.Visible.Hash, src.Parent.Hash)
				r.match("checkpoint.root", c.Root, src.ParentRoot)
			}
		}
	}
	permits := []retention.Permit{}
	historical := []retention.Permit{}
	if source != nil && source.Completion != nil {
		historical = append(historical, *source.Completion)
	}
	if source != nil && source.Maintenance != nil {
		r.observePermit("source.maintenance.permit", "pending", *source.Maintenance)
		permits = append(permits, *source.Maintenance)
	}
	if native != nil {
		for _, f := range []*retention.NativeFence{native.Fence, native.DetachedPending} {
			if f != nil {
				r.observePermit("native.fence.permit", f.Phase, f.Permit)
				r.add("native.fence", Hold, "maintenance_fence_present")
				permits = append(permits, f.Permit)
			}
		}
		if native.Floor != nil {
			p := native.Floor.Permit
			r.observePermit("native.floor.permit", "retained", p)
			historical = append(historical, p)
			for _, c := range controls {
				if p.Request.Through >= c.Visible.Height || p.Request.Through < c.Anchor.Height {
					r.add("native.floor", Fail, "floor_outside_public_boundary")
				}
			}
		}
	}
	if rtOK && rt.Permit.Validate() == nil {
		r.observePermit("indexer.retention.permit", "recorded", rt.Permit)
		if rt.Pending {
			permits = append(permits, rt.Permit)
		} else {
			historical = append(historical, rt.Permit)
		}
	}
	if native != nil && native.Floor != nil {
		for _, f := range []*retention.NativeFence{native.Fence, native.DetachedPending} {
			if f != nil && (f.Phase == "cleanup" || f.Phase == "done") {
				r.match("native.phase.floor", f.Permit, native.Floor.Permit)
			}
		}
	}
	for _, p := range historical {
		r.identity("retained.identity", p.Request.Chain, p.Request.Anchor)
		if source != nil {
			r.match("retained.source.chain", p.Request.Chain, source.Chain)
			r.match("retained.source.anchor", p.Request.Anchor, source.Anchor)
			if p.Request.ACK.Generation == source.Committed.Generation && p.Request.ACK.Height == source.Committed.Height {
				r.match("retained.source.ack_hash", p.Request.ACK.Hash, source.Committed.Hash)
			}
			if p.Request.ACK.Generation > source.Committed.Generation || p.Request.Through >= source.Committed.Height {
				r.add("retained.source.floor", Fail, "floor_ahead_of_ack")
			}
		}
		if srcOK {
			r.match("retained.artifact.chain", p.Request.Chain, src.Chain)
			r.match("retained.artifact.anchor", p.Request.Anchor, src.Anchor)
			if p.Request.ACK.Generation > src.Committed.Generation || p.Request.Through >= src.Committed.Height {
				r.add("retained.artifact.floor", Fail, "floor_ahead_of_ack")
			}
			if src.HeaderAvailable && p.Visible.Height == src.Parent.Height && p.Visible.Hash == src.Parent.Hash {
				r.match("retained.artifact.root", p.Root, src.ParentRoot)
			}
		}
		for _, c := range controls {
			r.match("retained.checkpoint.chain", p.Request.Chain, c.Chain)
			r.match("retained.checkpoint.anchor", p.Request.Anchor, c.Anchor)
			if p.Visible.Generation > c.Generation || p.Request.Through >= c.Visible.Height {
				r.add("retained.checkpoint.floor", Fail, "floor_ahead_of_public_checkpoint")
			}
			if p.Visible.Generation == c.Generation && p.Visible.Height == c.Visible.Height {
				r.match("retained.checkpoint.hash", p.Visible.Hash, c.Visible.Hash)
				r.match("retained.checkpoint.root", p.Root, c.Root)
			}
		}
	}
	for i, p := range permits {
		r.identity("maintenance.identity", p.Request.Chain, p.Request.Anchor)
		if i > 0 {
			r.match("maintenance.permits", permits[0], p)
		}
		if source != nil {
			r.match("maintenance.source.chain", p.Request.Chain, source.Chain)
			r.match("maintenance.source.anchor", p.Request.Anchor, source.Anchor)
			r.match("maintenance.source.ack", p.Request.ACK, source.Committed)
		}
		if srcOK {
			r.match("maintenance.artifact.chain", p.Request.Chain, src.Chain)
			r.match("maintenance.artifact.anchor", p.Request.Anchor, src.Anchor)
			r.match("maintenance.artifact.ack", p.Request.ACK, src.Committed)
			if src.HeaderAvailable && p.Visible.Height == src.Parent.Height && p.Visible.Hash == src.Parent.Hash {
				r.match("maintenance.artifact.root", p.Root, src.ParentRoot)
			}
		}
		for _, c := range controls {
			r.match("maintenance.checkpoint.chain", p.Request.Chain, c.Chain)
			r.match("maintenance.checkpoint.anchor", p.Request.Anchor, c.Anchor)
			if p.Visible.Generation > c.Generation || p.Visible.Height > c.Visible.Height {
				r.add("maintenance.checkpoint", Fail, "permit_ahead_of_public_checkpoint")
			}
			if p.Visible.Generation == c.Generation && p.Visible.Height == c.Visible.Height {
				r.match("maintenance.checkpoint.hash", p.Visible.Hash, c.Visible.Hash)
				r.match("maintenance.checkpoint.root", p.Root, c.Root)
			}
		}
	}
	if e := reader.Verify(); e != nil {
		r.inputError("artifacts", e, true)
	}
	if source != nil {
		v, e := spool.InspectMetadata(ctx, in.spool)
		if e != nil {
			r.inputError("source.resample", e, false)
		} else if !reflect.DeepEqual(*source, v) {
			r.add("source.resample", Hold, "changed_during_read")
		}
	}
	if native != nil && native.StorePresent {
		v, e := product.InspectNative(ctx, in.native)
		if e != nil {
			r.inputError("native.resample", e, false)
		} else if !reflect.DeepEqual(*native, v) {
			r.add("native.resample", Hold, "changed_during_read")
		}
	}
	if e := ctx.Err(); e != nil {
		r.inputError("input", e, false)
	}
	return r
}
func compareSource(r *Report, c port.ExecutionControl, chain execwire.Chain, anchor, ack execwire.Ref) {
	r.match("checkpoint.chain", c.Chain, chain)
	r.match("checkpoint.anchor", c.Anchor, anchor)
	if ack.Generation > c.Generation || ack.Generation >= c.Generation && ack.Height > c.Visible.Height {
		r.add("checkpoint.ack", Fail, "ack_ahead_of_public_checkpoint")
	}
	if ack.Generation == c.Generation && ack.Height == c.Visible.Height && ack.Hash != c.Visible.Hash {
		r.add("checkpoint.ack", Fail, "ack_hash_conflict")
	}
	if ack.Generation < c.Generation {
		r.add("checkpoint.generation", Hold, "generation_rebind_unperformed")
	}
}
func Run(ctx context.Context, args []string, out io.Writer, product Product) int {
	r := Check(ctx, args, product)
	b, e := json.Marshal(r)
	if e != nil || len(b) > 64<<10 {
		return 1
	}
	b = append(b, '\n')
	if n, e := out.Write(b); e != nil || n != len(b) || ctx.Err() != nil {
		return 1
	}
	switch r.Status {
	case Pass:
		return 0
	case Hold:
		return 2
	default:
		return 1
	}
}
