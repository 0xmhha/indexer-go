package nativealerts

import (
	"math"
	"time"

	"github.com/0xmhha/indexer-go/internal/monitorpredicate"
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
)

func validKey(k Key) bool {
	if k.Resource == (nativeobserve.ResourceID{}) || k.Kind < Capacity || k.Kind > DeliveryFailed {
		return false
	}
	if k.Kind != Capacity {
		return k.Metric == 0
	}
	return k.Metric >= nativeobserve.DatabaseBytes && k.Metric <= nativeobserve.FreeBytes
}
func (r Rule) Validate() error {
	if !validKey(r.Key) || r.Policy == (nativeobserve.Digest{}) || r.Mode < High || r.Mode > BudgetBPS ||
		r.WarnCount < 2 || r.WarnCount > 100 || r.ClearCount < 2 || r.ClearCount > 100 ||
		r.WarnDwell < 0 || r.ClearDwell < 0 || r.WarnDwell > 24*time.Hour || r.ClearDwell > 24*time.Hour ||
		r.MaxAge <= 0 || r.MaxAge > 24*time.Hour || r.Cooldown < 0 || r.Cooldown > 7*24*time.Hour {
		return ErrRule
	}
	if r.Mode == Low {
		if r.Stop >= r.Warn || r.Warn >= r.Clear {
			return ErrRule
		}
	} else if r.Clear >= r.Warn || r.Warn >= r.Stop {
		return ErrRule
	}
	if r.Mode == BudgetBPS && (r.Key.Kind != Capacity || r.Stop > 10000) {
		return ErrRule
	}
	if r.Key.Kind == Capacity && (r.Key.Metric == nativeobserve.FreeBytes) != (r.Mode == Low) {
		return ErrRule
	}
	return nil
}
func grade(r Rule, in Signal) (target Level, clear bool, ok bool) {
	if !in.Value.Present {
		return Normal, false, false
	}
	v := in.Value.Value
	switch r.Mode {
	case BudgetBPS:
		if !in.Limit.Present || in.Limit.Value == 0 {
			return Normal, false, false
		}
		atLeast := func(bps uint64) bool { return monitorpredicate.ProductGE(v, 10000, in.Limit.Value, bps) }
		clear = monitorpredicate.ProductGE(in.Limit.Value, r.Clear, v, 10000)
		if atLeast(r.Stop) {
			return Critical, clear, true
		}
		if atLeast(r.Warn) {
			return Warning, clear, true
		}
	case Low:
		clear = v >= r.Clear
		if v <= r.Stop {
			return Critical, clear, true
		}
		if v <= r.Warn {
			return Warning, clear, true
		}
	default:
		clear = v <= r.Clear
		if v >= r.Stop {
			return Critical, clear, true
		}
		if v >= r.Warn {
			return Warning, clear, true
		}
	}
	return Normal, clear, true
}
func resetPending(s *State) { s.Count = 0; s.Since = time.Time{}; s.Candidate = Normal }

