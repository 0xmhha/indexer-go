package stream

import (
	"context"
	"errors"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// Resynchronization (refactoring plan R3-4). A client that lost its
// connection, or was disconnected as too slow, subscribes again from the
// sequence after the last event it received. The engine reads the events
// from there that are still in the outbox, then continues with live events,
// so the client sees every sequence once: live events that arrive while the
// outbox is read wait in the subscription, and events the outbox already
// gave are dropped by sequence.
//
// A client without a position starts from a snapshot: it reads the
// stream's position (the GraphQL streamSequence query), then the state it
// needs, and subscribes from the position after. Changes committed between
// the two reads may arrive again as events; clients apply events by block
// number and hash, so applying one twice changes nothing.

// Outbox is what subscriptions resume from: the outbox of the chain whose
// events the engine publishes (port.Outbox provides it).
type Outbox interface {
	ReadOutbox(ctx context.Context, after uint64, limit int) ([]port.OutboxEntry, error)
	LastOutboxSeq(ctx context.Context) (uint64, error)
}

type outboxSource struct{ Outbox }

// SetOutbox sets the outbox subscriptions resume from. Without one,
// SubscribeFrom returns ErrNoOutbox.
func (e *Engine) SetOutbox(ob Outbox) {
	if ob == nil {
		e.outbox.Store(nil)
		return
	}
	e.outbox.Store(&outboxSource{ob})
}

// ErrNoOutbox is returned by SubscribeFrom when the engine has no outbox.
var ErrNoOutbox = errors.New("stream: events are not kept; resuming from a sequence is not supported")

// TooOldError is returned by SubscribeFrom when the events from the
// requested sequence were pruned. Oldest is the oldest sequence kept (0 if
// none is); the client starts again from a snapshot.
type TooOldError struct {
	From, Oldest uint64
}

func (e *TooOldError) Error() string {
	return fmt.Sprintf("stream: events from sequence %d are no longer kept (oldest kept: %d)", e.From, e.Oldest)
}

// resumeBatch is how many outbox entries a resuming subscription reads at
// a time.
const resumeBatch = 256

// SubscribeFrom subscribes like Subscribe, delivering the events from
// sequence from: first those in the outbox, then live ones, each sequence
// once and in order. A from beyond the last event starts there. It fails
// with *TooOldError when the outbox no longer holds the events from from.
// The outbox is read in the background; ctx ends that reading early.
func (c *Conn) SubscribeFrom(ctx context.Context, id, topicName string, match func(events.Event) bool, from uint64) error {
	e := c.engine
	src := e.outbox.Load()
	if src == nil {
		return ErrNoOutbox
	}
	if from == 0 {
		from = 1
	}
	e.mu.Lock()
	t, ok := e.topics[topicName]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownTopic, topicName)
	}

	// Register first, so no live event is missed while the outbox is
	// checked and read; live events wait in pending.
	s := &subscription{conn: c, id: id, topic: topicName, match: match, next: from, catching: true}
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	if _, dup := c.subs[id]; dup {
		c.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrDuplicateSub, id)
	}
	c.subs[id] = s
	c.mu.Unlock()
	e.mu.Lock()
	e.add(s, t)
	e.mu.Unlock()

	if err := checkKept(ctx, src, from); err != nil {
		c.Unsubscribe(id)
		return err
	}
	go c.catchUp(ctx, src, s, t, from-1)
	return nil
}

// checkKept fails when the outbox no longer holds the events from from.
func checkKept(ctx context.Context, src Outbox, from uint64) error {
	first, err := src.ReadOutbox(ctx, from-1, 1)
	if err != nil {
		return fmt.Errorf("stream: read outbox: %w", err)
	}
	if len(first) > 0 {
		if first[0].Seq > from {
			return &TooOldError{From: from, Oldest: first[0].Seq}
		}
		return nil
	}
	last, err := src.LastOutboxSeq(ctx)
	if err != nil {
		return fmt.Errorf("stream: read outbox: %w", err)
	}
	if from <= last { // committed, but nothing from there is kept
		return &TooOldError{From: from}
	}
	return nil
}

