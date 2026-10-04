package feature

import (
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// BackfillJob asks for one feature to process blocks From..To.
type BackfillJob struct {
	Feature  string
	From, To uint64
}

// Reconcile compares the enabled features (in execution order) with the
// stored feature states of a database whose latest indexed block is latest
// (hasData false for an empty database). It returns the states to write now
// and the backfills to run, in order (feature registry design, 8.2).
func Reconcile(enabled []string, states map[string]storage.FeatureState, latest uint64, hasData bool) (map[string]storage.FeatureState, []BackfillJob) {
	writes := map[string]storage.FeatureState{}
	on := map[string]bool{}
	for _, n := range enabled {
		on[n] = true
	}

	// An empty database, or one written before feature states existed (the
	// features then always ran inside the fetcher): every enabled feature is
	// complete.
	if !hasData || len(states) == 0 {
		for _, n := range enabled {
			writes[n] = storage.FeatureState{Active: true}
		}
		return writes, nil
	}

	var jobs []BackfillJob
	for _, n := range enabled {
		st, known := states[n]
		switch {
		case known && st.Active:
			// up to date
		case known && st.Through >= latest:
			writes[n] = storage.FeatureState{Active: true}
		case known:
			jobs = append(jobs, BackfillJob{Feature: n, From: st.Through + 1, To: latest})
		default:
			jobs = append(jobs, BackfillJob{Feature: n, From: 0, To: latest})
		}
	}
	for n, st := range states {
		if !on[n] && st.Active {
			writes[n] = storage.FeatureState{Through: latest}
		}
	}
	return writes, jobs
}

// Only returns a pipeline with the handlers of one feature.
func (p *Pipeline) Only(name string) *Pipeline {
	out := &Pipeline{deps: p.deps}
	for _, h := range p.handlers {
		if h.feature == name {
			out.handlers = append(out.handlers, h)
		}
	}
	return out
}