// Reduce borrows all inputs. Invalid configuration leaves the old state intact.
// Unknown/gaps reset dwell without clearing a known unresolved resource level.
func Reduce(s State, r Rule, in Signal, at time.Time) (State, *Event, error) {
	original := s
	if r.Validate() != nil || s.Key != r.Key || s.Incarnation == (Incarnation{}) || s.Active > Critical || s.Quality > Backpressured ||
		s.Count < 0 || s.Count > 100 || s.Candidate > Warning || s.Count == 0 && !s.Since.IsZero() || s.Count > 0 && s.Since.IsZero() ||
		s.Revision == 0 && (s.Active != Normal || s.Reminder != 0 || !s.TransitionAt.IsZero() || !s.LastNoticeAt.IsZero()) {
		return original, nil, ErrRule
	}
	if in.Key != r.Key || at.IsZero() || in.ObservedAt.After(at) || at.Before(s.LastNoticeAt) || !in.ObservedAt.IsZero() && in.ObservedAt.Before(s.TransitionAt) || in.Quality > Backpressured {
		s.Quality = Invalid
		resetPending(&s)
		return s, nil, nil
	}
	if in.ObservedAt.IsZero() || in.Binding == (ID{}) || in.Run == (nativeobserve.RunID{}) || in.Sequence == 0 {
		s.Quality = in.Quality
		if s.Quality == Good {
			s.Quality = Missing
		}
		resetPending(&s)
		return s, nil, nil
	}
	policy := ruleID(r)
	changed := s.Binding != (ID{}) && (s.Binding != in.Binding || s.Run != in.Run || s.Policy != policy)
	restored := s.Binding == (ID{}) && s.Revision > 0
	if !changed && !restored && s.Sequence > 0 && (in.Sequence <= s.Sequence || !in.ObservedAt.After(s.ObservedAt)) {
		s.Quality = Invalid
		resetPending(&s)
		return s, nil, nil
	}
	gap := !changed && !restored && s.Sequence > 0 && (in.Sequence-s.Sequence > 1 || in.ObservedAt.Sub(s.ObservedAt) > r.MaxAge)
	s.Binding, s.Policy, s.Run, s.Sequence, s.ObservedAt = in.Binding, policy, in.Run, in.Sequence, in.ObservedAt
	if changed || restored || gap {
		resetPending(&s)
	}
	s.Quality = in.Quality
	if at.Sub(in.ObservedAt) > r.MaxAge {
		s.Quality = Stale
	}
	if s.Quality != Good && s.Quality != Gap {
		resetPending(&s)
		return s, nil, nil
	}
	target, clear, known := grade(r, in)
	if !known {
		s.Quality = Missing
		resetPending(&s)
		return s, nil, nil
	}
	if changed || restored {
		s.Quality = Reset
	}
	if gap || in.Quality == Gap {
		s.Quality = Gap
	}
	if target == Critical && s.Active != Critical {
		if s.Revision == math.MaxUint64 {
			return original, nil, ErrOverflow
		}
		s.Active = Critical
		s.Revision++
		s.Reminder = 0
		s.TransitionAt = in.ObservedAt
		s.LastNoticeAt = at
		resetPending(&s)
		e := eventFor(s)
		return s, &e, nil
	}
	if s.Quality == Reset || s.Quality == Gap {
		return s, nil, nil
	}
	// Critical can clear only through the separate clear threshold; warning
	// values cannot downgrade it. Values in hysteresis pause all clear progress.
	wanted, eligible := s.Active, false
	count, dwell := r.WarnCount, r.WarnDwell
	if target == Warning && s.Active == Normal {
		wanted, eligible = Warning, true
	}
	if clear && s.Active != Normal {
		wanted, eligible = Normal, true
		count, dwell = r.ClearCount, r.ClearDwell
	}
	if eligible {
		if s.Count == 0 || s.Candidate != wanted {
			s.Candidate = wanted
			s.Count = 1
			s.Since = in.ObservedAt
		} else if s.Count < count {
			s.Count++
		}
		if s.Count >= count && in.ObservedAt.Sub(s.Since) >= dwell {
			if s.Revision == math.MaxUint64 {
				return original, nil, ErrOverflow
			}
			s.Active = wanted
			s.Revision++
			s.Reminder = 0
			s.TransitionAt = in.ObservedAt
			s.LastNoticeAt = at
			resetPending(&s)
			e := eventFor(s)
			return s, &e, nil
		}
	} else {
		resetPending(&s)
	}
	if s.Active != Normal && r.Cooldown > 0 && !s.LastNoticeAt.IsZero() && at.Sub(s.LastNoticeAt) >= r.Cooldown {
		if s.Reminder == math.MaxUint64 {
			return original, nil, ErrOverflow
		}
		s.Reminder++
		s.LastNoticeAt = at
		e := eventFor(s)
		return s, &e, nil
	}
	return s, nil, nil
}
