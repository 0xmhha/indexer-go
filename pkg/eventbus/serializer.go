package eventbus

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// JSONSerializer implements EventSerializer using JSON encoding
type JSONSerializer struct{}

// NewJSONSerializer creates a new JSON serializer
func NewJSONSerializer() *JSONSerializer {
	return &JSONSerializer{}
}

// Ensure JSONSerializer implements EventSerializer
var _ EventSerializer = (*JSONSerializer)(nil)

// eventEnvelope wraps an event with type information for deserialization
type eventEnvelope struct {
	Type      events.EventType `json:"type"`
	Timestamp time.Time        `json:"timestamp"`
	NodeID    string           `json:"node_id,omitempty"`
	ChainID   string           `json:"chain_id,omitempty"`
	Data      json.RawMessage  `json:"data"`
}

// Serialize converts an event to JSON bytes
func (s *JSONSerializer) Serialize(event events.Event) ([]byte, error) {
	if event == nil {
		return nil, ErrSerializationFailed
	}

	data, err := events.MarshalEvent(event)
	if err != nil {
		if _, ok := events.CodecOf(event.Type()); !ok && !builtin(event) {
			return nil, fmt.Errorf("%w: unknown event type %T", ErrInvalidEventType, event)
		}
		return nil, fmt.Errorf("%w: %v", ErrSerializationFailed, err)
	}

	envelope := eventEnvelope{
		Type:      event.Type(),
		Timestamp: event.Timestamp(),
		Data:      data,
	}

	result, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSerializationFailed, err)
	}

	return result, nil
}

// Deserialize converts JSON bytes back to an event
func (s *JSONSerializer) Deserialize(data []byte) (events.Event, error) {
	if len(data) == 0 {
		return nil, ErrDeserializationFailed
	}

	var envelope eventEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDeserializationFailed, err)
	}

	if !builtinType(envelope.Type) {
		if _, ok := events.CodecOf(envelope.Type); !ok {
			return nil, fmt.Errorf("%w: unknown event type %s", ErrInvalidEventType, envelope.Type)
		}
	}
	event, err := events.UnmarshalEvent(envelope.Type, envelope.Data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDeserializationFailed, err)
	}
	return event, nil
}

// builtin reports whether the event is one of the types pkg/events encodes
// itself.
func builtin(event events.Event) bool { return builtinType(event.Type()) }

func builtinType(t events.EventType) bool {
	switch t {
	case events.EventTypeBlock, events.EventTypeTransaction, events.EventTypeLog,
		events.EventTypeChainConfig, events.EventTypeValidatorSet, events.EventTypeReorg:
		return true
	}
	return false
}

// ContentType returns the MIME type for JSON
func (s *JSONSerializer) ContentType() string {
	return "application/json"
}

