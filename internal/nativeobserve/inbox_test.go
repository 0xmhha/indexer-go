package nativeobserve

import (
	"sync"
	"testing"
	"time"
)

func TestOwnerInboxBoundedLossRetirementAndOriginalTime(t *testing.T) {
	i, e := NewInbox(1)
	if e != nil {
		t.Fatal(e)
	}
	b := databaseBinding()
	bindings := []Binding{b}
	tap, e := i.Bind(bindings)
	if e != nil {
		t.Fatal(e)
	}
	bindings[0].Source.Incarnation++
	o := sample(b, clock().Add(-time.Minute))
	o.EventAt = clock().Add(-time.Hour)
	if !tap.Offer(DatabaseBytes, o) {
		t.Fatal("first owner result rejected")
	}
	if tap.Offer(DatabaseBytes, o) || !i.Lost() {
		t.Fatal("full inbox did not preserve loss")
	}
	got, ok := i.Poll()
	if !ok || got.Sequence != 1 || got.Binding != b || !got.AcquiredAt.Equal(o.AcquiredAt) || !got.EventAt.Equal(o.EventAt) {
		t.Fatal("result time/binding changed")
	}
	if !tap.Offer(DatabaseBytes, o) {
		t.Fatal("owner blocked after overflow")
	}
	got, ok = i.Poll()
	if !ok || got.Sequence != 3 {
		t.Fatal("overflow hid the per-key sequence gap")
	}
	if !tap.Offer(DatabaseBytes, o) {
		t.Fatal("offer failed")
	}
	tap.Retire()
	if _, ok = i.Poll(); ok {
		t.Fatal("queued detached source remained current")
	}
	if tap.Offer(DatabaseBytes, o) {
		t.Fatal("retired source accepted")
	}
	for _, n := range []int{0, MaxKeys + 1} {
		if _, e := NewInbox(n); e == nil {
			t.Fatal("invalid bound admitted")
		}
	}
}

func TestOwnerInboxConcurrentOfferAndRetire(t *testing.T) {
	i, _ := NewInbox(2)
	tap, e := i.Bind([]Binding{databaseBinding()})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 3 {
		wg.Go(func() {
			<-start
			for range 20 {
				tap.Offer(DatabaseBytes, sample(databaseBinding(), clock()))
			}
		})
	}
	close(start)
	tap.Retire()
	wg.Wait()
	if _, ok := i.Poll(); ok {
		t.Fatal("retired queued result leaked")
	}
}

func TestACKProgressSeedDuplicateAndReset(t *testing.T) {
	var c ACKClock
	seed := c.Observe(100, Hash{1}, 1, clock())
	if !seed.IsZero() {
		t.Fatal("restored ACK gained a progress time")
	}
	next := clock().Add(time.Second)
	if !c.Observe(101, Hash{2}, 1, next).Equal(next) {
		t.Fatal("new durable advance missing")
	}
	if !c.Observe(101, Hash{2}, 1, clock().Add(time.Hour)).Equal(next) {
		t.Fatal("duplicate refreshed progress")
	}
	if !c.Observe(100, Hash{3}, 2, clock()).IsZero() {
		t.Fatal("reorg/reset became fresh progress")
	}
}
