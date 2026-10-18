package indexingstatus

import (
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/feedtelemetry"
)

// Backlogs are from one capture DB snapshot. Runtime timings are a separate,
// process-local observation, never a network head or completion authority.
type PipelineStatus struct {
	ProofOldestPendingAgeMS *float64               `json:"proofOldestPendingAgeMs"`
	StateOldestReadyAgeMS   *float64               `json:"stateOldestReadyAgeMs"`
	Runtime                 feedtelemetry.Snapshot `json:"runtime"`
	Generation              uint64                 `json:"generation"`
	ProofBacklog            *uint64                `json:"proofBacklog"`
	StateBacklog            *uint64                `json:"stateBacklog"`
	ReadyStateBacklog       *uint64                `json:"readyStateBacklog"`
	NetworkHead             *model.BlockPoint      `json:"networkHead"`
}

func readPipeline(cap capture.StatusSnapshot, d model.ProjectionDescriptor, r feedtelemetry.Snapshot) *PipelineStatus {
	out := &PipelineStatus{Runtime: r, Generation: cap.State.Generation}
	// A reset invalidates old process progress as a current-epoch diagnostic.
	// Stage samples cover the whole run; observed headers remain untrusted.
	for name, p := range out.Runtime.Progress {
		if p.Input.Generation != cap.State.Generation {
			delete(out.Runtime.Progress, name)
		}
	}
	if cap.StateProofs != nil && cap.StateProofs.Status != "stale" {
		v := cap.StateProofs.Backlog
		out.ProofBacklog = &v
	}
	if cap.StateChanges != nil && cap.StateChanges.Status != "stale" {
		v := cap.StateChanges.Backlog
		out.StateBacklog = &v
	}
	if out.ProofBacklog != nil && out.StateBacklog != nil && *out.StateBacklog >= *out.ProofBacklog {
		v := *out.StateBacklog - *out.ProofBacklog
		out.ReadyStateBacklog = &v
	}
	digest, _ := d.Plan.Digest()
	if out.ProofBacklog != nil && *out.ProofBacklog > 0 {
		next := d.StateProofs.FromHeight
		if cap.StateProofs.Complete != nil {
			next = cap.StateProofs.Complete.Height + 1
		}
		out.ProofOldestPendingAgeMS = feedtelemetry.PendingAge(d.Telemetry, d.Anchor, digest, feedtelemetry.Captured, cap.State.Generation, next)
	}
	if out.ReadyStateBacklog != nil && *out.ReadyStateBacklog > 0 {
		next := d.StateChanges.Stream.FromHeight
		if cap.StateChanges.Complete != nil {
			next = cap.StateChanges.Complete.Height + 1
		}
		out.StateOldestReadyAgeMS = feedtelemetry.PendingAge(d.Telemetry, d.Anchor, digest, feedtelemetry.Proved, cap.State.Generation, next)
	}
	return out
}
