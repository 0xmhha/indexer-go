package events

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ABIParser implements ContractParser using a dynamic ABI
type ABIParser struct {
	contractABI *ContractABI
	eventBus    *EventBus
}

// NewABIParser creates a new ABI-based parser
func NewABIParser(contractABI *ContractABI, eventBus *EventBus) *ABIParser {
	return &ABIParser{
		contractABI: contractABI,
		eventBus:    eventBus,
	}
}

// ContractAddress returns the address this parser handles
func (p *ABIParser) ContractAddress() common.Address {
	return p.contractABI.Address
}

// ContractName returns the contract name
func (p *ABIParser) ContractName() string {
	return p.contractABI.Name
}

// SupportedEvents returns all event names from the ABI
func (p *ABIParser) SupportedEvents() []string {
	events := make([]string, 0, len(p.contractABI.EventSigs))
	for _, name := range p.contractABI.EventSigs {
		events = append(events, name)
	}
	return events
}

// CanParse checks if this parser can handle the log
func (p *ABIParser) CanParse(log *types.Log) bool {
	if log.Address != p.contractABI.Address {
		return false
	}
	if len(log.Topics) == 0 {
		return false
	}
	_, ok := p.contractABI.GetEventName(log.Topics[0])
	return ok
}

// Parse parses a log entry using the ABI
func (p *ABIParser) Parse(ctx context.Context, log *types.Log) (*ParsedEvent, error) {
	return p.contractABI.ParseLog(log)
}

// ABIParserFactory creates ABIParser instances from ABI JSON
type ABIParserFactory struct {
	eventBus *EventBus
}

// NewABIParserFactory creates a new factory
func NewABIParserFactory(eventBus *EventBus) *ABIParserFactory {
	return &ABIParserFactory{
		eventBus: eventBus,
	}
}

// CreateFromJSON creates an ABIParser from contract address, name, and ABI JSON
func (f *ABIParserFactory) CreateFromJSON(address common.Address, name, abiJSON string) (*ABIParser, error) {
	contractABI, err := NewContractABI(address, name, abiJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to create contract ABI: %w", err)
	}
	return NewABIParser(contractABI, f.eventBus), nil
}

// CreateFromABI creates an ABIParser from an existing ContractABI
func (f *ABIParserFactory) CreateFromABI(contractABI *ContractABI) *ABIParser {
	return NewABIParser(contractABI, f.eventBus)
}

// DynamicEventParser wraps the registry to provide a unified parsing interface
type DynamicEventParser struct {
	registry *ParserRegistry
	factory  *ABIParserFactory
}

// NewDynamicEventParser creates a new dynamic event parser
func NewDynamicEventParser(eventBus *EventBus) *DynamicEventParser {
	registry := NewParserRegistry(eventBus)
	factory := NewABIParserFactory(eventBus)

	return &DynamicEventParser{
		registry: registry,
		factory:  factory,
	}
}

// RegisterContractABI registers a contract for parsing using its ABI
func (p *DynamicEventParser) RegisterContractABI(address common.Address, name, abiJSON string) error {
	parser, err := p.factory.CreateFromJSON(address, name, abiJSON)
	if err != nil {
		return err
	}
	return p.registry.RegisterParser(parser)
}

// RegisterCustomParser registers a custom parser for a contract
func (p *DynamicEventParser) RegisterCustomParser(parser ContractParser) error {
	return p.registry.RegisterParser(parser)
}

// RegisterHandler registers an event handler
func (p *DynamicEventParser) RegisterHandler(handler EventHandler) {
	p.registry.RegisterHandler(handler)
}

// RegisterStorageHandler registers a storage handler
func (p *DynamicEventParser) RegisterStorageHandler(handler StorageHandler) {
	p.registry.RegisterStorageHandler(handler)
}

// UnregisterContract removes a contract parser
func (p *DynamicEventParser) UnregisterContract(address common.Address) {
	p.registry.UnregisterParser(address)
}

// ParseLog parses a log using the registered parser
func (p *DynamicEventParser) ParseLog(ctx context.Context, log *types.Log) (*ParsedEvent, error) {
	return p.registry.ParseLog(ctx, log)
}

// ProcessLog parses, handles, and stores a log
func (p *DynamicEventParser) ProcessLog(ctx context.Context, log *types.Log) (*ParsedEvent, error) {
	return p.registry.ProcessLog(ctx, log)
}

// ListContracts returns all registered contract addresses
func (p *DynamicEventParser) ListContracts() []common.Address {
	return p.registry.ListRegisteredContracts()
}

// GetContractInfo returns information about a registered contract
func (p *DynamicEventParser) GetContractInfo(address common.Address) *ContractInfo {
	return p.registry.GetContractInfo(address)
}

// IsContractRegistered checks if a contract is registered
func (p *DynamicEventParser) IsContractRegistered(address common.Address) bool {
	_, hasParser := p.registry.GetParser(address)
	_, hasABI := p.registry.GetABI(address)
	return hasParser || hasABI
}
