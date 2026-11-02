package nativealerts

import (
	"context"
	"slices"
	"time"

	"github.com/0xmhha/indexer-go/internal/nativeobserve"
)

const MaxAttempts = 3

type Delivery struct {
	Event       Event
	Attempts    int
	NextAttempt time.Time
	Dead        bool
}

// Record contains incident metadata only. No sample/time series/sequence/dwell,
// process run, original block payload or source acknowledgement is persisted.
type Record struct {
	Key                        Key
	Incarnation                Incarnation
	Active                     Level
	Revision, Reminder         uint64
	TransitionAt, LastNoticeAt time.Time
	Deferred                   *Event
}
type Snapshot struct {
	Version    int
	Records    []Record
	Deliveries []Delivery
}
type entry struct {
	state    State
	deferred *Event
}
type Engine struct {
	rules                         []Rule
	entries                       []entry
	queue                         []Delivery
	loadFailed, persistenceFailed bool
	initialized                   bool
}
type Store interface {
	Read(context.Context) (Snapshot, error)
	Write(context.Context, Snapshot) error
}
type Sink interface {
	Enabled() bool
	Send(context.Context, Event) (Receipt, error)
}

func New(rules []Rule, incarnation Incarnation) (*Engine, error) {
	if len(rules) < 1 || len(rules) > MaxRules || incarnation == (Incarnation{}) {
		return nil, ErrRule
	}
	e := &Engine{rules: slices.Clone(rules)}
	seen := map[Key]bool{}
	resources := map[nativeobserve.ResourceID]bool{}
	for _, r := range rules {
		if r.Validate() != nil || seen[r.Key] {
			return nil, ErrRule
		}
		seen[r.Key] = true
		resources[r.Key.Resource] = true
		e.entries = append(e.entries, entry{state: State{Key: r.Key, Incarnation: incarnation, Quality: Missing}})
	}
	if len(resources) > nativeobserve.MaxResources {
		return nil, ErrRule
	}
	return e, nil
}
func (e *Engine) State(key Key) (State, bool) {
	for _, v := range e.entries {
		if v.state.Key == key {
			return v.state, true
		}
	}
	return State{}, false
}
func (e *Engine) PersistenceFailed() bool { return e.persistenceFailed || e.loadFailed }
func (e *Engine) flushDeferred() {
	for n := range e.entries {
		v := &e.entries[n]
		if v.deferred != nil && len(e.queue) < MaxQueued {
			e.queue = append(e.queue, Delivery{Event: *v.deferred})
			v.deferred = nil
		}
	}
}

func (e *Engine) Apply(key Key, in Signal, at time.Time) error {
	if e.loadFailed {
		return ErrCheckpoint
	}
	e.flushDeferred()
	for n, r := range e.rules {
		if r.Key != key {
			continue
		}
		e.initialized = true
		v := &e.entries[n]
		if v.deferred != nil {
			v.state.Quality = Backpressured
			resetPending(&v.state)
			return ErrBackpressure
		}
		old := v.state
		next, event, err := Reduce(old, r, in, at)
		if err != nil {
			return err
		}
		if event == nil {
			v.state = next
			return nil
		}
		if event.Reminder > 0 {
			for _, d := range e.queue {
				if d.Event.Transition == event.Transition {
					next.Reminder, next.LastNoticeAt = old.Reminder, old.LastNoticeAt
					v.state = next
					return nil
				}
			}
		}
		if len(e.queue) < MaxQueued {
			e.queue = append(e.queue, Delivery{Event: *event})
			v.state = next
			return nil
		}
		if event.Level == Critical && event.Reminder == 0 {
			next.Quality = Backpressured
			v.state = next
			v.deferred = event
		} else {
			old.Quality = Backpressured
			resetPending(&old)
			old.Binding, old.Policy, old.Run, old.Sequence, old.ObservedAt = next.Binding, next.Policy, next.Run, next.Sequence, next.ObservedAt
			v.state = old
		}
		return ErrBackpressure
	}
	return ErrRule
}

