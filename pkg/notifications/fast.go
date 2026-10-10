package notifications

import (
	"context"
	"fmt"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// Delivery is when a setting's notifications are created.
type Delivery string

const (
	// DeliveryDurable (the default) creates notifications from the change
	// stream after a block is committed: none is missed or created twice.
	DeliveryDurable Delivery = "durable"
	// DeliveryFast creates them when the block is fetched, before it is
	// stored (the fast path): sooner, at least once. A block whose
	// indexing is retried is evaluated again; its notifications keep their
	// ids, so none is created twice, but one already delivered may be
	// delivered again after a restart. Reorganizations are notified as
	// for durable settings.
	DeliveryFast Delivery = "fast"
)

// fastQueueBlocks is how many blocks wait for the fast path; when they
// are more, a block is dropped and counted (FastDropped).
const fastQueueBlocks = 1024

// fastBlock is a block's events waiting for the fast path.
type fastBlock struct {
	height uint64
	events []events.Event
}

// fastState is the fast path's part of the service.
type fastState struct {
	queue    chan fastBlock
	settings atomic.Int64  // enabled fast settings
	dropped  atomic.Uint64 // blocks the queue had no room for
}

func validateDelivery(d Delivery) error {
	switch d {
	case "", DeliveryDurable, DeliveryFast:
		return nil
	}
	return fmt.Errorf("delivery %q: must be %s or %s", d, DeliveryDurable, DeliveryFast)
}

func (st *NotificationSetting) fast() bool { return st.Delivery == DeliveryFast }

// countFast records how many enabled settings are fast; the caller holds
// s.mu.
func (s *NotificationService) countFast() {
	n := 0
	for _, st := range s.settings {
		if st.Enabled && st.fast() {
			n++
		}
	}
	s.fastPath.settings.Store(int64(n))
}

// Active implements fetch.BlockTap: the fetcher builds a block's events
// only while a fast setting is enabled.
func (s *NotificationService) Active() bool { return s.fastPath.settings.Load() > 0 }

// OfferBlock implements fetch.BlockTap: it queues a block's events and
// returns at once. When the queue is full the block is dropped and
// counted, so indexing never waits for notifications.
func (s *NotificationService) OfferBlock(height uint64, evs []events.Event) {
	select {
	case s.fastPath.queue <- fastBlock{height: height, events: evs}:
	default:
		n := s.fastPath.dropped.Add(1)
		s.logger.Warn("fast notifications behind: block dropped from the fast path",
			zap.Uint64("block", height), zap.Uint64("dropped_blocks", n))
	}
}

// FastDropped is how many blocks the fast path dropped because its queue
// was full.
func (s *NotificationService) FastDropped() uint64 { return s.fastPath.dropped.Load() }

// fastProcessor evaluates queued blocks in order until the service stops.
func (s *NotificationService) fastProcessor() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case b := <-s.fastPath.queue:
			if err := s.notifyFast(s.ctx, b.events); err != nil && s.ctx.Err() == nil {
				s.logger.Error("fast notifications of a block failed", zap.Uint64("block", b.height), zap.Error(err))
			}
		}
	}
}

// fastKey identifies an event of a block for notification ids: a
// transaction by hash, a log by block hash and index.
func fastKey(ev events.Event) string {
	switch e := ev.(type) {
	case *events.TransactionEvent:
		return "tx/" + e.Hash.Hex()
	case *events.LogEvent:
		if e.Log != nil {
			return fmt.Sprintf("log/%s/%d", e.Log.BlockHash.Hex(), e.Log.Index)
		}
	}
	return ""
}

// notifyFast creates and queues the notifications of fast settings for a
// block's events.
func (s *NotificationService) notifyFast(ctx context.Context, evs []events.Event) error {
	s.mu.RLock()
	settings := make([]*NotificationSetting, 0, len(s.settings))
	for _, st := range s.settings {
		if st.Enabled && st.fast() {
			settings = append(settings, st)
		}
	}
	s.mu.RUnlock()
	for _, ev := range evs {
		key := fastKey(ev)
		if key == "" {
			continue
		}
		kinds := s.eventKinds(ev)
		for _, setting := range settings {
			if err := s.deliverMatch(ctx, setting, ev, kinds, key); err != nil {
				return err
			}
		}
	}
	return nil
}
