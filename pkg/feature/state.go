package feature

import (
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// BackfillJob asks for one unit (a feature, or a part of one) to process
// blocks From..To. An Online job runs in the background while ingest
// continues. Definition is the part's, to keep in its state.
type BackfillJob struct {
	Feature    string
	From, To   uint64
	Online     bool
	Definition string `json:",omitempty"`
}

// OrderIndependent is implemented by features whose result does not depend
// on the order blocks are processed in (their keys identify each event, and
// they keep no running totals or latest-state records). Such a feature can be
// backfilled while ingest continues.
type OrderIndependent interface {
	OrderIndependent() bool
}

// IsOrderIndependent reports whether the registered feature name (or the
// feature of a part) declares itself order-independent.
func IsOrderIndependent(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := features[FeatureOf(name)].(OrderIndependent)
	return ok && f.OrderIndependent()
}

// Reconcile is ReconcileUnits for features without parts.
func Reconcile(enabled []string, states map[string]port.FeatureState, latest uint64, hasData bool) (map[string]port.FeatureState, []BackfillJob) {
	units := make([]Unit, len(enabled))
	for i, n := range enabled {
		units[i] = Unit{Name: n}
	}
	writes, jobs, _ := ReconcileUnits(units, states, latest, hasData) // fails only on part definitions
	return writes, jobs
}

// ReconcileUnits compares the enabled units (in execution order) with the
// stored feature states of a database whose latest indexed block is latest
// (hasData false for an empty database). It returns the states to write now
// and the backfills to run, in order (feature registry design, 8.2).
//
// Order-independent features (IsOrderIndependent) are backfilled online: they
// are recorded active at once, with the blocks they missed as a gap, and the
// job is marked Online. A gap left by an interrupted online backfill is
// resumed.
//
// A part is reconciled like a feature of its own, so a part added to an
// indexed feature is backfilled alone. A part whose stored definition
// differs from its current one is an error: its data is of the earlier
// definition. A database indexed before its feature had parts recorded the
// feature only; the parts take over the feature's state.
func ReconcileUnits(enabled []Unit, states map[string]port.FeatureState, latest uint64, hasData bool) (map[string]port.FeatureState, []BackfillJob, error) {
	writes := map[string]port.FeatureState{}
	on := map[string]bool{}
	hasParts := map[string]bool{} // features recorded by their parts
	for _, u := range enabled {
		on[u.Name] = true
		if f := FeatureOf(u.Name); f != u.Name {
			hasParts[f] = true
		}
	}
	// define returns st with the unit's definition.
	define := func(u Unit, st port.FeatureState) port.FeatureState {
		st.Definition = u.Definition
		return st
	}

	// An empty database, or one written before feature states existed (the
	// features then always ran inside the fetcher): every enabled feature is
	// complete.
	if !hasData || len(states) == 0 {
		for _, u := range enabled {
			writes[u.Name] = define(u, port.FeatureState{Active: true})
		}
		return writes, nil, nil
	}

	recordedParts := map[string]bool{}
	for n := range states {
		if f := FeatureOf(n); f != n {
			recordedParts[f] = true
		}
	}

	var jobs []BackfillJob
	for _, u := range enabled {
		n := u.Name
		st, known := states[n]
		if f := FeatureOf(n); !known && f != n && !recordedParts[f] {
			// The feature was recorded before it had parts: each part has
			// what the feature had.
			if st, known = states[f]; known {
				st = define(u, st)
				writes[n] = st
			}
		}
		if known && st.Definition != u.Definition && st.Definition != "" {
			return nil, nil, fmt.Errorf("%s changed since it was indexed, so its data is of the earlier definition: "+
				"give it a new name to index it anew, or reindex the database", n)
		}
		var from uint64
		switch {
		case known && st.Active && st.Gap != nil:
			jobs = append(jobs, BackfillJob{Feature: n, From: st.Gap.From, To: st.Gap.To, Online: true, Definition: u.Definition})
			continue
		case known && st.Active:
			if st.Definition == "" && u.Definition != "" {
				writes[n] = define(u, st)
			}
			continue // up to date
		case known && st.Through >= latest:
			writes[n] = define(u, port.FeatureState{Active: true})
			continue
		case known:
			from = st.Through + 1
		default:
			from = 0
		}
		if IsOrderIndependent(n) {
			writes[n] = define(u, port.FeatureState{Active: true, Gap: &port.BlockRange{From: from, To: latest}})
			jobs = append(jobs, BackfillJob{Feature: n, From: from, To: latest, Online: true, Definition: u.Definition})
			continue
		}
		jobs = append(jobs, BackfillJob{Feature: n, From: from, To: latest, Definition: u.Definition})
	}
	for n, st := range states {
		if on[n] || !st.Active || hasParts[n] {
			continue // enabled, already off, or recorded by its parts now
		}
		through := latest
		if st.Gap != nil {
			// It never filled its gap: complete only below the gap. A gap
			// from genesis leaves block 0, which has no transactions, so
			// an order-independent feature has nothing to record there.
			through = 0
			if st.Gap.From > 0 {
				through = st.Gap.From - 1
			}
		}
		writes[n] = port.FeatureState{Through: through, Definition: st.Definition}
	}
	return writes, jobs, nil
}

// Only returns a pipeline with the handlers of one unit: a feature (with
// all its parts) or one part.
func (p *Pipeline) Only(name string) *Pipeline {
	out := &Pipeline{deps: p.deps}
	for _, h := range p.handlers {
		if h.feature == name || h.unit() == name {
			out.handlers = append(out.handlers, h)
		}
	}
	return out
}
