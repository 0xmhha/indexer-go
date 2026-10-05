package events

import (
	"fmt"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// SourcedEvent is an event decoded from a contract log and identified by
// the emitting contract and the event name. Chain-specific event types
// (for example StableNet system contract events) implement it, so filters
// can match them without knowing the type.
type SourcedEvent interface {
	Event
	// Source returns the contract that emitted the event, the event name
	// and the block number.
	Source() (contract common.Address, name string, block uint64)
}

// EventCodec converts an event type to and from a JSON-friendly value, for
// event buses that carry events between processes. Packages that define
// their own event types register one; the event bus serializes the built-in
// types itself.
type EventCodec struct {
	// Encode returns the value to marshal for an event of the type.
	Encode func(Event) (interface{}, error)
	// Decode rebuilds an event from the marshaled value.
	Decode func(data []byte) (Event, error)
}

var (
	codecMu sync.RWMutex
	codecs  = map[EventType]EventCodec{}
)

// RegisterCodec registers the codec of an event type. Registering a type
// twice panics: it is a wiring bug.
func RegisterCodec(t EventType, c EventCodec) {
	codecMu.Lock()
	defer codecMu.Unlock()
	if _, dup := codecs[t]; dup {
		panic(fmt.Sprintf("events: codec for %q registered twice", t))
	}
	codecs[t] = c
}

// CodecOf returns the registered codec of an event type.
func CodecOf(t EventType) (EventCodec, bool) {
	codecMu.RLock()
	defer codecMu.RUnlock()
	c, ok := codecs[t]
	return c, ok
}

// CodecTypes returns the event types with a registered codec, sorted.
func CodecTypes() []EventType {
	codecMu.RLock()
	defer codecMu.RUnlock()
	out := make([]EventType, 0, len(codecs))
	for t := range codecs {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
