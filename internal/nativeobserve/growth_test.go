package nativeobserve

import (
	"math"
	"testing"
	"time"
)

func growthPair() (Observation, Observation) {
	b := databaseBinding()
	before, current := sample(b, clock().Add(-time.Minute)), sample(b, clock())
	before.Number.Value, current.Number.Value, current.Sequence = 100, 200, 2
	return current, before
}

func TestGrowthExactHorizonAndUnresolvedIsNotHealthy(t *testing.T) {
	current, previous := growthPair()
	c := catalog(t, current.Binding)
	g, a := c.CompareGrowth(current, previous, clock(), time.Hour, time.Minute, time.Hour)
	if !a.Usable() {
		t.Fatal(a)
	}
	for _, tc := range []struct {
		h       time.Duration
		reached bool
	}{{480 * time.Second, true}, {480*time.Second - time.Nanosecond, false}, {481 * time.Second, true}} {
		reached, known := g.Within(tc.h)
		if !known || reached != tc.reached {
			t.Fatalf("horizon %v: %v,%v", tc.h, reached, known)
		}
	}
	if _, known := g.Within(0); known {
		t.Fatal("zero horizon resolved")
	}
	current.Binding.Budget.Limit.Value, previous.Binding.Budget.Limit.Value = math.MaxUint64, math.MaxUint64
	current.Number.Value, previous.Number.Value = math.MaxUint64-1, 0
	g, a = catalog(t, current.Binding).CompareGrowth(current, previous, clock(), time.Hour, time.Minute, time.Hour)
	if !a.Usable() {
		t.Fatal(a)
	}
	if reached, known := g.Within(time.Nanosecond); !known || !reached {
		t.Fatal("full-width product overflowed")
	}
	if _, known := (Growth{}).Within(time.Hour); known {
		t.Fatal("unresolved growth became healthy")
	}
}

func TestHistoricalSampleIsNotRequiredToBeCurrent(t *testing.T) {
	current, previous := growthPair()
	// Ten-second freshness applies to the current value. The valid one-minute
	// history is governed by its original quality and declared pair interval.
	g, a := catalog(t, current.Binding).CompareGrowth(current, previous, clock(), 10*time.Second, time.Minute, time.Hour)
	if !a.Usable() {
		t.Fatalf("valid historic sample became a current stale value: %+v", a)
	}
	if _, known := g.Within(time.Hour); !known {
		t.Fatal("valid historic pair unresolved")
	}
}

func TestGrowthBindingResetAndIndependentSampleValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Observation, *Observation)
		code   Code
	}{
		{"run", func(_, p *Observation) { p.Binding.Source.Run[0]++ }, SeriesReset},
		{"incarnation", func(_, p *Observation) { p.Binding.Source.Incarnation++ }, SeriesReset},
		{"series", func(_, p *Observation) { p.Binding.Series[0]++ }, SeriesReset},
		{"policy", func(_, p *Observation) { p.Binding.Policy[0]++ }, SeriesReset},
		{"budget", func(_, p *Observation) { p.Binding.Budget.Limit.Value++ }, SeriesReset},
		{"scope", func(_, p *Observation) { p.Binding.Scope.ID[0]++; p.Binding.Budget.Scope = p.Binding.Scope }, SeriesReset},
		{"old-error-even-current-missing", func(c, p *Observation) {
			c.Number = Number{}
			p.Quality = Failed
			p.Number = Number{}
			p.Cause = PermissionDenied
		}, SampleFailed},
		{"old-zero-budget-even-current-missing", func(c, p *Observation) { c.Number = Number{}; p.Binding.Budget.Limit.Value = 0 }, InvalidNumber},
		{"future-previous-even-missing", func(c, p *Observation) { c.Number = Number{}; p.AcquiredAt = clock().Add(time.Second) }, InvalidTime},
		{"explicit-stale-previous", func(_, p *Observation) { p.Quality = Stale }, SampleStale},
		{"history-outside-interval", func(_, p *Observation) { p.AcquiredAt = clock().Add(-2 * time.Hour); p.StartedAt = p.AcquiredAt }, InvalidInterval},
		{"flat", func(c, p *Observation) { c.Number.Value = p.Number.Value }, NoGrowth},
		{"shrink", func(c, p *Observation) { c.Number.Value = p.Number.Value - 1 }, NoGrowth},
		{"same-sequence", func(c, p *Observation) { c.Sequence = p.Sequence }, InvalidSequence},
		{"lost-sequence", func(c, _ *Observation) { c.Sequence++ }, SequenceGap},
		{"interval-short", func(_, p *Observation) { p.AcquiredAt = clock().Add(-time.Second); p.StartedAt = p.AcquiredAt }, InvalidInterval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, previous := growthPair()
			c := catalog(t, current.Binding)
			tc.mutate(&current, &previous)
			beforeC, beforeP := current, previous
			g, a := c.CompareGrowth(current, previous, clock(), time.Hour, time.Minute, time.Hour)
			requireCode(t, a, tc.code)
			if _, known := g.Within(time.Hour); known {
				t.Fatal("invalid pair produced a forecast")
			}
			if current != beforeC || previous != beforeP {
				t.Fatal("pair inputs mutated")
			}
		})
	}
	// Restoring a raw window changes its own typed epoch, not capture generation.
	b := positionBinding(RawBytes)
	current, previous := sample(b, clock()), sample(b, clock().Add(-time.Minute))
	current.Number.Value, previous.Number.Value, current.Sequence = 200, 100, 2
	previous.Binding.Epochs.Raw++
	_, a := catalog(t, b).CompareGrowth(current, previous, clock(), time.Hour, time.Minute, time.Hour)
	requireCode(t, a, SeriesReset)
}
