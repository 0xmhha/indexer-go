// Package stream delivers a chain's change stream (refactoring plan Phase
// 3). The relay reads the outbox, which the indexer writes in each block's
// transaction, and hands the events to a bus in sequence order.
package stream

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// Defaults of RelayConfig.
const (
	DefaultRelayName  = "relay"
	DefaultRelayBatch = 512
	DefaultRelayPoll  = time.Second
)

// RelayConfig configures a Relay.
type RelayConfig struct {
	// Name identifies the relay's cursor in the outbox.
	Name string
	// Batch is how many entries are read at a time.
	Batch int
	// Poll is how long the relay waits for new entries when nobody calls
	// Notify; Notify wakes it at once.
	Poll time.Duration
	// Retain is how many delivered entries stay in the outbox (for
	// consumers that ask for missed events); 0 keeps every entry.
	Retain uint64
}

// Relay delivers outbox entries to publish, in sequence order (R3-1).
//
// Delivery is at least once: the relay records how far it delivered after
// each batch, so after a crash it delivers the entries since the last
// record again. Every event carries its sequence (events.Stream), and
// consumers drop the ones they have seen (events.EventBus does). Entries are
// never skipped: when publish refuses an event (a full buffer) the relay
// waits and offers it again.
type Relay struct {
	outbox  port.Outbox
	publish func(events.Event) bool
	cfg     RelayConfig
	logger  *zap.Logger
	wake    chan struct{}

	prunedTo uint64 // entries below were pruned in this run
}

// NewRelay returns a relay from outbox to publish.
func NewRelay(outbox port.Outbox, publish func(events.Event) bool, cfg RelayConfig, logger *zap.Logger) *Relay {
	if cfg.Name == "" {
		cfg.Name = DefaultRelayName
	}
	if cfg.Batch <= 0 {
		cfg.Batch = DefaultRelayBatch
	}
	if cfg.Poll <= 0 {
		cfg.Poll = DefaultRelayPoll
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Relay{outbox: outbox, publish: publish, cfg: cfg, logger: logger, wake: make(chan struct{}, 1)}
}

// Notify tells the relay that entries were committed. It never blocks.
func (r *Relay) Notify() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run delivers entries until ctx ends. It returns ctx.Err() then, or the
// first storage error.
func (r *Relay) Run(ctx context.Context) error {
	cursor, err := r.outbox.OutboxCursor(ctx, r.cfg.Name)
	if err != nil {
		return fmt.Errorf("relay: read cursor: %w", err)
	}
	poll := time.NewTicker(r.cfg.Poll)
	defer poll.Stop()
	for {
		next, err := r.deliverBatch(ctx, cursor)
		if err != nil {
			return err
		}
		if next != cursor {
			cursor = next
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.wake:
		case <-poll.C:
		}
	}
}

// deliverBatch delivers one batch of entries after cursor, records the new
// cursor and prunes, and returns the new cursor.
func (r *Relay) deliverBatch(ctx context.Context, cursor uint64) (uint64, error) {
	entries, err := r.outbox.ReadOutbox(ctx, cursor, r.cfg.Batch)
	if err != nil {
		return cursor, fmt.Errorf("relay: read outbox after %d: %w", cursor, err)
	}
	if len(entries) == 0 {
		return cursor, nil
	}
	for _, e := range entries {
		if cursor != 0 && e.Seq != cursor+1 {
			// Only entries that were never delivered by this relay can be
			// missing (deleted by a reindex), so nothing is lost here.
			r.logger.Warn("Outbox sequence jumps", zap.Uint64("after", cursor), zap.Uint64("next", e.Seq))
		}
		ev, err := events.UnmarshalEvent(events.EventType(e.Type), e.Data)
		if err != nil {
			// The entry was written by a build that knows the type and this
			// one does not; the sequence stays visible as a gap.
			r.logger.Error("Undecodable outbox entry skipped", zap.Uint64("seq", e.Seq), zap.String("type", e.Type), zap.Error(err))
		} else {
			if s, ok := ev.(events.Sequenced); ok {
				s.SetSequence(e.Seq)
			}
			if err := r.offer(ctx, ev); err != nil {
				return cursor, err
			}
		}
		cursor = e.Seq
	}
	if err := r.outbox.SetOutboxCursor(ctx, r.cfg.Name, cursor); err != nil {
		return cursor, fmt.Errorf("relay: record cursor %d: %w", cursor, err)
	}
	r.prune(ctx, cursor)
	return cursor, nil
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

// pruneStep is how many entries pass between prunes.
const pruneStep = 1024

// prune deletes the delivered entries beyond the retained ones.
func (r *Relay) prune(ctx context.Context, cursor uint64) {
	if r.cfg.Retain == 0 || cursor <= r.cfg.Retain {
		return
	}
	before := cursor - r.cfg.Retain + 1
	if before < r.prunedTo+pruneStep {
		return
	}
	if err := r.outbox.PruneOutbox(ctx, before); err != nil {
		r.logger.Warn("Outbox prune failed", zap.Uint64("before", before), zap.Error(err))
		return
	}
	r.prunedTo = before
}
