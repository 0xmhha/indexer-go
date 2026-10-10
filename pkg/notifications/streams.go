package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// NotificationTypeStream sends a setting's notifications to the stream
// connections of the key that owns the setting (/v1/subscriptions/stream).
// They are not stored or retried: a notification created while no stream
// of the owner is connected, or that a full connection has no room for,
// is lost. Settings that must not lose notifications use a webhook.
const NotificationTypeStream NotificationType = "stream"

// Stream message types.
const (
	// StreamNotification carries a notification of a stream setting.
	StreamNotification = "notification"
	// StreamLagging tells the owners of fast settings that the fast path
	// dropped blocks, from FromBlock on, because its queue was full.
	StreamLagging = "lagging"
)

// StreamMessage is a message sent to a stream connection.
type StreamMessage struct {
	Type         string        `json:"type"`
	Notification *Notification `json:"notification,omitempty"`
	FromBlock    uint64        `json:"from_block,omitempty"`
}

// DefaultMaxStreamsPerOwner is how many stream connections a key may hold
// when the configuration does not say.
const DefaultMaxStreamsPerOwner = 4

// streamBuffer is how many messages wait for a connection; a connection
// that falls further behind is closed (Stream.Overflowed).
const streamBuffer = 1024

// ErrTooManyStreams is returned by Streams.Open when the owner holds the
// most connections it may.
var ErrTooManyStreams = errors.New("too many stream connections for this key")

// Streams holds the open stream connections by owner (API key label).
type Streams struct {
	mu       sync.RWMutex
	byOwner  map[string]map[*Stream]struct{}
	maxConns int
}

// NewStreams returns an empty set of connections allowing maxPerOwner
// connections per owner (DefaultMaxStreamsPerOwner when not positive).
func NewStreams(maxPerOwner int) *Streams {
	if maxPerOwner <= 0 {
		maxPerOwner = DefaultMaxStreamsPerOwner
	}
	return &Streams{byOwner: map[string]map[*Stream]struct{}{}, maxConns: maxPerOwner}
}

// Stream is one connection's queue of encoded messages.
type Stream struct {
	owner      string
	messages   chan []byte
	done       chan struct{}
	once       sync.Once
	overflowed bool // set before done is closed by an overflow
}

// Messages are the connection's encoded messages (JSON), in order.
func (c *Stream) Messages() <-chan []byte { return c.messages }

// Done is closed when the connection must end: it was closed, or it fell
// more than its buffer behind (Overflowed).
func (c *Stream) Done() <-chan struct{} { return c.done }

// Overflowed reports whether the connection ended because it fell behind;
// read it after Done is closed.
func (c *Stream) Overflowed() bool { return c.overflowed }

// Owner is the label of the key the connection belongs to.
func (c *Stream) Owner() string { return c.owner }

// Open adds a connection of owner.
func (s *Streams) Open(owner string) (*Stream, error) {
	if owner == "" {
		return nil, errors.New("a stream needs an API key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	conns := s.byOwner[owner]
	if len(conns) >= s.maxConns {
		return nil, fmt.Errorf("%w (%d)", ErrTooManyStreams, s.maxConns)
	}
	if conns == nil {
		conns = map[*Stream]struct{}{}
		s.byOwner[owner] = conns
	}
	c := &Stream{owner: owner, messages: make(chan []byte, streamBuffer), done: make(chan struct{})}
	conns[c] = struct{}{}
	return c, nil
}

// Close removes a connection; closing it again does nothing.
func (s *Streams) Close(c *Stream) {
	s.mu.Lock()
	s.remove(c)
	s.mu.Unlock()
	c.once.Do(func() { close(c.done) })
}

// remove takes c out of its owner's set; the caller holds s.mu.
func (s *Streams) remove(c *Stream) {
	if conns := s.byOwner[c.owner]; conns != nil {
		delete(conns, c)
		if len(conns) == 0 {
			delete(s.byOwner, c.owner)
		}
	}
}

// Connected is how many connections owner holds.
func (s *Streams) Connected(owner string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byOwner[owner])
}

// send queues msg on every connection of owner without waiting and
// returns how many took it. A connection without room is ended as
// overflowed: its client must reconnect and use a webhook or durable
// settings for what it may not miss.
func (s *Streams) send(owner string, msg []byte) int {
	s.mu.RLock()
	var sent int
	var full []*Stream
	for c := range s.byOwner[owner] {
		select {
		case c.messages <- msg:
			sent++
		default:
			full = append(full, c)
		}
	}
	s.mu.RUnlock()
	for _, c := range full {
		s.mu.Lock()
		s.remove(c)
		s.mu.Unlock()
		c.once.Do(func() {
			c.overflowed = true
			close(c.done)
		})
	}
	return sent
}

// sendJSON encodes m and sends it to owner's connections.
func (s *Streams) sendJSON(owner string, m *StreamMessage) (int, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return 0, err
	}
	return s.send(owner, data), nil
}

// streamHandler is the handler of stream settings: delivery happens when
// a notification is created (deliverStream), so it only validates and
// answers TestSetting.
type streamHandler struct {
	streams *Streams
	logger  *zap.Logger
}

func (h *streamHandler) Type() NotificationType { return NotificationTypeStream }

// Validate requires an owner: the stream belongs to the key that owns the
// setting.
func (h *streamHandler) Validate(setting *NotificationSetting) error {
	if setting.Owner == "" {
		return errors.New("a stream setting needs an owner (create it with an API key)")
	}
	return nil
}

// Deliver sends a notification to the owner's connections; it fails when
// none is connected.
func (h *streamHandler) Deliver(_ context.Context, n *Notification, setting *NotificationSetting) (*DeliveryResult, error) {
	start := time.Now()
	sent, err := h.streams.sendJSON(setting.Owner, &StreamMessage{Type: StreamNotification, Notification: n})
	if err == nil && sent == 0 {
		err = errors.New("no stream connection of the setting's owner")
	}
	result := &DeliveryResult{Success: err == nil, DeliveredAt: time.Now(), Duration: time.Since(start).Milliseconds()}
	if err != nil {
		result.Error = err.Error()
	}
	return result, err
}
