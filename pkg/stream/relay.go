// Package stream delivers a chain's change stream (refactoring plan Phase
// 3). The indexer writes the stream to the outbox in each block's
// transaction; a Bus delivers it to consumer groups, and a Relay hands one
// group's events to an event bus in sequence order.
package stream

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// DefaultGroup is the consumer group of a relay that is given none.
const DefaultGroup = "relay"

// Relay delivers a consumer group's entries to publish, decoded and in
// sequence order (R3-1): it is how a node feeds its event bus from the
// change stream. Each node relays as its own group (R3-2), so every node's
// bus receives every event.
//
// Delivery is at least once (Bus): after a crash the group's last batch is
// delivered again. Every event carries its sequence (events.Stream), and
// consumers drop the ones they have seen (events.EventBus does). Entries are
// never skipped: when publish refuses an event (a full buffer) the relay
// waits and offers it again.
type Relay struct {
	bus     Bus
	group   string
	publish func(events.Event) bool
	logger  *zap.Logger
}

// NewRelay returns a relay of group from bus to publish.
func NewRelay(bus Bus, group string, publish func(events.Event) bool, logger *zap.Logger) *Relay {
	if group == "" {
		group = DefaultGroup
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Relay{bus: bus, group: group, publish: publish, logger: logger}
}

// Group returns the relay's consumer group.
func (r *Relay) Group() string { return r.group }

// Run delivers entries until ctx ends. It returns ctx.Err() then, or the
// first error of the bus.
func (r *Relay) Run(ctx context.Context) error {
	if err := r.bus.Consume(ctx, r.group, r.deliver); err != nil {
		return fmt.Errorf("relay %q: %w", r.group, err)
	}
	return nil
}

// deliver publishes one batch.
func (r *Relay) deliver(ctx context.Context, batch []port.OutboxEntry) error {
	for _, e := range batch {
		ev, err := events.UnmarshalEvent(events.EventType(e.Type), e.Data)
		if err != nil {
			// The entry was written by a build that knows the type and this
			// one does not; the sequence stays visible as a gap.
			r.logger.Error("Undecodable outbox entry skipped", zap.Uint64("seq", e.Seq), zap.String("type", e.Type), zap.Error(err))
			continue
		}
		if s, ok := ev.(events.Sequenced); ok {
			s.SetSequence(e.Seq)
		}
		if err := r.offer(ctx, ev); err != nil {
			return err
		}
	}
	return nil
}

// offer hands ev to publish until it is accepted or ctx ends.
func (r *Relay) offer(ctx context.Context, ev events.Event) error {
	wait := time.Millisecond
	for !r.publish(ev) {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
		if wait < 50*time.Millisecond {
			wait *= 2
		}
	}
	return nil
}
