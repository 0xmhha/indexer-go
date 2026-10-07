// Package streamtest checks a stream.Bus implementation against the bus
// contract (refactoring plan R3-2): every consumer group receives every
// entry in sequence order, groups do not share progress, and a group
// resumes after the last batch it accepted, in another bus instance (a
// restarted or another node) too. Run it from the implementation's tests.
package streamtest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/stream"
)

// Harness is one empty stream under test.
type Harness struct {
	// Append commits one transaction with an entry per value (as Data, in
	// order) and wakes the stream's consumers.
	Append func(t *testing.T, values ...string)
	// NewBus returns a bus over the stream, as another process (a node, or
	// the same node after a restart) opens it. Positions recorded through
	// one bus are seen by the others.
	NewBus func(t *testing.T) stream.Bus
}

// NewHarness returns a harness over a new, empty stream.
type NewHarness func(t *testing.T) Harness

// wait is how long a consumer may take to receive what it should.
const wait = 10 * time.Second

// Run checks the contract.
func Run(t *testing.T, newHarness NewHarness) {
	ctx := context.Background()

	t.Run("EveryGroupReceivesEveryEntry", func(t *testing.T) {
		h := newHarness(t)
		h.Append(t, values(1, 3)...) // before the groups join
		groups := []string{"node-a", "node-b", "node-c"}
		got := make([]*recorder, len(groups))
		for i, g := range groups {
			bus := h.NewBus(t) // a node each
			_, err := bus.Join(ctx, g, stream.StartEarliest)
			require.NoError(t, err)
			got[i] = consume(t, bus, g, nil)
		}
		for i := 4; i <= 40; i += 4 {
			h.Append(t, values(i, i+3)...)
		}
		for i, r := range got {
			r.await(t, 43)
			assert.Equal(t, seqs(1, 43), r.seqs(), "group %s receives every entry once, in order", groups[i])
			r.stop(t)
		}
	})

	t.Run("GroupsKeepTheirOwnPositions", func(t *testing.T) {
		h := newHarness(t)
		h.Append(t, values(1, 10)...)
		bus := h.NewBus(t)
		_, err := bus.Join(ctx, "fast", stream.StartEarliest)
		require.NoError(t, err)
		_, err = bus.Join(ctx, "slow", stream.StartEarliest)
		require.NoError(t, err)
		fast := consume(t, bus, "fast", nil)
		fast.await(t, 10)
		fast.stop(t)

		pos, err := h.NewBus(t).Join(ctx, "slow", stream.StartLatest)
		require.NoError(t, err)
		assert.Zero(t, pos, "another group's progress does not move this one")
		pos, err = h.NewBus(t).Join(ctx, "fast", stream.StartEarliest)
		require.NoError(t, err)
		assert.Equal(t, uint64(10), pos, "a joined group keeps its position; start applies to new groups")

		slow := consume(t, h.NewBus(t), "slow", nil)
		slow.await(t, 10)
		assert.Equal(t, seqs(1, 10), slow.seqs())
		slow.stop(t)
	})

	t.Run("NewGroupStarts", func(t *testing.T) {
		h := newHarness(t)
		h.Append(t, values(1, 5)...)
		bus := h.NewBus(t)
		pos, err := bus.Join(ctx, "latest", stream.StartLatest)
		require.NoError(t, err)
		assert.Equal(t, uint64(5), pos)
		pos, err = bus.Join(ctx, "earliest", stream.StartEarliest)
		require.NoError(t, err)
		assert.Zero(t, pos)

		latest := consume(t, bus, "latest", nil)
		h.Append(t, values(6, 8)...)
		latest.await(t, 8)
		assert.Equal(t, seqs(6, 8), latest.seqs(), "StartLatest receives what is committed after joining")
		latest.stop(t)

		// A group that never joined joins at StartLatest when it starts
		// consuming: it receives none of the entries committed before, and
		// every entry from the first it receives.
		unjoined := consume(t, h.NewBus(t), "unjoined", nil)
		n := 9
		require.Eventually(t, func() bool {
			h.Append(t, values(n, n)...)
			n++
			return len(unjoined.seqs()) > 0
		}, wait, 20*time.Millisecond)
		last := uint64(n - 1)
		unjoined.await(t, last)
		got := unjoined.seqs()
		assert.Greater(t, got[0], uint64(8), "entries committed before it started are not delivered")
		assert.Equal(t, seqs(got[0], last), got)
		unjoined.stop(t)
	})

	t.Run("ResumesAfterFailedBatch", func(t *testing.T) {
		h := newHarness(t)
		_, err := h.NewBus(t).Join(ctx, "g", stream.StartEarliest)
		require.NoError(t, err)
		h.Append(t, values(1, 5)...)

		// The first consumer fails on entry 3 after accepting batches up
		// to entry 2 at most, and returns the handler's error.
		boom := errors.New("handler failed")
		var seen []uint64
		var accepted uint64
		err = h.NewBus(t).Consume(ctx, "g", func(_ context.Context, batch []port.OutboxEntry) error {
			for _, e := range batch {
				if e.Seq == 3 {
					return boom
				}
				seen = append(seen, e.Seq)
			}
			accepted = batch[len(batch)-1].Seq
			return nil
		})
		require.ErrorIs(t, err, boom)
		require.Equal(t, seqs(1, 2), seen)

		// The next consumer, in another bus, starts after the last
		// accepted batch: entries of the failed batch come again, nothing
		// is lost.
		h.Append(t, values(6, 7)...)
		next := consume(t, h.NewBus(t), "g", nil)
		next.await(t, 7)
		got := next.seqs()
		require.NotEmpty(t, got)
		assert.Equal(t, accepted+1, got[0], "resumes after the last accepted batch")
		assert.Equal(t, seqs(accepted+1, 7), got)
		next.stop(t)
	})

	t.Run("ResumesAfterStop", func(t *testing.T) {
		h := newHarness(t)
		_, err := h.NewBus(t).Join(ctx, "g", stream.StartEarliest)
		require.NoError(t, err)
		h.Append(t, values(1, 4)...)
		first := consume(t, h.NewBus(t), "g", nil)
		first.await(t, 4)
		first.stop(t)

		h.Append(t, values(5, 9)...)
		second := consume(t, h.NewBus(t), "g", nil)
		second.await(t, 9)
		assert.Equal(t, seqs(5, 9), second.seqs(), "accepted entries are not delivered again")
		second.stop(t)
	})

	t.Run("OneConsumerPerGroup", func(t *testing.T) {
		h := newHarness(t)
		bus := h.NewBus(t)
		_, err := bus.Join(ctx, "g", stream.StartEarliest)
		require.NoError(t, err)
		h.Append(t, values(1, 1)...)
		r := consume(t, bus, "g", nil)
		r.await(t, 1) // the consumer runs
		second, cancel := context.WithTimeout(ctx, wait)
		defer cancel()
		err = bus.Consume(second, "g", func(context.Context, []port.OutboxEntry) error { return nil })
		require.ErrorIs(t, err, stream.ErrGroupBusy)
		r.stop(t)
	})

	t.Run("ConsumeEndsWithContext", func(t *testing.T) {
		h := newHarness(t)
		r := consume(t, h.NewBus(t), "g", nil)
		r.stop(t) // requires context.Canceled
	})
}

