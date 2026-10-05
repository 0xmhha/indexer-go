package feature

import (
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// BackfillJob asks for one feature to process blocks From..To. An Online job
// runs in the background while ingest continues.
type BackfillJob struct {
	Feature  string
	From, To uint64
	Online   bool
}

// OrderIndependent is implemented by features whose result does not depend
// on the order blocks are processed in (their keys identify each event, and
// they keep no running totals or latest-state records). Such a feature can be
// backfilled while ingest continues.
type OrderIndependent interface {
	OrderIndependent() bool
}

// IsOrderIndependent reports whether the registered feature name declares
// itself order-independent.
func IsOrderIndependent(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := features[name].(OrderIndependent)
	return ok && f.OrderIndependent()
}

// Reconcile compares the enabled features (in execution order) with the
// stored feature states of a database whose latest indexed block is latest
// (hasData false for an empty database). It returns the states to write now
// and the backfills to run, in order (feature registry design, 8.2).
//
// Order-independent features (IsOrderIndependent) are backfilled online: they
// are recorded active at once, with the blocks they missed as a gap, and the
// job is marked Online. A gap left by an interrupted online backfill is
// resumed.
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
		var from uint64
		switch {
		case known && st.Active && st.Gap != nil:
			jobs = append(jobs, BackfillJob{Feature: n, From: st.Gap.From, To: st.Gap.To, Online: true})
			continue
		case known && st.Active:
			continue // up to date
		case known && st.Through >= latest:
			writes[n] = storage.FeatureState{Active: true}
			continue
		case known:
			from = st.Through + 1
		default:
			from = 0
		}
		if IsOrderIndependent(n) {
			writes[n] = storage.FeatureState{Active: true, Gap: &storage.BlockRange{From: from, To: latest}}
			jobs = append(jobs, BackfillJob{Feature: n, From: from, To: latest, Online: true})
			continue
		}
		jobs = append(jobs, BackfillJob{Feature: n, From: from, To: latest})
	}
	for n, st := range states {
		if !on[n] && st.Active {
			writes[n] = storage.FeatureState{Through: latest}
			if st.Gap != nil {
				// It never filled its gap: complete only below the gap. A gap
				// from genesis leaves block 0, which has no transactions, so
				// an order-independent feature has nothing to record there.
				through := uint64(0)
				if st.Gap.From > 0 {
					through = st.Gap.From - 1
				}
				writes[n] = storage.FeatureState{Through: through}
			}
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