func (e *Engine) Snapshot() Snapshot {
	s := Snapshot{Version: 1, Deliveries: slices.Clone(e.queue)}
	for _, v := range e.entries {
		st := v.state
		r := Record{Key: st.Key, Incarnation: st.Incarnation, Active: st.Active, Revision: st.Revision, Reminder: st.Reminder, TransitionAt: st.TransitionAt, LastNoticeAt: st.LastNoticeAt}
		if v.deferred != nil {
			event := *v.deferred
			r.Deferred = &event
		}
		s.Records = append(s.Records, r)
	}
	return s
}

func (e *Engine) loseState() {
	e.loadFailed = true
	for n := range e.entries {
		e.entries[n].state.Quality = StateLost
		resetPending(&e.entries[n].state)
	}
}
func (e *Engine) Restore(s Snapshot) error {
	if e.initialized || validateSnapshot(s) != nil || len(s.Records) != len(e.rules) {
		e.loseState()
		return ErrCheckpoint
	}
	byKey := map[Key]Record{}
	for _, r := range s.Records {
		byKey[r.Key] = r
	}
	next := make([]entry, len(e.rules))
	for n, r := range e.rules {
		v, ok := byKey[r.Key]
		if !ok {
			e.loseState()
			return ErrCheckpoint
		}
		st := stateFrom(v)
		st.Quality = Reset
		next[n].state = st
		if v.Deferred != nil {
			event := *v.Deferred
			next[n].deferred = &event
		}
	}
	e.entries, e.queue = next, slices.Clone(s.Deliveries)
	e.loadFailed, e.persistenceFailed = false, false
	e.initialized = true
	return nil
}
func (e *Engine) Load(ctx context.Context, store Store) error {
	if store == nil {
		e.loseState()
		return ErrCheckpoint
	}
	s, err := store.Read(ctx)
	if err != nil {
		e.loseState()
		return ErrCheckpoint
	}
	return e.Restore(s)
}
func (e *Engine) Persist(ctx context.Context, store Store) error {
	if e.loadFailed || store == nil || ctx.Err() != nil {
		e.persistenceFailed = true
		return ErrCheckpoint
	}
	snapshot := e.Snapshot()
	if validateSnapshot(snapshot) != nil || store.Write(ctx, snapshot) != nil {
		e.persistenceFailed = true
		return ErrCheckpoint
	}
	e.persistenceFailed = false
	return nil
}

// Dispatch performs one explicit caller-owned delivery. Persist the attempt
// reservation before IO; an exact delivery receipt alone permits removal.
// Failed post-receipt persistence restores the durable attempt for replay with
// the same ID. Dead letters stay bounded and block later messages of that key.
func (e *Engine) Dispatch(ctx context.Context, at time.Time, sink Sink, store Store) error {
	if sink == nil || !sink.Enabled() {
		return ErrDisabled
	}
	if at.IsZero() || ctx.Err() != nil {
		return ErrDelivery
	}
	if e.loadFailed {
		return ErrCheckpoint
	}
	e.flushDeferred()
	seen := map[Key]bool{}
	for n, d := range e.queue {
		if seen[d.Event.Key] {
			continue
		}
		seen[d.Event.Key] = true
		if d.Dead || d.NextAttempt.After(at) {
			continue
		}
		if d.Attempts >= MaxAttempts {
			e.queue[n].Dead = true
			return e.Persist(ctx, store)
		}
		e.queue[n].Attempts++
		e.queue[n].NextAttempt = at.Add(time.Second * time.Duration(1<<(e.queue[n].Attempts-1)))
		if err := e.Persist(ctx, store); err != nil {
			e.queue[n] = d
			return err
		}
		reserved := slices.Clone(e.queue)
		receipt, sendErr := sink.Send(ctx, d.Event)
		success := sendErr == nil && ctx.Err() == nil && receipt.ID == d.Event.ID
		if success {
			e.queue = append(e.queue[:n], e.queue[n+1:]...)
		} else if e.queue[n].Attempts >= MaxAttempts {
			e.queue[n].Dead = true
		}
		if err := e.Persist(ctx, store); err != nil {
			e.queue = reserved
			return err
		}
		if !success {
			return ErrDelivery
		}
		return nil
	}
	return nil
}
