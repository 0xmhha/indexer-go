package fetch

import (
	"context"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/stream"
)

// Events of indexed blocks go through the storage's outbox (refactoring
// plan R3-1, defect D8): the writer records them in the block's transaction,
// so they exist exactly when the block does, and a relay delivers them to
// the event bus in sequence order after the commit. A storage without an
// outbox gets the events published directly after the commit, as before.

// outboxState is the fetcher's outbox and relay.
type outboxState struct {
	store  port.Outbox
	relay  *stream.Relay
	once   sync.Once
	cancel context.CancelFunc
	done   chan struct{}
}

// initOutbox uses the storage's outbox when it has one and blocks are
// indexed in transactions. retain is how many delivered entries the relay
// keeps (0 keeps all).
func (f *Fetcher) initOutbox(retain uint64) {
	ob, ok := f.storage.(port.Outbox)
	if !ok || f.txr == nil {
		return
	}
	f.outbox = &outboxState{store: ob}
	if f.eventBus != nil {
		f.outbox.relay = stream.NewRelay(ob, f.eventBus.Publish, stream.RelayConfig{Retain: retain}, f.logger)
	}
}

// recordEvents adds the events of the block being written to the outbox in
// its transaction (txCtx). An event that cannot be encoded is logged and
// left out; it does not stop the block.
func (f *Fetcher) recordEvents(txCtx context.Context, evs []events.Event) error {
	entries := make([]port.OutboxEntry, 0, len(evs))
	for _, ev := range evs {
		data, err := events.MarshalEvent(ev)
		if err != nil {
			f.logger.Error("Event left out of the outbox", zap.String("type", string(ev.Type())), zap.Error(err))
			continue
		}
		entries = append(entries, port.OutboxEntry{Type: string(ev.Type()), Data: data})
	}
	return f.outbox.store.AppendOutbox(txCtx, entries)
}

// committed delivers the events of a committed write: it wakes the relay,
// or publishes them directly when the storage has no outbox.
func (f *Fetcher) committed(evs []events.Event, height uint64) {
	if f.outbox != nil {
		f.notifyRelay()
		return
	}
	for _, ev := range evs {
		if !f.publish(ev) {
			f.logger.Warn("Failed to publish event (channel full)",
				zap.Uint64("height", height),
				zap.String("type", string(ev.Type())),
			)
		}
	}
}

// notifyRelay starts the relay on first use and wakes it.
func (f *Fetcher) notifyRelay() {
	o := f.outbox
	if o == nil || o.relay == nil {
		return
	}
	o.once.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		o.cancel, o.done = cancel, make(chan struct{})
		go func() {
			defer close(o.done)
			for {
				err := o.relay.Run(ctx)
				if ctx.Err() != nil {
					return
				}
				f.logger.Error("Event relay stopped; restarting", zap.Error(err))
				if sleepCtx(ctx, time.Second) != nil {
					return
				}
			}
		}()
	})
	o.relay.Notify()
}

// StartRelay starts delivering the outbox, including entries committed
// before this process started. Indexing starts it on the first commit.
func (f *Fetcher) StartRelay() { f.notifyRelay() }

// stopRelay stops the relay and waits for it. Entries it did not deliver
// stay in the outbox for the next start.
func (f *Fetcher) stopRelay() {
	o := f.outbox
	if o == nil {
		return
	}
	o.once.Do(func() {}) // never start a relay just to stop it
	if o.cancel != nil {
		o.cancel()
		<-o.done
	}
}

// rollbackHook returns the undo hook that records a reorganization's
// events in the outbox with each rolled-back block, or nil when the storage
// has no outbox.
func (f *Fetcher) rollbackHook() port.UndoHook {
	if f.outbox == nil {
		return nil
	}
	return func(txCtx context.Context, r *port.Reorg, ob *port.OrphanedBlock, first bool) error {
		return f.recordEvents(txCtx, reorgEvents(r, ob, first))
	}
}

// reorgEvents returns the events announcing that block ob was rolled back
// in reorganization r: the reorganization first (with the newest block),
// then every log of the block with Removed set, in reverse order, as
// go-ethereum's log subscriptions report them.
func reorgEvents(r *port.Reorg, ob *port.OrphanedBlock, first bool) []events.Event {
	var out []events.Event
	if first {
		ev := &events.ReorgEvent{
			Seq: r.Seq, ForkNumber: r.ForkNumber, ForkHash: r.ForkHash, OldHead: r.OldHead, CreatedAt: time.Now(),
		}
		for _, b := range r.Removed {
			ev.Removed = append(ev.Removed, events.BlockRef{Number: b.Number, Hash: b.Hash})
		}
		out = append(out, ev)
	}
	for i := len(ob.Receipts) - 1; i >= 0; i-- {
		logs := ob.Receipts[i].Logs
		for j := len(logs) - 1; j >= 0; j-- {
			l := logs[j]
			out = append(out, events.NewLogEvent(&types.Log{
				Address: l.Address, Topics: l.Topics, Data: l.Data,
				BlockNumber: l.BlockNumber, BlockHash: l.BlockHash, TxHash: l.TxHash,
				TxIndex: l.TxIndex, Index: l.Index, Removed: true,
			}))
		}
	}
	return out
}
