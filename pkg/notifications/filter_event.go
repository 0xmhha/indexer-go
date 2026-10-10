package notifications

import (
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/declared"
)

// A filter's Event names one event by its human-readable signature with
// argument names ("Swap(address indexed sender, int256 amount0, ...)"): a
// log matches only when its first topic is that event's, and the
// notification carries the decoded arguments (EventPayload.Decoded).
// Participants match logs where one of the addresses is an indexed
// argument (topics after the first), such as the from or to of a token
// Transfer.

// filterEvents caches compiled event signatures.
var filterEvents sync.Map // signature -> *declared.TablePlan

// filterEvent returns the compiled event of a signature.
func filterEvent(sig string) (*declared.TablePlan, error) {
	if p, ok := filterEvents.Load(sig); ok {
		return p.(*declared.TablePlan), nil
	}
	ev, err := declared.ParseEvent(sig)
	if err != nil {
		return nil, err
	}
	if ev.Anonymous {
		return nil, fmt.Errorf("event %s is anonymous", ev.Name)
	}
	for _, a := range ev.Inputs {
		if a.Name == "" {
			return nil, fmt.Errorf("event %s has an unnamed argument", ev.Name)
		}
	}
	p := &declared.TablePlan{Name: ev.Name, Event: ev}
	filterEvents.Store(sig, p)
	return p, nil
}

// validateFilter checks a setting's filter when it is registered.
func validateFilter(f *NotifyFilter) error {
	if f == nil || f.Event == "" {
		return nil
	}
	if _, err := filterEvent(f.Event); err != nil {
		return fmt.Errorf("filter event %q: %w", f.Event, err)
	}
	return nil
}

// matchesEvent reports whether a log (by its topics) is of the filter's
// event.
func matchesEvent(f *NotifyFilter, topics []common.Hash) bool {
	if f.Event == "" {
		return true
	}
	p, err := filterEvent(f.Event)
	return err == nil && len(topics) > 0 && topics[0] == p.Event.ID
}

// matchesParticipants reports whether one of the filter's participants is
// an indexed argument of the log.
func matchesParticipants(f *NotifyFilter, topics []common.Hash) bool {
	if len(f.Participants) == 0 {
		return true
	}
	for _, topic := range topics[min(1, len(topics)):] {
		for _, a := range f.Participants {
			if topic == common.BytesToHash(a.Bytes()) {
				return true
			}
		}
	}
	return false
}

// decodeFilterEvent returns the arguments of a log of the filter's event,
// nil when the filter names no event or the log does not decode.
func decodeFilterEvent(f *NotifyFilter, l *types.Log) map[string]string {
	if f == nil || f.Event == "" || l == nil {
		return nil
	}
	p, err := filterEvent(f.Event)
	if err != nil {
		return nil
	}
	fields, err := p.Decode(gethconv.LogFromGeth(l))
	if err != nil {
		return nil
	}
	return fields
}

// FilterInput is a filter as an API request gives it, every value as a
// string.
type FilterInput struct {
	Addresses    []string   `json:"addresses,omitempty"`
	Topics       [][]string `json:"topics,omitempty"`
	MinValue     *string    `json:"minValue,omitempty"`
	Event        string     `json:"event,omitempty"`
	Participants []string   `json:"participants,omitempty"`
}

// Parse returns the filter, refusing malformed addresses, topics and
// event signatures instead of dropping them.
func (in FilterInput) Parse() (*NotifyFilter, error) {
	f := &NotifyFilter{Event: in.Event}
	address := func(field, s string) (common.Address, error) {
		if !common.IsHexAddress(s) {
			return common.Address{}, fmt.Errorf("filter %s: %q is not an address", field, s)
		}
		return common.HexToAddress(s), nil
	}
	for _, s := range in.Addresses {
		a, err := address("addresses", s)
		if err != nil {
			return nil, err
		}
		f.Addresses = append(f.Addresses, a)
	}
	for _, s := range in.Participants {
		a, err := address("participants", s)
		if err != nil {
			return nil, err
		}
		f.Participants = append(f.Participants, a)
	}
	for i, options := range in.Topics {
		position := []common.Hash{}
		for _, s := range options {
			b, err := hexutil.Decode(s)
			if err != nil || len(b) != common.HashLength {
				return nil, fmt.Errorf("filter topics[%d]: %q is not a 32-byte hex value", i, s)
			}
			position = append(position, common.BytesToHash(b))
		}
		f.Topics = append(f.Topics, position)
	}
	if in.MinValue != nil && *in.MinValue != "" {
		v := *in.MinValue
		f.MinValue = &v
	}
	if err := validateFilter(f); err != nil {
		return nil, err
	}
	return f, nil
}
