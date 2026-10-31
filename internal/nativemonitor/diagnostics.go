package nativemonitor

import (
	"math"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
)

func (r *Report) diagnostics(p *Policy, s *Snapshot, at *time.Time) {
	if s.Diagnostics == nil {
		r.add("diagnostics", hold, "not_observed")
		return
	}
	r.fresh("diagnostics.time", s.Diagnostics.ObservedAt, at, p)
	d := s.Diagnostics.Report
	if d == nil {
		r.add("diagnostics", hold, "not_observed")
		return
	}
	if d.Version != 1 || d.Profile == "" || len(d.Profile) > 64 || d.Scope != "explicit_metadata_and_exported_artifacts_only" || d.Authority != "not_authorized" || d.LiveFreshness != "not_performed" || d.FullState != "not_performed" || d.Consistency != "separate_samples_not_atomic_not_a_lease" || d.Findings == nil || d.Observations == nil || len(d.Findings) > 128 || len(d.Observations) > 12 {
		r.add("diagnostics", fail, "invalid_report_contract")
		return
	}
	if s.Identity != nil && s.Identity.Profile != d.Profile {
		r.add("diagnostics.identity", fail, "profile_mismatch")
	}
	aggregate := pass
	for _, f := range d.Findings {
		if len(f.Field) > 128 || len(f.Code) > 128 || f.Field == "" || f.Code == "" {
			r.add("diagnostics", fail, "invalid_finding")
		}
		switch f.Status {
		case fail:
			aggregate = fail
		case hold:
			if aggregate != fail {
				aggregate = hold
			}
		case pass:
		default:
			r.add("diagnostics", fail, "invalid_finding")
		}
	}
	if d.Status != aggregate {
		r.add("diagnostics", fail, "status_findings_conflict")
	}
	switch d.Status {
	case fail:
		r.add("diagnostics", fail, "reported_failure")
	case hold:
		r.add("diagnostics", hold, "reported_incomplete")
	case pass:
		r.add("diagnostics", pass, "reported_metadata_compatible")
	default:
		r.add("diagnostics", fail, "invalid_status")
	}
	for _, o := range d.Observations {
		if !validRef(o.Chain, o.Anchor) || o.Anchor.Generation != 1 {
			r.add("diagnostics.identity", fail, "invalid_identity")
		}
		if s.Identity != nil && (o.Chain != s.Identity.Chain || o.Anchor != s.Identity.Anchor) {
			r.add("diagnostics.identity", fail, "identity_mismatch")
		}
		if !validBasis(o.Kind, o.Basis) || len(o.Phase) > 32 {
			r.add("diagnostics.observation", fail, "invalid_kind_or_basis")
		}
		for _, ref := range []*execwire.Ref{o.ACK, o.Base, o.Visible, o.Parent} {
			if ref != nil && !validRef(o.Chain, *ref) {
				r.add("diagnostics.reference", fail, "invalid_reference")
			}
		}
		for _, ref := range []*InputRef{o.Received, o.NativeTip} {
			if ref != nil && (ref.Height > math.MaxInt64 || ref.Hash == (execwire.Hash{}) || ref.Root == (execwire.Hash{}) || ref.Height < o.Anchor.Height || ref.Height == 0 && ref.Hash != o.Chain.Genesis) {
				r.add("diagnostics.reference", fail, "invalid_input_reference")
			}
		}
		for _, epoch := range []*uint64{o.RawEpoch, o.NativeEpoch, o.NativeReadEpoch} {
			if epoch != nil && (*epoch == 0 || *epoch > math.MaxInt64) {
				r.add("diagnostics.observation", fail, "invalid_epoch_metadata")
			}
		}
		if o.Root != nil && *o.Root == (execwire.Hash{}) || o.Generation != nil && (*o.Generation == 0 || *o.Generation > math.MaxInt64) || o.Floor != nil && *o.Floor > math.MaxInt64 {
			r.add("diagnostics.observation", fail, "invalid_numeric_metadata")
		}
		if o.Kind == "native.fence.permit" || o.MaintenancePending != nil && *o.MaintenancePending {
			r.add("diagnostics.pending", warn, "maintenance_pending")
		}
		if o.TransitionPending != nil && *o.TransitionPending {
			r.add("diagnostics.pending", warn, "transition_pending")
		}
		if o.StateAvailable != nil && !*o.StateAvailable || o.HeaderAvailable != nil && !*o.HeaderAvailable {
			r.add("diagnostics.availability", hold, "not_available")
		}
		if s.Metrics == nil || s.Metrics.Pipeline == nil || s.Metrics.Pipeline.Generation == nil {
			continue
		}
		generation := *s.Metrics.Pipeline.Generation
		// Archived permits and native header parents carry different historical
		// generation semantics. Only current source/public observations compare.
		switch o.Kind {
		case "source.store", "source.artifact":
			if o.ACK != nil && o.ACK.Generation != generation {
				r.add("diagnostics.generation", hold, "generation_rebind_unverified")
			}
		case "checkpoint.public", "retention.public":
			if o.Generation != nil && *o.Generation != generation || o.Visible != nil && o.Visible.Generation != generation {
				r.add("diagnostics.generation", hold, "generation_rebind_unverified")
			}
		}
	}
}

func validBasis(kind, basis string) bool {
	switch kind {
	case "source.store":
		return basis == "separate_metadata_samples"
	case "source.artifact", "checkpoint.public", "retention.public":
		return basis == "exported_artifact_unauthenticated_not_live"
	case "managed.sql":
		return basis == "exported_artifact_unauthenticated_not_live"
	case "managed.raw", "managed.native", "managed.maintenance":
		return basis == "fixed_managed_metadata_not_sdk_state_or_lease"
	case "source.maintenance.permit", "native.fence.permit", "native.floor.permit", "indexer.retention.permit":
		return basis == "recorded_permit_metadata_not_current_native_head"
	}
	return false
}
