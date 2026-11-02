package nativealerts

import (
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"time"
)

// CapacitySignal evaluates exactly one resource component. A legitimate value
// above a declared budget remains critical evidence; other qualification errors
// cannot be masked. Derived backlog/lag need their own verified adapters in D.
func CapacitySignal(r Rule, expected nativeobserve.Binding, o nativeobserve.Observation, at time.Time, lost bool) (Signal, error) {
	if r.Validate() != nil || r.Key.Kind != Capacity || r.Key.Resource != expected.Scope.ID || r.Key.Metric != expected.Metric {
		return Signal{}, ErrRule
	}
	c, a := nativeobserve.NewCatalog([]nativeobserve.Binding{expected})
	if !a.Usable() {
		return Signal{}, ErrRule
	}
	s := Signal{Key: r.Key, Binding: digest(o.Binding), Run: o.Binding.Source.Run, Sequence: o.Sequence, ObservedAt: o.AcquiredAt, Value: o.Number, Limit: o.Binding.Budget.Limit, Quality: Good}
	for _, code := range c.Qualify(o, at, r.MaxAge).Codes {
		switch code {
		case nativeobserve.BudgetExceeded: // measured usage, not a bad limit
		case nativeobserve.SampleStale:
			if s.Quality != Invalid {
				s.Quality = Stale
			}
		case nativeobserve.MissingValue, nativeobserve.MissingTime, nativeobserve.SampleFailed, nativeobserve.SampleUnsupported, nativeobserve.BudgetMissing:
			if s.Quality == Good {
				s.Quality = Missing
			}
		default:
			s.Quality = Invalid
		}
	}
	if lost && s.Quality == Good {
		s.Quality = Gap
	}
	return s, nil
}
