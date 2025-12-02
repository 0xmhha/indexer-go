package events

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ContractParser defines the interface for parsing contract events
type ContractParser interface {
	// ContractAddress returns the address this parser handles
	ContractAddress() common.Address

	// ContractName returns a human-readable name for the contract
	ContractName() string

	// SupportedEvents returns a list of event names this parser can handle
	SupportedEvents() []string

	// CanParse checks if this parser can handle the given log
	CanParse(log *types.Log) bool

	// Parse parses a log entry and returns a structured event
	Parse(ctx context.Context, log *types.Log) (*ParsedEvent, error)
}

// EventHandler defines the interface for handling parsed events
type EventHandler interface {
	// EventName returns the name of the event this handler processes
	EventName() string

	// Handle processes the parsed event data
	Handle(ctx context.Context, event *ParsedEvent) error
}

// StorageHandler defines the interface for persisting events
type StorageHandler interface {
	// Store persists the event to storage
	Store(ctx context.Context, event *ParsedEvent) error

	// EventTypes returns the event types this handler can store
	EventTypes() []string
}

// ParsedEvent represents a parsed contract event with typed data
type ParsedEvent struct {
	// Contract information
	ContractAddress common.Address
	ContractName    string

	// Event information
	EventName   string
	EventSig    common.Hash
	BlockNumber uint64
	TxHash      common.Hash
	LogIndex    uint

	// Parsed data as key-value pairs
	Data map[string]interface{}

	// Raw log for reference
	RawLog *types.Log

	// Timestamp (if available)
	Timestamp uint64
}

// EventField represents a parsed event field
type EventField struct {
	Name    string
	Type    string
	Indexed bool
	Value   interface{}
}

// ContractABI holds ABI information for a verified contract (Pure Data Structure)
type ContractABI struct {
	Address     common.Address
	Name        string
	ABI         *abi.ABI
	EventSigs   map[common.Hash]string // topic0 -> event name
	VerifiedAt  uint64
	BlockNumber uint64
}

// NewContractABI creates a new ContractABI from raw ABI JSON
func NewContractABI(address common.Address, name string, abiJSON string) (*ContractABI, error) {
	parsedABI, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	eventSigs := make(map[common.Hash]string)
	for name, event := range parsedABI.Events {
		eventSigs[event.ID] = name
	}

	return &ContractABI{
		Address:   address,
		Name:      name,
		ABI:       &parsedABI,
		EventSigs: eventSigs,
	}, nil
}

// GetEventName returns the event name for a given topic0
func (c *ContractABI) GetEventName(topic0 common.Hash) (string, bool) {
	name, ok := c.EventSigs[topic0]
	return name, ok
}

// GetEvent returns the ABI event definition for a given event name
func (c *ContractABI) GetEvent(eventName string) (abi.Event, bool) {
	event, ok := c.ABI.Events[eventName]
	return event, ok
}

// ABILogParser handles parsing of logs using ContractABI (Single Responsibility: Parsing Only)
type ABILogParser struct {
	abi *ContractABI
}

// NewABILogParser creates a new ABI log parser
func NewABILogParser(contractABI *ContractABI) *ABILogParser {
	return &ABILogParser{abi: contractABI}
}

// Parse parses a log entry using the ABI
func (p *ABILogParser) Parse(log *types.Log) (*ParsedEvent, error) {
	if len(log.Topics) == 0 {
		return nil, fmt.Errorf("log has no topics")
	}

	eventName, ok := p.abi.GetEventName(log.Topics[0])
	if !ok {
		return nil, fmt.Errorf("unknown event signature: %s", log.Topics[0].Hex())
	}

	event, ok := p.abi.GetEvent(eventName)
	if !ok {
		return nil, fmt.Errorf("event %s not found in ABI", eventName)
	}

	// Parse indexed and non-indexed arguments
	data := make(map[string]interface{})

	// Parse non-indexed arguments from data
	if len(log.Data) > 0 {
		values, err := event.Inputs.UnpackValues(log.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to unpack event data: %w", err)
		}

		nonIndexedIdx := 0
		for _, input := range event.Inputs {
			if !input.Indexed {
				if nonIndexedIdx < len(values) {
					data[input.Name] = convertABIValue(values[nonIndexedIdx])
					nonIndexedIdx++
				}
			}
		}
	}

	// Parse indexed arguments from topics
	topicIdx := 1 // topic[0] is event signature
	for _, input := range event.Inputs {
		if input.Indexed {
			if topicIdx < len(log.Topics) {
				data[input.Name] = parseIndexedTopic(input, log.Topics[topicIdx])
				topicIdx++
			}
		}
	}

	return &ParsedEvent{
		ContractAddress: log.Address,
		ContractName:    p.abi.Name,
		EventName:       eventName,
		EventSig:        log.Topics[0],
		BlockNumber:     log.BlockNumber,
		TxHash:          log.TxHash,
		LogIndex:        log.Index,
		Data:            data,
		RawLog:          log,
	}, nil
}

// CanParse checks if this parser can handle the given log
func (p *ABILogParser) CanParse(log *types.Log) bool {
	if log.Address != p.abi.Address {
		return false
	}
	if len(log.Topics) == 0 {
		return false
	}
	_, ok := p.abi.GetEventName(log.Topics[0])
	return ok
}

// parseIndexedTopic parses an indexed topic based on its type
func parseIndexedTopic(input abi.Argument, topic common.Hash) interface{} {
	switch input.Type.T {
	case abi.AddressTy:
		return common.BytesToAddress(topic.Bytes())
	case abi.UintTy, abi.IntTy:
		return new(big.Int).SetBytes(topic.Bytes())
	case abi.BoolTy:
		return topic[31] == 1
	case abi.BytesTy, abi.FixedBytesTy:
		return topic.Bytes()
	default:
		// For complex types like strings/arrays, indexed topics contain hash
		return topic
	}
}

// convertABIValue converts ABI decoded values to standard Go types
func convertABIValue(value interface{}) interface{} {
	switch v := value.(type) {
	case [32]byte:
		return common.BytesToHash(v[:])
	case []byte:
		return v
	case *big.Int:
		return v
	case common.Address:
		return v
	case bool:
		return v
	case string:
		return v
	default:
		return v
	}
}
