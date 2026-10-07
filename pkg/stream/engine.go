package stream

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// The subscription engine (refactoring plan R3-3) delivers events to many
// client connections (GraphQL subscriptions) without letting one slow
// client hold up the others:
//
//   - Subscriptions are indexed by event type in copy-on-write lists, so
//     publishing reads them without a lock and visits only the subscriptions
//     of the event's type.
//   - Each topic (a subscription kind, such as newBlock) encodes an event
//     once; every connection that receives it shares the bytes.
//   - Each connection has a bounded queue that its writer goroutine drains.
//     Publishing never waits for a connection. A connection whose queue is
//     full is disconnected with the sequence to resubscribe from
//     (SlowError), instead of silently losing events.

// Defaults of EngineConfig.
const (
	DefaultEngineBuffer  = 16384
	DefaultEngineHistory = 100
)

// EngineConfig configures an Engine.
type EngineConfig struct {
	// Buffer is how many frames a connection may have queued. A connection
	// that falls further behind is disconnected.
	Buffer int
	// History is how many recent events are kept for subscriptions that
	// ask for a replay.
	History int
}

// Encoder encodes an event for a topic; false skips the event.
type Encoder func(events.Event) ([]byte, bool)

// Frame is one encoded event for one subscription of a connection.
// Payload is shared between connections and must not be modified.
type Frame struct {
	Sub     string
	Seq     uint64
	Payload []byte
}

// SlowError is the error of a connection disconnected because its queue
// was full. ResumeFrom is the change stream sequence of the first event it
// did not receive (0 if the events were not sequenced): resubscribing from
// it misses nothing.
type SlowError struct {
	ResumeFrom uint64
}

func (e *SlowError) Error() string {
	if e.ResumeFrom == 0 {
		return "stream: subscriber too slow"
	}
	return fmt.Sprintf("stream: subscriber too slow; resume from sequence %d", e.ResumeFrom)
}

// Errors of Conn.
var (
	ErrConnClosed   = errors.New("stream: connection closed")
	ErrUnknownTopic = errors.New("stream: unknown topic")
	ErrDuplicateSub = errors.New("stream: subscription id in use")
)

type topic struct {
	eventType events.EventType
	encode    Encoder
}

// subscription is one subscription of a connection. The fields below
// match are guarded by conn.mu.
type subscription struct {
	conn  *Conn
	id    string
	topic string
	match func(events.Event) bool

	// next is the lowest sequence still to deliver: sequenced events below
	// it were delivered already (a subscription that resumed from the
	// outbox, resume.go). 0 delivers every event.
	next uint64
	// catching is true while the subscription reads the outbox; live
	// events wait in pending meanwhile.
	catching bool
	pending  []Frame
}

// subList is the copy-on-write list of one event type's subscriptions.
type subList struct {
	subs atomic.Pointer[[]*subscription]
}

// Engine fans events out to connections.
type Engine struct {
	cfg EngineConfig

	// mu guards topics and changes of the index.
	mu     sync.Mutex
	topics map[string]topic
	index  atomic.Pointer[map[events.EventType]*subList]

	// outbox is where subscriptions resume from (SetOutbox).
	outbox atomic.Pointer[outboxSource]

	// pubMu serializes publishing with subscriptions that replay history.
	pubMu   sync.Mutex
	history []events.Event // ring of the last cfg.History events
	histAt  int
	lastSeq uint64
	closed  atomic.Bool

	conns sync.Map // *Conn -> struct{}

	published   atomic.Uint64
	frames      atomic.Uint64
	disconnects atomic.Uint64
}

// NewEngine returns an engine without topics.
func NewEngine(cfg EngineConfig) *Engine {
	if cfg.Buffer <= 0 {
		cfg.Buffer = DefaultEngineBuffer
	}
	if cfg.History <= 0 {
		cfg.History = DefaultEngineHistory
	}
	e := &Engine{cfg: cfg, topics: make(map[string]topic), history: make([]events.Event, cfg.History)}
	empty := make(map[events.EventType]*subList)
	e.index.Store(&empty)
	return e
}

// AddTopic adds a topic: subscriptions to it receive the events of
// eventType that encode returns true for.
func (e *Engine) AddTopic(name string, eventType events.EventType, encode Encoder) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.topics[name] = topic{eventType: eventType, encode: encode}
}

// EventTypes returns the event types of the topics.
func (e *Engine) EventTypes() []events.EventType {
	e.mu.Lock()
	defer e.mu.Unlock()
	seen := make(map[events.EventType]bool)
	var out []events.EventType
	for _, t := range e.topics {
		if !seen[t.eventType] {
			seen[t.eventType] = true
			out = append(out, t.eventType)
		}
	}
	return out
}

