package nativealerts

import (
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/nativeobserve"
)

func rules() Rule {
	return Rule{Key: Key{Resource: nativeobserve.ResourceID{1}, Metric: nativeobserve.DatabaseBytes, Kind: Capacity}, Policy: nativeobserve.Digest{1}, Mode: High, Warn: 80, Stop: 95, Clear: 70, WarnCount: 2, ClearCount: 2, WarnDwell: time.Second, ClearDwell: time.Second, MaxAge: time.Minute}
}
func clock() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }
func signal(seq uint64, value uint64) Signal {
	return Signal{Key: rules().Key, Binding: ID{1}, Run: nativeobserve.RunID{1}, Sequence: seq, ObservedAt: clock().Add(time.Duration(seq) * time.Second), Value: nativeobserve.Number{Present: true, Value: value}, Quality: Good}
}
func criticalState() State {
	return State{Key: rules().Key, Incarnation: Incarnation{1}, Active: Critical, Quality: Good, Revision: 1, TransitionAt: clock(), LastNoticeAt: clock(), Binding: ID{1}, Policy: ruleID(rules()), Run: nativeobserve.RunID{1}, Sequence: 1, ObservedAt: signal(1, 99).ObservedAt}
}

func TestUnknownNeverResolvesCritical(t *testing.T) {
	s := criticalState()
	in := signal(2, 0)
	in.Value = nativeobserve.Number{}
	in.Quality = Missing
	next, event, err := Reduce(s, rules(), in, in.ObservedAt)
	if err != nil || event != nil || next.Active != Critical {
		t.Fatalf("unknown resolved incident: %+v %+v %v", next, event, err)
	}
}
func TestGapNeverResolvesCritical(t *testing.T) {
	s := criticalState()
	in := signal(5, 0)
	next, event, err := Reduce(s, rules(), in, in.ObservedAt)
	if err != nil || event != nil || next.Active != Critical {
		t.Fatalf("gap resolved incident: %+v %+v %v", next, event, err)
	}
}

func TestDifferentResourceCannotResolveCritical(t *testing.T) {
	s := criticalState()
	for _, seq := range []uint64{2, 3} {
		in := signal(seq, 0)
		in.Key.Resource[0] = 2
		next, event, err := Reduce(s, rules(), in, in.ObservedAt)
		if err != nil || event != nil || next.Active != Critical {
			t.Fatal("another resource resolved this critical")
		}
		s = next
	}
}
