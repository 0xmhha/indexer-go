package capture

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/0xmhha/indexer-go/internal/progress"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// StatusSnapshot is a read-only capture view. It never calls the node or
// treats a historical receipt/output as current canonical completion.
type StatusSnapshot struct {
	State         State
	HeadCanonical bool
	Units         map[string]UnitStatus
	Workers       []WorkerStatus
	CallTraces    *SupplementStatus
	StateProofs   *SupplementStatus
	StateChanges  *StateProjectionStatus
}
type UnitStatus struct {
	Epoch       uint64            `json:"epoch"`
	Status      string            `json:"status"`
	Complete    *model.BlockPoint `json:"completeThrough"`
	LastApplied *model.BlockPoint `json:"lastApplied"`
}
type WorkerStatus struct {
	Name       string            `json:"name"`
	Definition string            `json:"definition"`
	Epoch      uint64            `json:"epoch"`
	Status     string            `json:"status"`
	Complete   *model.BlockPoint `json:"completeThrough"`
	Sequence   uint64            `json:"sequence"`
}

func ReadStatus(ctx context.Context, db port.CaptureDatabase, d model.ProjectionDescriptor, page port.IndexingPage) (out StatusSnapshot, err error) {
	if d.ValidateStateChanges() != nil || d.ValidateTelemetry() != nil || d.ValidateCallTraces() != nil || d.ValidateBlobCapability() != nil || d.ValidateSourceLookup() != nil || d.ValidateSourceSearch() != nil {
		return out, port.ErrInvalidData
	}
	if db == nil || d.Version != 1 || d.Anchor.Validate() != nil || d.Plan.Validate() != nil || len(d.Workers) > 8 {
		return out, port.ErrInvalidData
	}
	out.Units = map[string]UnitStatus{}
	out.Workers = []WorkerStatus{}
	err = db.CaptureView(ctx, d.Anchor.Chain, func(tx port.CaptureReader) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		out.State = st
		digest, e := d.Plan.Digest()
		if e != nil {
			return e
		}
		actual, e := st.Plan.Digest()
		if e != nil {
			return e
		}
		if digest != actual || st.Anchor != d.Anchor {
			return ErrConflict
		}
		if d.StateChanges != nil {
			state, e := stateProjectionStatus(tx, st, *d.StateChanges)
			if e != nil {
				return e
			}
			out.StateChanges = &state
		}
		if d.CallTraces != nil {
			state, e := supplementStatus(tx, st, *d.CallTraces)
			if e != nil {
				return e
			}
			out.CallTraces = &state
		}
		if d.StateProofs != nil {
			state, e := supplementStatus(tx, st, *d.StateProofs)
			if e != nil {
				return e
			}
			out.StateProofs = &state
		}
		canonical := func(p model.BlockPoint) (bool, error) {
			id, e := canonicalID(tx, st, model.FeedBlock{Chain: d.Anchor.Chain, Number: p.Height, Hash: p.Hash})
			if e == ErrConflict || e == ErrGap {
				return false, nil
			}
			if e != nil {
				return false, e
			}
			if e = checkProjection(tx, st.Generation, id); e != nil {
				return false, e
			}
			return true, nil
		}
		if page.Head != nil {
			out.HeadCanonical, e = canonical(*page.Head)
			if e != nil {
				return e
			}
		}

		for _, u := range page.Units {
			state := UnitStatus{Status: "unreported"}
			c := u.Checkpoint
			if c == nil {
				out.Units[u.Name] = state
				continue
			}
			if e := progress.Validate(*c); e != nil {
				return e
			}
			state.Epoch = c.Epoch
			state.Status = "stale"
			if c.Epoch != st.Generation || c.PlanDigest != digest || c.Chain != d.Anchor.Chain || c.Start != st.Anchor.Number+1 || c.Definition != u.State.Definition {
				out.Units[u.Name] = state
				continue
			}
			last := c.Ranges[len(c.Ranges)-1]
			lp := model.BlockPoint{Height: last.To, Hash: last.EndHash}
			ok, e := canonical(lp)
			if e != nil {
				return e
			}
			if !ok {
				out.Units[u.Name] = state
				continue
			}
			state.LastApplied = &lp
			state.Status = "backfilling"
			first := c.Ranges[0]
			if first.From == c.Start {
				firstOK, e := canonical(model.BlockPoint{Height: first.To, Hash: first.EndHash})
				if e != nil {
					return e
				}
				if !firstOK {
					state.Status = "stale"
					state.LastApplied = nil
					out.Units[u.Name] = state
					continue
				}
				n := first.To
				if gap := u.State.Gap; gap != nil && gap.From <= n {
					if gap.From <= c.Start {
						out.Units[u.Name] = state
						continue
					}
					n = gap.From - 1
				}
				// canonicalID needs the exact hash. Intermediate completion is looked up
				// from the canonical pointer, never reconstructed from the global head.
				id, e := canonicalAtHeight(tx, st, n)
				if e != nil {
					return e
				}
				state.Complete = &model.BlockPoint{Height: n, Hash: id.Block.Hash}
			}
			if !u.State.Active {
				state.Status = "disabled"
			} else if state.Complete != nil && state.Complete.Height == st.Through.Number && u.State.Gap == nil {
				state.Status = "caught_up"
			} else if state.Complete != nil && u.State.Gap == nil {
				state.Status = "lagging"
			}
			out.Units[u.Name] = state
		}
		for _, name := range d.Workers {
			if !validProjectionConsumer(name) {
				return port.ErrInvalidData
			}
			w := WorkerStatus{Name: name, Status: "not_started"}
			var c WorkerCursor
			e := getJSON(tx, workerPrefix(name)+"cursor", &c)
			if errors.Is(e, port.ErrNotFound) {
				out.Workers = append(out.Workers, w)
				continue
			}
			if e != nil {
				return e
			}
			if !validWorkerCursor(c) || c.Name != name || c.Anchor != st.Anchor {
				return ErrConflict
			}
			if name == model.SourceSearchWorker {
				if d.SourceSearch == nil || c.Definition != d.SourceSearch.Definition() {
					return ErrConflict
				}
				if c.Epoch == st.Generation {
					if _, _, e := loadSearch(tx, *d.SourceSearch, st.Generation); e != nil {
						return e
					}
				}
			}
			if name == model.SourceLookupWorker {
				if d.SourceLookup == nil || c.Definition != d.SourceLookup.Definition() {
					return ErrConflict
				}
				if c.Epoch == st.Generation {
					if _, _, e := loadLookup(tx, *d.SourceLookup, st.Generation); e != nil {
						return e
					}
				}
			}
			w.Definition, w.Epoch, w.Sequence = c.Definition, c.Epoch, c.Sequence
			w.Status = "stale"
			if c.Epoch == st.Generation {
				w.Status = "waiting"
				if c.Through.Number > st.Anchor.Number {
					p := model.BlockPoint{Height: c.Through.Number, Hash: c.Through.Hash}
					ok, e := canonical(p)
					if e != nil {
						return e
					}
					if ok {
						w.Complete = &p
						w.Status = "lagging"
						if c.Through == st.Through {
							w.Status = "caught_up"
						}
					}
				}
			}
			out.Workers = append(out.Workers, w)
		}
		return nil
	})
	return out, err
}
func canonicalAtHeight(tx port.CaptureReader, st State, n uint64) (model.CaptureIdentity, error) {
	if n <= st.Anchor.Number || n > st.Through.Number {
		return model.CaptureIdentity{}, port.ErrNotFound
	}
	var id model.CaptureIdentity
	if e := getJSON(tx, heightKey(n), &id); e != nil {
		return id, e
	}
	if id.Block.Number != n || id.Generation == 0 || id.Generation > st.Generation {
		return id, ErrConflict
	}
	if _, e := canonicalID(tx, st, id.Block); e != nil {
		return id, e
	}
	return id, nil
}

