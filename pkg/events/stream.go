package events

import "time"

// Stream is a chain's change stream position, embedded in every event type
// that is recorded in the outbox (refactoring plan R3-1). Events of indexed
// blocks are numbered when their block commits: the numbers of one chain
// start at 1 and increase by one per event, without gaps, in commit order.
// Events published directly (pending transactions) are not in the stream
// and keep 0.
type Stream struct {
	StreamSeq uint64 `json:"-"`
}

// Sequence returns the event's position in the change stream, 0 if none.
func (s *Stream) Sequence() uint64 { return s.StreamSeq }

// SetSequence sets the event's position in the change stream.
func (s *Stream) SetSequence(seq uint64) { s.StreamSeq = seq }

// Sequenced is an event that carries a change stream position (it embeds
// Stream).
type Sequenced interface {
	Event
	Sequence() uint64
	SetSequence(seq uint64)
}

// SequenceOf returns an event's change stream position, 0 if the event is
// not in the stream.
func SequenceOf(ev Event) uint64 {
	if s, ok := ev.(Sequenced); ok {
		return s.Sequence()
	}
	return 0
}

// EventTypeSkipped is the type of SkippedEvent.
const EventTypeSkipped EventType = "skipped"

// SkippedEvent stands for a change stream entry this build cannot decode
// (written by a build that knows more event types). It carries only the
// entry's position and type, so consumers that follow positions see no
// gap; no subscription delivers it.
type SkippedEvent struct {
	Stream
	Original EventType
}

// Type implements Event.
func (e *SkippedEvent) Type() EventType { return EventTypeSkipped }

// Timestamp implements Event: unknown, the zero time.
func (e *SkippedEvent) Timestamp() time.Time { return time.Time{} }