// catchUp delivers the outbox entries after pos to s, then the live events
// that waited, and switches s to live delivery.
func (c *Conn) catchUp(ctx context.Context, src Outbox, s *subscription, t topic, pos uint64) {
	for {
		batch, err := src.ReadOutbox(ctx, pos, resumeBatch)
		if err != nil {
			if ctx.Err() == nil {
				c.fail(fmt.Errorf("stream: read outbox after %d: %w", pos, err))
			}
			return
		}
		if len(batch) == 0 {
			if c.goLive(s, pos) {
				return
			}
			continue // live events are ahead of what was read: read again
		}
		if batch[0].Seq != pos+1 {
			// Pruned while being read: the client must start again.
			c.fail(&TooOldError{From: pos + 1, Oldest: batch[0].Seq})
			return
		}
		for _, entry := range batch {
			pos = entry.Seq
			if events.EventType(entry.Type) != t.eventType {
				continue
			}
			ev, err := events.UnmarshalEvent(t.eventType, entry.Data)
			if err != nil {
				continue // written by a build that knows more types; the relay logs it
			}
			if sq, ok := ev.(events.Sequenced); ok {
				sq.SetSequence(entry.Seq)
			}
			if s.match != nil && !s.match(ev) {
				continue
			}
			payload, ok := t.encode(ev)
			if !ok {
				continue
			}
			if !c.backfill(ctx, s, Frame{Sub: s.id, Seq: entry.Seq, Payload: payload}) {
				return
			}
		}
		c.mu.Lock()
		gone := c.err != nil || c.subs[s.id] != s
		if s.next <= pos {
			s.next = pos + 1
		}
		c.mu.Unlock()
		if gone {
			return
		}
	}
}

// goLive ends the catch-up of s once its pending live events continue the
// entries read (up to pos): it queues them, dropping the ones the outbox
// gave, and lets later events through, all under the connection's lock so
// that no live event overtakes them. It returns false when the pending
// events start beyond pos+1, that is, when events committed after the last
// read are missing: the caller reads the outbox again.
func (c *Conn) goLive(s *subscription, pos uint64) bool {
	c.mu.Lock()
	if c.err != nil || c.subs[s.id] != s {
		c.mu.Unlock()
		return true // closed or unsubscribed
	}
	if s.next <= pos {
		s.next = pos + 1
	}
	for _, f := range s.pending {
		if f.Seq == 0 {
			continue
		}
		if f.Seq > s.next {
			c.mu.Unlock()
			return false
		}
		break
	}
	var slow *SlowError
	for _, f := range s.pending {
		if f.Seq != 0 && f.Seq < s.next {
			continue // delivered from the outbox
		}
		if len(c.queue) >= c.engine.cfg.Buffer {
			slow = &SlowError{ResumeFrom: s.next}
			break
		}
		c.queue = append(c.queue, f)
		if f.Seq != 0 {
			s.next = f.Seq + 1
		}
	}
	s.pending, s.catching = nil, false
	c.mu.Unlock()
	if slow != nil {
		c.engine.disconnects.Add(1)
		c.fail(slow)
		return true
	}
	select {
	case c.ready <- struct{}{}:
	default:
	}
	return true
}

// backfill queues a frame read from the outbox, waiting for the writer to
// make room instead of failing the connection: reading the outbox is not
// the client falling behind. It returns false when the connection ended.
func (c *Conn) backfill(ctx context.Context, s *subscription, f Frame) bool {
	for {
		c.mu.Lock()
		if c.err != nil || c.subs[s.id] != s {
			c.mu.Unlock()
			return false
		}
		if len(c.queue) < c.engine.cfg.Buffer/2+1 {
			if f.Seq >= s.next {
				c.queue = append(c.queue, f)
				s.next = f.Seq + 1
			}
			c.mu.Unlock()
			select {
			case c.ready <- struct{}{}:
			default:
			}
			return true
		}
		c.mu.Unlock()
		select {
		case <-c.space:
		case <-c.done:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// deliver queues a live frame for s: it waits in s.pending while s reads
// the outbox (at most a queue's worth; beyond that the catch-up reads them
// from the outbox too), is dropped when the outbox gave it already, and
// otherwise goes to the queue (push).
func (c *Conn) deliver(s *subscription, f Frame) bool {
	c.mu.Lock()
	if f.Seq != 0 && f.Seq < s.next {
		c.mu.Unlock()
		return false // delivered from the outbox
	}
	if s.catching {
		if len(s.pending) >= c.engine.cfg.Buffer {
			// Live events are committed before they are published, so the
			// catch-up reads the dropped ones from the outbox.
			s.pending = nil
			c.mu.Unlock()
			return false
		}
		s.pending = append(s.pending, f)
		c.mu.Unlock()
		return true
	}
	if f.Seq != 0 {
		s.next = f.Seq + 1
	}
	c.mu.Unlock()
	return c.push(f)
}
