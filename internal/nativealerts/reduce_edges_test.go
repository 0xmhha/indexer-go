package nativealerts

import (
	"math"
	"testing"
	"time"
)

func TestWarningAndRecoveryRequireCountAndDwell(t *testing.T) {
	r := rules()
	r.WarnDwell = 2 * time.Second
	s := State{Key: r.Key, Incarnation: Incarnation{1}}
	for seq := uint64(1); seq <= 3; seq++ {
		in := signal(seq, 80)
		next, event, err := Reduce(s, r, in, in.ObservedAt)
		if err != nil {
			t.Fatal(err)
		}
		if (event != nil) != (seq == 3) {
			t.Fatalf("warning dwell at %d", seq)
		}
		s = next
	}
	if s.Active != Warning {
		t.Fatal("warning absent")
	}
	for _, tc := range []struct {
		seq, value uint64
		resolved   bool
	}{{4, 70, false}, {5, 71, false}, {6, 70, false}, {7, 70, true}} {
		in := signal(tc.seq, tc.value)
		next, event, err := Reduce(s, r, in, in.ObservedAt)
		if err != nil {
			t.Fatal(err)
		}
		if (event != nil) != tc.resolved {
			t.Fatalf("clear hysteresis at %d: %+v", tc.seq, event)
		}
		s = next
	}
	if s.Active != Normal {
		t.Fatal("valid clear not reached")
	}
}

func TestResetInvalidAndUnknownNeverRecover(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*Rule, *Signal)
		quality Quality
	}{
		{"run", func(_ *Rule, in *Signal) { in.Run[0]++ }, Reset},
		{"series", func(_ *Rule, in *Signal) { in.Binding[0]++ }, Reset},
		{"policy", func(r *Rule, _ *Signal) { r.Clear = 69 }, Reset},
		{"future", func(_ *Rule, in *Signal) { in.ObservedAt = in.ObservedAt.Add(time.Hour) }, Invalid},
		{"stale", func(_ *Rule, in *Signal) { in.Quality = Stale }, Stale},
		{"missing", func(_ *Rule, in *Signal) { in.Quality = Missing }, Missing},
		{"gap", func(_ *Rule, in *Signal) { in.Sequence = 8 }, Gap},
		{"duplicate", func(_ *Rule, in *Signal) { in.Sequence = 1 }, Invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := rules()
			s := criticalState()
			in := signal(2, 0)
			at := in.ObservedAt
			tc.change(&r, &in)
			next, event, err := Reduce(s, r, in, at)
			if err != nil || event != nil || next.Active != Critical || next.Quality != tc.quality {
				t.Fatalf("next=%+v event=%+v err=%v", next, event, err)
			}
		})
	}
}

func TestCriticalIsImmediateEvenAfterLoss(t *testing.T) {
	s := State{Key: rules().Key, Incarnation: Incarnation{1}}
	in := signal(1, 95)
	in.Quality = Gap
	next, event, err := Reduce(s, rules(), in, in.ObservedAt)
	if err != nil || event == nil || next.Active != Critical || next.Quality != Gap {
		t.Fatal("loss hid known current critical")
	}
}

func TestUnknownWithoutMetadataStaysMissing(t *testing.T) {
	s := criticalState()
	in := Signal{Key: rules().Key, Quality: Missing}
	next, event, err := Reduce(s, rules(), in, clock().Add(2*time.Second))
	if err != nil || event != nil || next.Active != Critical || next.Quality != Missing {
		t.Fatal("unobserved sample became a known invalid value")
	}
}

func TestCooldownStableTransitionAndDistinctDelivery(t *testing.T) {
	r := rules()
	r.Cooldown = 3 * time.Second
	s := State{Key: r.Key, Incarnation: Incarnation{1}}
	in := signal(1, 99)
	s, first, err := Reduce(s, r, in, in.ObservedAt)
	if err != nil || first == nil {
		t.Fatal(err)
	}
	for seq := uint64(2); seq <= 4; seq++ {
		in = signal(seq, 99)
		next, event, err := Reduce(s, r, in, in.ObservedAt)
		if err != nil {
			t.Fatal(err)
		}
		if seq < 4 && event != nil {
			t.Fatal("repeated state spam")
		}
		if seq == 4 && (event == nil || event.Transition != first.Transition || event.ID == first.ID || event.OccurredAt != first.OccurredAt) {
			t.Fatal("cooldown identity changed")
		}
		s = next
	}
}

func TestRuleValidationAndCounterExhaustion(t *testing.T) {
	for _, change := range []func(*Rule){func(r *Rule) { r.Stop = r.Warn }, func(r *Rule) { r.Clear = r.Warn }, func(r *Rule) { r.WarnCount = 1 }, func(r *Rule) { r.ClearCount = 101 }, func(r *Rule) { r.MaxAge = 0 }, func(r *Rule) { r.WarnDwell = -1 }} {
		r := rules()
		change(&r)
		s := criticalState()
		next, event, err := Reduce(s, r, signal(2, 0), signal(2, 0).ObservedAt)
		if err == nil || event != nil || next != s {
			t.Fatal("invalid policy mutated state")
		}
	}
	s := criticalState()
	s.Revision = math.MaxUint64
	in := signal(2, 0)
	s, _, _ = Reduce(s, rules(), in, in.ObservedAt)
	in = signal(3, 0)
	next, event, err := Reduce(s, rules(), in, in.ObservedAt)
	if err != ErrOverflow || event != nil || next != s {
		t.Fatal("revision wrapped or incident cleared")
	}
}
