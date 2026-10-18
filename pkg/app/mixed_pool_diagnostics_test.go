package app

import (
	"maps"
	"net/http"
	"runtime/trace"
	"sync"
	"time"

	apigraphql "github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
)

type mixedAPIClientTiming struct {
	PrepareMS, HTTPMS, ValidationMS float64
	HTTPStarted, ValidationStarted  bool
}

func mixedMS(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func (t *mixedAPITrace) observeGraphQL(o apigraphql.RequestObservation) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.data.GraphQLMS == nil {
		t.data.GraphQLMS = make(map[string]float64)
	}
	t.data.GraphQLMS[o.Stage] += mixedMS(o.Elapsed)
}
func (t *mixedAPITrace) observeRead(o postgres.ReadObservation) {
	t.mu.Lock()
	switch o.Stage {
	case "block_decode":
		t.data.BlockDecodeMS += mixedMS(o.Elapsed)
	case "admission":
		t.data.AdmissionMS += mixedMS(o.Elapsed)
		if t.data.AdmissionPaths == nil {
			t.data.AdmissionPaths = make(map[string]int)
		}
		t.data.AdmissionPaths[o.Path]++
		t.data.BorrowProbes += o.BorrowProbes
		if o.Failed {
			t.data.AdmissionFailures++
		}
	}
	t.mu.Unlock()
	if t.readHook != nil {
		t.readHook(o) // functional-test barrier; nil in the measurement fixture
	}
}

// Share the exact request-correlation/diagnostic path between the functional
// DB tests and the opt-in load fixture. Neither the header nor this hook is
// installed by the production API server.
func mixedObservedHTTP(next http.Handler, traces *sync.Map) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value, ok := traces.Load(r.Header.Get("X-Indexer-Measure")); ok {
			observation := value.(*mixedAPITrace)
			begin := time.Now()
			region := trace.StartRegion(r.Context(), "api.request")
			defer region.End()
			if trace.IsEnabled() {
				trace.Logf(r.Context(), "api.request", "%s %d", r.Header.Get("X-Indexer-Measure"), begin.UnixNano())
			}
			defer func() {
				finished := time.Now()
				observation.mu.Lock()
				observation.data.ServerMS = mixedMS(finished.Sub(begin))
				observation.data.ServerStarted, observation.data.ServerFinished = begin, finished
				observation.data.HandlerComplete = true
				observation.mu.Unlock()
				close(observation.done)
			}()
			ctx := postgres.ObserveQueries(r.Context(), observation.observe)
			ctx = postgres.ObserveReads(ctx, observation.observeRead)
			r = r.WithContext(apigraphql.ObserveRequest(ctx, observation.observeGraphQL))
		}
		next.ServeHTTP(w, r)
	})
}
func (t *mixedAPITrace) snapshot() mixedAPIDiagnostics {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.data
	out.GraphQLMS = maps.Clone(out.GraphQLMS)
	out.AdmissionPaths = maps.Clone(out.AdmissionPaths)
	return out
}

// Handler completion and interval containment are independent of client
// success: cancellation or an early response can overlap the handler. Do not
// turn unavailable/overlapping intervals into zero or negative "network" time.
func mixedOutsideHandler(row mixedAPIObservation) *float64 {
	d := row.Diagnostics
	if !d.HandlerComplete || d.ServerStarted.IsZero() || d.ServerFinished.IsZero() ||
		d.ServerStarted.Before(row.Started) || d.ServerFinished.After(row.Finished) ||
		d.ServerFinished.Before(d.ServerStarted) || d.ServerMS < 0 || row.MS < 0 || d.ServerMS > row.MS {
		return nil
	}
	ms := row.MS - d.ServerMS
	return &ms
}

type mixedAPIValidation struct {
	Passed                bool
	Requests              int
	Errors                map[string]int
	IncompleteDiagnostics int
}

func mixedAPIVerdict(rows []mixedAPIObservation) mixedAPIValidation {
	v := mixedAPIValidation{Requests: len(rows), Errors: make(map[string]int)}
	for _, row := range rows {
		if row.Result != "ok" {
			v.Errors[row.Result]++
		}
		if !row.Diagnostics.HandlerComplete {
			v.IncompleteDiagnostics++
		}
	}
	v.Passed = v.Requests > 0 && len(v.Errors) == 0 && v.IncompleteDiagnostics == 0
	return v
}