// Publish delivers an event to the subscriptions of its type. It never
// waits for a connection. Events must be published from one goroutine at a
// time, in stream order.
func (e *Engine) Publish(ev events.Event) {
	e.pubMu.Lock()
	defer e.pubMu.Unlock()
	if e.closed.Load() {
		return
	}
	e.published.Add(1)
	seq := events.SequenceOf(ev)
	if seq != 0 {
		if e.lastSeq != 0 && seq > e.lastSeq+1 {
			// Events were lost before the engine (a full bus buffer): every
			// connection may have missed some, so all resubscribe from the
			// first missing one.
			e.failAll(&SlowError{ResumeFrom: e.lastSeq + 1})
		}
		if seq > e.lastSeq {
			e.lastSeq = seq
		}
	}
	e.history[e.histAt] = ev
	e.histAt = (e.histAt + 1) % len(e.history)

	list := (*e.index.Load())[ev.Type()]
	if list == nil {
		return
	}
	subs := *list.subs.Load()
	if len(subs) == 0 {
		return
	}
	enc := encodings{engine: e, ev: ev}
	for _, s := range subs {
		if s.match != nil && !s.match(ev) {
			continue
		}
		payload, ok := enc.get(s.topic)
		if !ok {
			continue
		}
		if s.conn.deliver(s, Frame{Sub: s.id, Seq: seq, Payload: payload}) {
			e.frames.Add(1)
		}
	}
}

// encodings encodes one event once per topic, on first use.
type encodings struct {
	engine *Engine
	ev     events.Event
	done   []encoded
}

type encoded struct {
	topic   string
	payload []byte
	ok      bool
}

func (c *encodings) get(name string) ([]byte, bool) {
	for _, d := range c.done {
		if d.topic == name {
			return d.payload, d.ok
		}
	}
	e := c.engine
	e.mu.Lock()
	t := e.topics[name]
	e.mu.Unlock()
	var payload []byte
	ok := false
	if t.encode != nil {
		payload, ok = t.encode(c.ev)
	}
	c.done = append(c.done, encoded{topic: name, payload: payload, ok: ok})
	return payload, ok
}

// Close disconnects every connection with ErrConnClosed; later publishes
// are ignored.
func (e *Engine) Close() {
	e.pubMu.Lock()
	defer e.pubMu.Unlock()
	e.closed.Store(true)
	e.failAll(ErrConnClosed)
}

// Closed reports whether the engine was closed.
func (e *Engine) Closed() bool { return e.closed.Load() }

// failAll disconnects every connection with err.
func (e *Engine) failAll(err error) {
	e.conns.Range(func(k, _ any) bool {
		k.(*Conn).fail(err)
		return true
	})
}

// EngineStats are an engine's counters.
type EngineStats struct {
	Published       uint64 // events published
	Frames          uint64 // frames queued for connections
	SlowDisconnects uint64 // connections disconnected as too slow
	Connections     int    // open connections
	Subscriptions   int    // subscriptions of open connections
}

// Stats returns the engine's counters.
func (e *Engine) Stats() EngineStats {
	s := EngineStats{Published: e.published.Load(), Frames: e.frames.Load(), SlowDisconnects: e.disconnects.Load()}
	e.conns.Range(func(_, _ any) bool { s.Connections++; return true })
	for _, l := range *e.index.Load() {
		s.Subscriptions += len(*l.subs.Load())
	}
	return s
}

// add puts s into the index.
func (e *Engine) add(s *subscription, t topic) {
	idx := *e.index.Load()
	list := idx[t.eventType]
	if list == nil {
		next := make(map[events.EventType]*subList, len(idx)+1)
		for k, v := range idx {
			next[k] = v
		}
		list = &subList{}
		empty := []*subscription{}
		list.subs.Store(&empty)
		next[t.eventType] = list
		e.index.Store(&next)
	}
	old := *list.subs.Load()
	next := make([]*subscription, len(old), len(old)+1)
	copy(next, old)
	next = append(next, s)
	list.subs.Store(&next)
}

// remove takes the subscriptions for which drop is true out of the index.
func (e *Engine) remove(drop func(*subscription) bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, list := range *e.index.Load() {
		old := *list.subs.Load()
		var next []*subscription
		changed := false
		for _, s := range old {
			if drop(s) {
				changed = true
				continue
			}
			next = append(next, s)
		}
		if changed {
			if next == nil {
				next = []*subscription{}
			}
			list.subs.Store(&next)
		}
	}
}