// values returns "e<from>" ... "e<to>": appended to an empty stream, entry
// n carries "e<n>".
func values(from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("e%d", i))
	}
	return out
}

func seqs(from, to uint64) []uint64 {
	var out []uint64
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

// recorder consumes a group and records what it receives.
type recorder struct {
	mu     sync.Mutex
	got    []port.OutboxEntry
	cancel context.CancelFunc
	done   chan error
}

// consume starts consuming group from bus; onBatch, if set, runs first for
// each batch.
func consume(t *testing.T, bus stream.Bus, group string, onBatch stream.Handler) *recorder {
	ctx, cancel := context.WithCancel(context.Background())
	r := &recorder{cancel: cancel, done: make(chan error, 1)}
	go func() {
		r.done <- bus.Consume(ctx, group, func(ctx context.Context, batch []port.OutboxEntry) error {
			if onBatch != nil {
				if err := onBatch(ctx, batch); err != nil {
					return err
				}
			}
			r.mu.Lock()
			r.got = append(r.got, batch...)
			r.mu.Unlock()
			return nil
		})
	}()
	t.Cleanup(cancel)
	return r
}

// await waits until an entry with Seq >= seq was received, and checks that
// each entry carries the value appended with its sequence.
func (r *recorder) await(t *testing.T, seq uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return len(r.got) > 0 && r.got[len(r.got)-1].Seq >= seq
	}, wait, 5*time.Millisecond, "entries up to %d", seq)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.got {
		require.Equal(t, fmt.Sprintf("e%d", e.Seq), string(e.Data), "entry %d carries its value", e.Seq)
	}
}

func (r *recorder) seqs() []uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []uint64
	for _, e := range r.got {
		out = append(out, e.Seq)
	}
	return out
}

// stop ends the consumer and requires it to return context.Canceled.
func (r *recorder) stop(t *testing.T) {
	t.Helper()
	r.cancel()
	select {
	case err := <-r.done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(wait):
		t.Fatal("Consume did not return after its context ended")
	}
}
