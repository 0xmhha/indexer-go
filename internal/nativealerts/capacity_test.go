package nativealerts

import (
	"math"
	"testing"

	"github.com/0xmhha/indexer-go/internal/nativeobserve"
)

func capacityFixture() (Rule, nativeobserve.Binding, nativeobserve.Observation) {
	r := rules()
	r.Mode = BudgetBPS
	r.Warn, r.Stop, r.Clear = 8000, 9500, 7000
	scope := nativeobserve.Scope{Kind: nativeobserve.Database, ID: r.Key.Resource}
	b := nativeobserve.Binding{Scope: scope, Metric: nativeobserve.DatabaseBytes, Unit: nativeobserve.Bytes, Definition: nativeobserve.DatabaseLogical, Source: nativeobserve.Source{Token: nativeobserve.SourceToken{1}, Incarnation: 1, Run: nativeobserve.RunID{1}}, Policy: r.Policy, Series: nativeobserve.Digest{1}, Budget: nativeobserve.Budget{Scope: scope, Definition: nativeobserve.DatabaseLogical, ID: nativeobserve.Digest{1}, Limit: nativeobserve.Number{Present: true, Value: math.MaxUint64}}}
	o := nativeobserve.Observation{Binding: b, Number: nativeobserve.Number{Present: true, Value: math.MaxUint64}, Quality: nativeobserve.Known, Coverage: nativeobserve.Exact, Sequence: 1, StartedAt: clock(), AcquiredAt: clock()}
	return r, b, o
}
func TestCapacityUsesExactBudgetAndIndependentQuality(t *testing.T) {
	r, b, o := capacityFixture()
	in, err := CapacitySignal(r, b, o, clock(), false)
	if err != nil || in.Quality != Good {
		t.Fatal(err, in)
	}
	s := State{Key: r.Key, Incarnation: Incarnation{1}}
	s, event, err := Reduce(s, r, in, clock())
	if err != nil || event == nil || s.Active != Critical {
		t.Fatal("full-width ratio overflowed")
	}
	b.Budget.Limit.Value = 100
	o.Binding = b
	o.Number.Value = 101
	in, err = CapacitySignal(r, b, o, clock(), false)
	if err != nil || in.Quality != Good {
		t.Fatal("real over-budget usage became invalid")
	}
	o.Coverage = nativeobserve.LowerBound
	in, err = CapacitySignal(r, b, o, clock(), false)
	if err != nil || in.Quality != Invalid {
		t.Fatal("inexact evidence treated as exact")
	}
	o.Coverage = nativeobserve.Exact
	o.Binding.Source.Token[0]++
	in, err = CapacitySignal(r, b, o, clock(), false)
	if err != nil || in.Quality != Invalid {
		t.Fatal("another source accepted")
	}
}
