package checkpoint

import (
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

type RecoveryReport struct {
	Status         string
	Findings       []string
	Recommendation string
}

// Diagnose never authorizes startup, ACK, deletion or generation changes.
func Diagnose(db port.ExecutionControl, src format.SourceObservation) RecoveryReport {
	r := RecoveryReport{Status: "blocked", Recommendation: "Preserve DB/node/spool. Restore a mutually matched backup, or verify a new checkpoint into a new schema and new spool; never lower source generation or SetHead blindly."}
	add := func(v string) { r.Findings = append(r.Findings, v) }
	if src.Committed.Generation == 0 || src.Committed.Generation < src.Anchor.Generation || src.Committed.Height < src.Anchor.Height || src.Committed.Hash == ([32]byte{}) {
		add("invalid source checkpoint reference")
	}
	if db.Chain != src.Chain || db.Anchor != src.Anchor {
		add("chain or original anchor mismatch")
	}
	if db.Rewinding {
		add("DB has unfinished rewind; use existing fenced recovery")
	}
	if !db.BaselineReady {
		add("DB baseline is not ready")
	}
	if src.TransitionPending {
		add("source has unfinished generation transition; inspect with owner stopped")
	}
	if src.Committed.Generation > db.Generation {
		add("source generation ahead of restored DB")
	}
	if src.Committed.Generation >= db.Generation && src.Committed.Height > db.Visible.Height {
		add("source durable ACK ahead of DB public commit")
	}
	if src.Committed.Generation == db.Generation && src.Committed.Height == db.Visible.Height && src.Committed.Hash != db.Visible.Hash {
		add("same-generation ACK hash conflict")
	}
	if !src.HeaderAvailable || src.Parent.Height != db.Visible.Height || src.Parent.Hash != db.Visible.Hash || src.ParentRoot != db.Root {
		add("exact public parent header/root unavailable or mismatched")
	}
	if !src.StateAvailable {
		add("exact public parent state unavailable; latest state is not a substitute")
	}
	if len(r.Findings) == 0 {
		r.Status = "compatible_observation"
		r.Recommendation = "An older source generation requires the existing verified rebind. Use the normal authenticated Hello/backlog/fresh gates. This observation is not permission to ACK, delete, change generation or start a node."
	}
	return r
}
