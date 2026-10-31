package diagnostics

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strconv"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/metadata"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/internal/nativediagnostics"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/x/arc/ingress"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
)

const managedBasis = "fixed_managed_metadata_not_sdk_state_or_lease"
const sqlBasis = "exported_artifact_unauthenticated_not_live"

func inspectManaged(ctx context.Context, in nativediagnostics.ManagedInputs) (out nativediagnostics.ManagedResult) {
	add := func(field, status, code string) {
		out.Findings = append(out.Findings, nativediagnostics.Finding{Field: field, Status: status, Code: code})
	}
	issue := func(field string, err error, artifact bool) {
		status, code := nativediagnostics.Hold, "io_uninspected"
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			status, code = nativediagnostics.Fail, "cancelled_or_deadline"
		case errors.Is(err, metadata.ErrChanged):
			code = "changed_during_read"
		case errors.Is(err, os.ErrNotExist):
			code = "not_observed"
			if artifact {
				status, code = nativediagnostics.Fail, "artifact_missing"
			}
		case errors.Is(err, metadata.ErrUnsafe), errors.Is(err, arcingress.ErrNative), errors.Is(err, nativeinput.ErrProof), errors.Is(err, port.ErrInvalidData), errors.Is(err, port.ErrExecutionConflict), errors.Is(err, spool.ErrCorrupt), errors.Is(err, spool.ErrConflict):
			status, code = nativediagnostics.Fail, "invalid_or_conflicting_managed_metadata"
		}
		add(field, status, code)
	}
	compare := func(field string, a, b any) {
		if reflect.DeepEqual(a, b) {
			add(field, nativediagnostics.Pass, "matching_metadata_not_authority")
		} else {
			add(field, nativediagnostics.Fail, "immutable_identity_conflict")
		}
	}
	allowed := func(c execwire.Chain) bool { return c.ID == "5042002" && Product().ChainAllowed(c) }
	anchor := func(i nativeinput.Identity) execwire.Ref {
		return execwire.Ref{Generation: 1, Height: i.Anchor.Height, Hash: i.Anchor.Hash}
	}
	ref := func(i nativeinput.Ref) *nativediagnostics.InputRef {
		return &nativediagnostics.InputRef{Height: i.Height, Hash: i.Hash, Root: i.Root}
	}
	add("managed.coverage", nativediagnostics.Hold, "sdk_payload_lineage_quota_parts_and_live_sql_uninspected")
	var native *arcingress.NativeObservation
	var raw *arcingress.RawObservation
	var journal *arcingress.MaintenanceObservation
	var sql *ingress.Inspection
	var source *spool.MetadataObservation
	var nativeErr, rawErr, journalErr error
	if in.Native == "" {
		add("managed.native", nativediagnostics.Hold, "not_selected")
	} else {
		n, e := arcingress.InspectNativeControl(ctx, in.Native)
		nativeErr = e
		if e != nil {
			issue("managed.native", e, false)
		} else {
			chain := execwire.Chain{ID: strconv.FormatUint(n.Identity.ChainID, 10), Genesis: n.Identity.Genesis}
			if !allowed(chain) {
				add("managed.native", nativediagnostics.Fail, "unsupported_managed_identity")
			} else {
				native = &n
				out.Observations = append(out.Observations, nativediagnostics.Observation{Kind: "managed.native", Basis: managedBasis, Chain: chain, Anchor: anchor(n.Identity), NativeTip: ref(n.Head.Tip), NativeEpoch: &n.Head.ImportEpoch, NativeReadEpoch: &n.Head.ReadEpoch})
				add("managed.native", nativediagnostics.Pass, "fixed_native_control_observed")
				if n.Head.ImportEpoch > 1 {
					add("managed.native.lineage", nativediagnostics.Hold, "epoch_lineage_not_traversed")
				}
			}
		}
		m, e := arcingress.InspectMaintenance(ctx, in.Native)
		journalErr = e
		if e != nil {
			issue("managed.maintenance", e, false)
		} else if !allowed(m.Point.Chain) {
			add("managed.maintenance", nativediagnostics.Fail, "unsupported_managed_identity")
		} else {
			journal = &m
			pending := m.Phase != "complete"
			out.Observations = append(out.Observations, nativediagnostics.Observation{Kind: "managed.maintenance", Basis: managedBasis, Chain: m.Point.Chain, Anchor: anchor(m.Identity), Visible: &m.Point.Ref, Root: &m.Point.Root, Phase: m.Phase, MaintenancePending: &pending})
			if pending {
				add("managed.maintenance", nativediagnostics.Hold, "explicit_recovery_required")
			} else {
				add("managed.maintenance", nativediagnostics.Pass, "historical_completion_not_worker_resume")
			}
		}
	}
	if in.Raw == "" {
		add("managed.raw", nativediagnostics.Hold, "not_selected")
	} else {
		r, e := arcingress.InspectRawControl(ctx, in.Raw)
		rawErr = e
		if e != nil {
			issue("managed.raw", e, false)
		} else {
			c := r.Control
			chain := execwire.Chain{ID: c.Chain.ID, Genesis: execwire.Hash(c.Chain.GenesisHash)}
			if !allowed(chain) {
				add("managed.raw", nativediagnostics.Fail, "unsupported_managed_identity")
			} else {
				raw = &r
				o := nativediagnostics.Observation{Kind: "managed.raw", Basis: managedBasis, Chain: chain, Anchor: execwire.Ref{Generation: 1, Height: c.Anchor.Height, Hash: c.Anchor.Hash}, Received: ref(c.Received), RawEpoch: &c.RawEpoch}
				if c.Version == 2 {
					o.NativeEpoch = &c.NativeEpoch
					o.Generation = &c.Generation
				}
				if r.Temporary {
					pending := true
					o.TransitionPending = &pending
					add("managed.raw", nativediagnostics.Hold, "owner_rename_temporary_present")
				}
				out.Observations = append(out.Observations, o)
				add("managed.raw", nativediagnostics.Pass, "fixed_raw_owner_observed_gc_uninspected")
			}
		}
	}
	reader := metadata.New(ctx)
	if in.Artifact == "" {
		add("managed.sql", nativediagnostics.Hold, "not_selected")
	} else {
		b, e := reader.File(in.Artifact)
		if e != nil {
			issue("managed.sql", e, true)
		} else {
			s, e := ingress.DecodeInspection(b)
			if e != nil {
				issue("managed.sql", e, true)
			} else if !allowed(s.Control.Chain) {
				add("managed.sql", nativediagnostics.Fail, "unsupported_managed_identity")
			} else {
				sql = &s
				c := s.Control
				pending := c.Rewinding || c.ReorgPending
				out.Observations = append(out.Observations, nativediagnostics.Observation{Kind: "managed.sql", Basis: sqlBasis, Chain: c.Chain, Anchor: c.Anchor, Visible: &c.Visible, Root: &c.Root, Generation: &c.Generation, MaintenancePending: &c.Maintenance, TransitionPending: &pending})
				if c.Maintenance || pending || s.RawPending || !c.BaselineReady {
					add("managed.sql", nativediagnostics.Hold, "exported_pending_or_incomplete")
				}
				add("managed.sql", nativediagnostics.Pass, "read_only_export_unauthenticated_not_live")
			}
		}
	}
	if in.Spool != "" {
		src, e := spool.InspectMetadata(ctx, in.Spool)
		if e != nil {
			issue("managed.source", e, false)
		} else {
			source = &src
			add("managed.source", nativediagnostics.Pass, "source_admission_and_ack_sampled")
		}
	} else {
		add("managed.source", nativediagnostics.Hold, "not_selected")
	}
	if native != nil && raw != nil {
		i, c := native.Identity, raw.Control
		compare("managed.native_raw.instance", i.Instance, c.Instance)
		compare("managed.native_raw.genesis", i.Genesis, execwire.Hash(c.Chain.GenesisHash))
		compare("managed.native_raw.anchor", i.Anchor, c.Anchor)
		if c.Version == 2 && c.NativeEpoch != native.Head.ImportEpoch {
			add("managed.native_raw.epoch", nativediagnostics.Hold, "separate_epoch_samples")
		}
		if c.Received.Height < native.Head.Tip.Height {
			add("managed.native_raw.progress", nativediagnostics.Hold, "separate_progress_samples")
		}
	}
	if journal != nil {
		m := journal
		if native != nil {
			compare("managed.journal.identity", m.Identity, native.Identity)
		}
		if in.Raw != "" {
			p, e := arcingress.InspectionPath(in.Raw)
			if e != nil {
				issue("managed.journal.raw", e, false)
			} else {
				compare("managed.journal.raw_path", m.RawPath, p)
			}
		}
		if in.Spool != "" {
			p, e := arcingress.InspectionPath(in.Spool)
			if e != nil {
				issue("managed.journal.source", e, false)
			} else {
				compare("managed.journal.source_path", m.SourcePath, p)
			}
		}
	}
	if sql != nil {
		s := sql
		c := s.Control
		if native != nil {
			compare("managed.sql_native.chain", c.Chain, execwire.Chain{ID: "5042002", Genesis: native.Identity.Genesis})
			compare("managed.sql_native.anchor", c.Anchor, anchor(native.Identity))
			compare("managed.sql_native.anchor_root", c.AnchorRoot, native.Identity.Anchor.Root)
			if c.Visible.Height > native.Head.Tip.Height || c.Visible.Height == native.Head.Tip.Height && (c.Visible.Hash != native.Head.Tip.Hash || c.Root != native.Head.Tip.Root) {
				add("managed.sql_native.public", nativediagnostics.Hold, "separate_public_native_samples")
			}
		}
		if s.Owner == nil || s.Owner.Binding == nil {
			add("managed.sql.owner", nativediagnostics.Hold, "owner_unbound_in_export")
		} else {
			owner := s.Owner
			b := owner.Binding
			if source != nil {
				compare("managed.source.chain", owner.Policy.Chain, source.Chain)
				compare("managed.source.anchor", owner.Policy.Anchor, source.Anchor)
				if source.Admission == nil {
					add("managed.source.admission", nativediagnostics.Fail, "managed_source_policy_missing")
				} else {
					compare("managed.source.admission", b.SourcePolicyDigest, *source.Admission)
				}
				if source.Committed == owner.Policy.Anchor && source.Digest == (execwire.Hash{}) {
					add("managed.source.ack", nativediagnostics.Hold, "anchor_without_result_ack")
				}
			}

			if owner.Policy.Module != arcingress.ModuleName {
				add("managed.sql.owner", nativediagnostics.Fail, "unsupported_owner_module")
			}
			var identity nativeinput.Identity
			if nativeinput.Decode(b.NativeIdentity, &identity) != nil || nativeinput.ValidIdentity(identity) != nil {
				add("managed.sql.owner", nativediagnostics.Fail, "invalid_native_binding")
			} else {
				compare("managed.sql.binding.instance", b.NativeInstance, identity.Instance)
				compare("managed.sql.binding.bootstrap", b.BootstrapDigest, identity.BootstrapDigest)
				if native != nil {
					compare("managed.sql.native_identity", identity, native.Identity)
				}
				if raw != nil {
					compare("managed.sql.raw_store", b.RawStoreID, raw.StoreID)
					compare("managed.sql.raw_instance", b.NativeInstance, raw.Control.Instance)
					if raw.Control.Version == 2 && c.Generation != raw.Control.Generation {
						add("managed.sql.raw_epoch", nativediagnostics.Hold, "separate_generation_samples")
					}
				}
				if journal != nil {
					compare("managed.sql.journal_policy", owner.PolicyDigest, journal.PolicyDigest)
					compare("managed.sql.journal_owner", owner.BindingDigest, journal.OwnerDigest)
					compare("managed.sql.journal_identity", identity, journal.Identity)
				}
			}
		}
		if journal != nil && journal.Permit != nil {
			if s.Retention == nil || *journal.Permit != s.Retention.Permit || journal.Receipt != nil && !reflect.DeepEqual(journal.Receipt, s.Retention.Receipt) {
				add("managed.sql.journal_completion", nativediagnostics.Hold, "separate_or_historical_retention_samples")
			}
		}
	}
	if source != nil && journal != nil && source.Committed == journal.Point.Ref {
		compare("managed.journal.ack_digest", journal.Point.Digest, source.Digest)
	}
	if source != nil {
		latest, e := spool.InspectMetadata(ctx, in.Spool)
		if e != nil || !reflect.DeepEqual(*source, latest) {
			add("managed.source.resample", nativediagnostics.Hold, "changed_during_read")
		}
	}
	if e := reader.Verify(); e != nil {
		issue("managed.sql.resample", e, true)
	}
	if in.Native != "" {
		n, e := arcingress.InspectNativeControl(ctx, in.Native)
		if native != nil && (e != nil || !reflect.DeepEqual(*native, n)) || native == nil && nativeErr != nil && e == nil {
			add("managed.native.resample", nativediagnostics.Hold, "changed_during_read")
		}
		m, e := arcingress.InspectMaintenance(ctx, in.Native)
		if journal != nil && (e != nil || !reflect.DeepEqual(*journal, m)) || journal == nil && journalErr != nil && e == nil {
			add("managed.journal.resample", nativediagnostics.Hold, "changed_during_read")
		}
	}
	if in.Raw != "" {
		r, e := arcingress.InspectRawControl(ctx, in.Raw)
		if raw != nil && (e != nil || !reflect.DeepEqual(*raw, r)) || raw == nil && rawErr != nil && e == nil {
			add("managed.raw.resample", nativediagnostics.Hold, "changed_during_read")
		}
	}
	if e := ctx.Err(); e != nil {
		issue("managed", e, false)
	}
	return out
}
