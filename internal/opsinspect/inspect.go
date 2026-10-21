// Package opsinspect observes checkpoint and deletion metadata without starting
// an application, network source, window writer or recovery coordinator.
package opsinspect

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/0xmhha/indexer-go/internal/indexingstatus"
	"github.com/0xmhha/indexer-go/internal/progress"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
)

var ErrChanged = errors.New("inspection: metadata changed during observation")

type Database interface {
	port.CaptureDatabase
	port.IndexingReader
}

type fixedPage struct {
	Database
	page port.IndexingPage
}

func (r fixedPage) ReadIndexingPage(context.Context, string, int) (port.IndexingPage, error) {
	return r.page, nil
}

type Report struct {
	Version           int                        `json:"version"`
	ObservedAt        time.Time                  `json:"observedAt"`
	Scope             string                     `json:"scope"`
	Consistency       string                     `json:"consistency"`
	Checkpoint        capture.CheckpointSnapshot `json:"checkpoint"`
	Projection        indexingstatus.Status      `json:"projection"`
	Raw               window.Inspection          `json:"raw"`
	UnitsPageComplete bool                       `json:"unitsPageComplete"`
	Findings          []string                   `json:"findings"`
	GCDecision        string                     `json:"gcDecision"`
}

// Read inspects one bounded feature page. Next/UnitsPageComplete prevent a partial
// page from masquerading as all required-unit completion. Source metadata is
// re-read after filesystem inspection. DB pages and files remain separate
// observations; stable metadata does not prove payload integrity or GC safety.
func Read(ctx context.Context, db Database, chain model.FeedChain, opts window.Options, after string, limit int) (out Report, err error) {
	if db == nil || opts.Chain != chain || chain.Validate() != nil || limit < 1 || limit > 100 || after != "" && !progress.ValidName(after) {
		return out, port.ErrInvalidData
	}
	page, err := db.ReadIndexingPage(ctx, after, limit)
	if err != nil {
		return out, err
	}
	first, err := capture.ReadCheckpoint(ctx, db, chain)
	if err != nil {
		return out, err
	}
	if bind := first.Binding; bind != nil && (bind.Volume != opts.Volume.ID || bind.Root != opts.Volume.Root || bind.Path != opts.Path || bind.Reservation != opts.Reservation) {
		return out, capture.ErrConflict
	}
	status, err := indexingstatus.Read(ctx, fixedPage{Database: db, page: page}, after, limit)
	if err != nil {
		return out, err
	}
	if status.Source != nil {
		src := status.Source
		digest, _ := first.State.Plan.Digest()
		if src.PlanDigest != digest {
			return out, ErrChanged
		}
		if src.Chain != chain || src.Generation != first.State.Generation || src.Anchor != first.State.Anchor {
			return out, ErrChanged
		}
		if src.Through == nil && first.State.Through != first.State.Anchor || src.Through != nil && (src.Through.Height != first.State.Through.Number || src.Through.Hash != first.State.Through.Hash) {
			return out, ErrChanged
		}
	}
	var refs []window.Reference
	if first.Pending != nil {
		refs = first.Pending.References
	}
	raw, err := window.Inspect(ctx, opts, refs)
	if err != nil {
		return out, err
	}
	last, err := capture.ReadCheckpoint(ctx, db, chain)
	if err != nil {
		return out, err
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(last)
	if string(a) != string(b) {
		return out, ErrChanged
	}
	lastPage, err := db.ReadIndexingPage(ctx, after, limit)
	if err != nil {
		return out, err
	}
	a, _ = json.Marshal(page)
	b, _ = json.Marshal(lastPage)
	if string(a) != string(b) {
		return out, ErrChanged
	}
	out = Report{Version: 1, ObservedAt: time.Now().UTC(), Scope: "checkpoint_and_gc_metadata", Consistency: "separate_snapshots_rechecked", Checkpoint: first, Projection: status, Raw: raw, UnitsPageComplete: after == "" && status.Next == "", Findings: []string{}, GCDecision: "not_authorized"}
	if first.Binding == nil {
		out.Findings = append(out.Findings, "window_binding_unreported")
	}
	if first.Pending != nil {
		out.Findings = append(out.Findings, "pending_deletion_requires_coordinated_recovery")
	}
	if first.ACKTruncated {
		out.Findings = append(out.Findings, "raw_ack_observation_truncated")
	}
	for _, ack := range first.RawACKs {
		if ack.Status == "missing" {
			out.Findings = append(out.Findings, "raw_ack_missing_in_observed_scope")
			break
		}
	}
	if len(raw.Intents) > 0 && first.Pending == nil {
		out.Findings = append(out.Findings, "raw_intent_without_db_fence")
	}
	if first.Pending != nil {
		for _, intent := range raw.Intents {
			if intent != *first.PendingRecord {
				out.Findings = append(out.Findings, "unmatched_raw_intent")
			}
		}
	}
	if !out.UnitsPageComplete {
		out.Findings = append(out.Findings, "feature_page_incomplete")
	}
	if status.Source == nil {
		out.Findings = append(out.Findings, "projection_scope_unreported")
	}
	if status.Projection != nil && !status.ProjectionCanonical {
		out.Findings = append(out.Findings, "projection_head_not_current_canonical")
	}
	return out, ctx.Err()
}