// ReadWorkerOutput serves only the selected worker's current canonical,
// completed prefix. Historical fork outputs require the explicit Store API.
func ReadWorkerOutput(ctx context.Context, db port.CaptureDatabase, d model.ProjectionDescriptor, name string, n uint64) (out WorkerOutput, epoch uint64, err error) {
	if db == nil || d.Version != 1 || d.Anchor.Validate() != nil || d.Plan.Validate() != nil || len(d.Workers) > 8 {
		return out, 0, port.ErrInvalidData
	}
	allowed := false
	for _, w := range d.Workers {
		if w == name {
			allowed = true
		}
	}
	if !allowed || !validProjectionConsumer(name) {
		return out, 0, port.ErrNotFound
	}
	err = db.CaptureView(ctx, d.Anchor.Chain, func(tx port.CaptureReader) error {
		st, e := loadState(tx)
		if e != nil {
			return e
		}
		epoch = st.Generation
		plan, e := st.Plan.Digest()
		if e != nil {
			return e
		}
		expected, e := d.Plan.Digest()
		if e != nil {
			return e
		}
		if d.Version != 1 || st.Anchor != d.Anchor || plan != expected {
			return ErrConflict
		}
		var c WorkerCursor
		if e = getJSON(tx, workerPrefix(name)+"cursor", &c); e != nil {
			return e
		}
		if !validWorkerCursor(c) || c.Name != name || c.Anchor != st.Anchor {
			return ErrConflict
		}
		if c.Epoch != st.Generation {
			return ErrStale
		}
		if n <= st.Anchor.Number || n > c.Through.Number {
			return port.ErrNotFound
		}
		if _, e = canonicalID(tx, st, c.Through); e != nil {
			return e
		}
		id, e := canonicalAtHeight(tx, st, n)
		if e != nil {
			return e
		}
		if e = getJSON(tx, workerOutputKey(name, c.Definition, id), &out); e != nil {
			return e
		}
		if out.Identity != id || out.Definition != c.Definition || len(out.Data) > MaxWorkerOutput || !json.Valid(out.Data) {
			return ErrConflict
		}
		return nil
	})
	return out, epoch, err
}
