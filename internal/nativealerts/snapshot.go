package nativealerts

import (
	"encoding/json"
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"time"
)

func sameEvent(a, b Event) bool {
	if !a.OccurredAt.Equal(b.OccurredAt) || !a.NoticeAt.Equal(b.NoticeAt) {
		return false
	}
	a.OccurredAt, a.NoticeAt, b.OccurredAt, b.NoticeAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	return a == b
}

func stateFrom(r Record) State {
	return State{Key: r.Key, Incarnation: r.Incarnation, Active: r.Active, Revision: r.Revision, Reminder: r.Reminder, TransitionAt: r.TransitionAt, LastNoticeAt: r.LastNoticeAt}
}
func validEvent(ev Event, inc Incarnation) bool {
	if !validKey(ev.Key) || ev.ID == (ID{}) || ev.Level > Critical || ev.Revision == 0 || ev.OccurredAt.IsZero() || ev.NoticeAt.Before(ev.OccurredAt) {
		return false
	}
	s := State{Key: ev.Key, Incarnation: inc, Active: ev.Level, Revision: ev.Revision, Reminder: ev.Reminder, TransitionAt: ev.OccurredAt, LastNoticeAt: ev.NoticeAt}
	if !sameEvent(ev, eventFor(s)) {
		return false
	}
	data, err := json.Marshal(ev)
	return err == nil && len(data) <= MaxEventBytes
}
func validateSnapshot(s Snapshot) error {
	if s.Version != 1 || len(s.Records) < 1 || len(s.Records) > MaxRules || len(s.Deliveries) > MaxQueued {
		return ErrCheckpoint
	}
	keys := map[Key]Record{}
	resources := map[nativeobserve.ResourceID]bool{}
	ids := map[ID]bool{}
	total := len(s.Deliveries)
	for _, r := range s.Records {
		if _, ok := keys[r.Key]; ok {
			return ErrCheckpoint
		}
		if !validKey(r.Key) || r.Incarnation == (Incarnation{}) || r.Active > Critical {
			return ErrCheckpoint
		}
		if r.Revision == 0 {
			if r.Active != Normal || r.Reminder != 0 || !r.TransitionAt.IsZero() || !r.LastNoticeAt.IsZero() || r.Deferred != nil {
				return ErrCheckpoint
			}
		} else if r.TransitionAt.IsZero() || r.LastNoticeAt.Before(r.TransitionAt) {
			return ErrCheckpoint
		}
		if r.Deferred != nil {
			ev := *r.Deferred
			total++
			if ev.Key != r.Key || ev.Level != Critical || ev.Reminder != 0 || !sameEvent(ev, eventFor(stateFrom(r))) || !validEvent(ev, r.Incarnation) || ids[ev.ID] {
				return ErrCheckpoint
			}
			ids[ev.ID] = true
		}
		keys[r.Key] = r
		resources[r.Key.Resource] = true
	}
	if len(resources) > nativeobserve.MaxResources {
		return ErrCheckpoint
	}
	order := map[Key]Event{}
	for _, d := range s.Deliveries {
		r, ok := keys[d.Event.Key]
		if !ok || !validEvent(d.Event, r.Incarnation) || d.Event.Revision > r.Revision || ids[d.Event.ID] || d.Attempts < 0 || d.Attempts > MaxAttempts ||
			d.Dead && d.Attempts != MaxAttempts || d.Attempts == 0 && !d.NextAttempt.IsZero() || d.Attempts > 0 && d.NextAttempt.Before(d.Event.NoticeAt) {
			return ErrCheckpoint
		}
		if before, ok := order[d.Event.Key]; ok {
			if d.Event.Revision < before.Revision || d.Event.Revision == before.Revision && (d.Event.Reminder <= before.Reminder || d.Event.Transition != before.Transition) {
				return ErrCheckpoint
			}
		}
		if d.Event.Revision == r.Revision && (d.Event.Level != r.Active || !d.Event.OccurredAt.Equal(r.TransitionAt) || d.Event.Reminder > r.Reminder) {
			return ErrCheckpoint
		}
		if d.Event.OccurredAt.After(r.TransitionAt) || d.Event.NoticeAt.After(r.LastNoticeAt) {
			return ErrCheckpoint
		}
		order[d.Event.Key] = d.Event
		ids[d.Event.ID] = true
	}
	if total > MaxDeliveries {
		return ErrCheckpoint
	}
	data, err := json.Marshal(s)
	if err != nil || len(data) > MaxCheckpointBytes {
		return ErrCheckpoint
	}
	return nil
}