// Connect opens a connection. Its writer reads frames with Take when Ready
// signals, and calls Close when it is done.
func (e *Engine) Connect() *Conn {
	c := &Conn{engine: e, ready: make(chan struct{}, 1), space: make(chan struct{}, 1), done: make(chan struct{}), subs: make(map[string]*subscription)}
	e.conns.Store(c, struct{}{})
	if e.closed.Load() { // closed meanwhile: failAll may have missed c
		c.fail(ErrConnClosed)
	}
	return c
}

// Conn is a client connection: its subscriptions and its queue of frames.
type Conn struct {
	engine *Engine
	ready  chan struct{} // signalled when frames are queued
	space  chan struct{} // signalled when Take empties the queue
	done   chan struct{} // closed when the connection fails or closes

	mu    sync.Mutex
	queue []Frame
	err   error
	subs  map[string]*subscription
}

// Subscribe subscribes the connection to a topic as id, receiving the
// events match returns true for (nil: all). With replay > 0 the last
// replay matching events of the engine's history are queued first, ahead
// of every later event.
func (c *Conn) Subscribe(id, topicName string, match func(events.Event) bool, replay int) error {
	e := c.engine
	if replay > 0 {
		// Hold publishing so that no event falls between the replay and
		// the subscription.
		e.pubMu.Lock()
		defer e.pubMu.Unlock()
	}
	e.mu.Lock()
	t, ok := e.topics[topicName]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownTopic, topicName)
	}
	s := &subscription{conn: c, id: id, topic: topicName, match: match}
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

	if replay > 0 {
		var frames []Frame
		for i := 0; i < len(e.history); i++ {
			ev := e.history[(e.histAt+i)%len(e.history)]
			if ev == nil || ev.Type() != t.eventType || (match != nil && !match(ev)) {
				continue
			}
			if payload, ok := t.encode(ev); ok {
				frames = append(frames, Frame{Sub: id, Seq: events.SequenceOf(ev), Payload: payload})
			}
		}
		if len(frames) > replay {
			frames = frames[len(frames)-replay:]
		}
		for _, f := range frames {
			c.push(f)
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	c.mu.Lock()
	live := c.err == nil && c.subs[id] == s
	c.mu.Unlock()
	if !live { // closed or unsubscribed meanwhile
		return nil
	}
	e.add(s, t)
	return nil
}

// Unsubscribe ends subscription id; its queued frames are not returned.
func (c *Conn) Unsubscribe(id string) {
	c.mu.Lock()
	s, ok := c.subs[id]
	delete(c.subs, id)
	c.mu.Unlock()
	if ok {
		c.engine.remove(func(x *subscription) bool { return x == s })
	}
}

// Ready is signalled when frames may be queued.
func (c *Conn) Ready() <-chan struct{} { return c.ready }

// Done is closed when the connection failed or was closed; Take returns
// the reason.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Take returns the queued frames of current subscriptions, oldest first,
// and empties the queue. After the connection failed or closed it returns
// the reason instead (a *SlowError for a slow connection).
func (c *Conn) Take() ([]Frame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	out := make([]Frame, 0, len(c.queue))
	for _, f := range c.queue {
		if _, ok := c.subs[f.Sub]; ok {
			out = append(out, f)
		}
	}
	c.queue = c.queue[:0]
	select {
	case c.space <- struct{}{}:
	default:
	}
	return out, nil
}

// Close ends the connection and its subscriptions.
func (c *Conn) Close() { c.fail(ErrConnClosed) }

// push queues a frame without waiting; a full queue fails the connection.
func (c *Conn) push(f Frame) bool {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return false
	}
	if len(c.queue) >= c.engine.cfg.Buffer {
		resume := f.Seq
		for _, q := range c.queue {
			if q.Seq != 0 {
				resume = q.Seq
				break
			}
		}
		c.mu.Unlock()
		c.engine.disconnects.Add(1)
		c.fail(&SlowError{ResumeFrom: resume})
		return false
	}
	c.queue = append(c.queue, f)
	c.mu.Unlock()
	select {
	case c.ready <- struct{}{}:
	default:
	}
	return true
}

// fail ends the connection with err, once, and takes its subscriptions out
// of the index.
func (c *Conn) fail(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	c.queue = nil
	subs := c.subs
	c.subs = make(map[string]*subscription)
	close(c.done)
	c.mu.Unlock()
	c.engine.conns.Delete(c)
	if len(subs) > 0 {
		c.engine.remove(func(s *subscription) bool { return s.conn == c })
	}
}
