package nativeobserve

import (
	"time"

	"github.com/0xmhha/indexer-go/internal/monitorpredicate"
)

// Growth is a conditional two-point estimate, never a promise of future usage.
// Fields are private so an invalid pair cannot be used as zero growth/headroom.
type Growth struct {
	delta, remaining uint64
	interval         time.Duration
}

// Within reports an inclusive exact horizon without division or rounding. A
// zero/invalid estimate or horizon is unresolved (false, false), never healthy.
func (g Growth) Within(horizon time.Duration) (reached, known bool) {
	if g.delta == 0 || g.interval <= 0 || horizon <= 0 {
		return false, false
	}
	return monitorpredicate.ProductGE(g.delta, uint64(horizon), g.remaining, uint64(g.interval)), true
}

// CompareGrowth qualifies both components independently. Changed scope,
// definition, budget, policy, source, series or run resets the temporal baseline;
// missing fields do not mask a known conflict in the other component.
func (c *Catalog) CompareGrowth(current, previous Observation, at time.Time, maxAge, minInterval, maxInterval time.Duration) (Growth, Assessment) {
	a := c.Qualify(current, at, maxAge)
	validateBinding(previous.Binding, &a)
	// History keeps its original quality/time and the pair's interval policy;
	// it is not projected as another current sample or timestamp-refreshed.
	qualifyValue(previous, at, maxAge, true, &a)
	if current.Binding != previous.Binding {
		a.add(SeriesReset)
	}
	if !budgeted(current.Binding.Metric) {
		a.add(SampleUnsupported)
	}
	if current.Sequence <= previous.Sequence {
		a.add(InvalidSequence)
	} else if current.Sequence-previous.Sequence != 1 {
		a.add(SequenceGap)
	}
	span := current.AcquiredAt.Sub(previous.AcquiredAt)
	if !current.AcquiredAt.IsZero() && !previous.AcquiredAt.IsZero() && span <= 0 {
		a.add(InvalidTime)
	}
	if minInterval <= 0 || maxInterval < minInterval || maxInterval > 24*time.Hour || span < minInterval || span > maxInterval {
		a.add(InvalidInterval)
	}
	if !a.Usable() {
		return Growth{}, a
	}
	if current.Number.Value <= previous.Number.Value {
		a.add(NoGrowth)
		return Growth{}, a
	}
	return Growth{current.Number.Value - previous.Number.Value, current.Binding.Budget.Limit.Value - current.Number.Value, span}, a
}
