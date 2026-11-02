package nativeobserve

import (
	"errors"
	"sync/atomic"
)

// Inbox accepts value-only owner results. It performs no IO or callback. Loss is
// sticky for this inbox lifetime; it cannot be mistaken for a recovered resource.
type Inbox struct {
	entries chan queuedObservation
	lost    atomic.Bool
}
type queuedObservation struct {
	value Observation
	tap   *Tap
}

// Tap is one source attachment, permanently fenced by Retire. It never owns the
// source, a DB lease, ACK or a background worker. Producers must not retain IO.
type Tap struct {
	inbox    *Inbox
	bindings []Binding
	sequence []atomic.Uint64
	active   atomic.Bool
}

func NewInbox(size int) (*Inbox, error) {
	if size < 1 || size > MaxKeys {
		return nil, errors.New("observation: invalid inbox bound")
	}
	return &Inbox{entries: make(chan queuedObservation, size)}, nil
}
func (i *Inbox) Bind(bindings []Binding) (*Tap, error) {
	if i == nil {
		return nil, errors.New("observation: missing inbox")
	}
	c, a := NewCatalog(bindings)
	if !a.Usable() {
		return nil, errors.New("observation: invalid bindings")
	}
	seen := map[Metric]bool{}
	for _, b := range bindings {
		if b.Source != bindings[0].Source || seen[b.Metric] {
			return nil, errors.New("observation: ambiguous tap source or metric")
		}
		seen[b.Metric] = true
	}
	t := &Tap{inbox: i, bindings: c.bindings, sequence: make([]atomic.Uint64, len(bindings))}
	t.active.Store(true)
	return t, nil
}

// Offer stamps the declared binding and a per-key sequence; it preserves all
// original value/timing/coverage fields. False is never a source failure/ACK.
func (t *Tap) Offer(metric Metric, value Observation) bool {
	if t == nil || !t.active.Load() {
		return false
	}
	for n, b := range t.bindings {
		if b.Metric != metric {
			continue
		}
		value.Binding = b
		value.Sequence = t.sequence[n].Add(1)
		if value.Sequence == 0 {
			t.inbox.lost.Store(true)
			t.Retire()
			return false
		}
		select {
		case t.inbox.entries <- queuedObservation{value, t}:
			return true
		default:
			t.inbox.lost.Store(true)
			return false
		}
	}
	return false
}
func (t *Tap) Retire() {
	if t != nil {
		t.active.Store(false)
	}
}
func (i *Inbox) Lost() bool { return i != nil && i.lost.Load() }

// Poll is bounded even if retired producers race the consumer. Queued results
// of detached sources are discarded; timestamps are never refreshed on dequeue.
func (i *Inbox) Poll() (Observation, bool) {
	if i == nil {
		return Observation{}, false
	}
	for range cap(i.entries) {
		select {
		case e := <-i.entries:
			if e.tap.active.Load() {
				return e.value, true
			}
			i.lost.Store(true)
		default:
			return Observation{}, false
		}
	}
	return Observation{}, false
}
