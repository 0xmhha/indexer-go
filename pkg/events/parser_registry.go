package events

import (
	"context"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ParserRegistry manages contract parsers and event handlers
type ParserRegistry struct {
	mu sync.RWMutex

	// Contract parsers indexed by address
	parsers map[common.Address]ContractParser

	// ABI-based parsers indexed by address
	abiParsers map[common.Address]*ABILogParser

	// ABI data indexed by address (for info queries)
	abiData map[common.Address]*ContractABI

	// Event handlers indexed by event name
	handlers map[string][]EventHandler

	// Storage handlers indexed by event type
	storageHandlers map[string][]StorageHandler

	// EventBus for publishing events
	eventBus *EventBus

	// Default handler for unknown events
	defaultHandler EventHandler
}

// NewParserRegistry creates a new parser registry
func NewParserRegistry(eventBus *EventBus) *ParserRegistry {
	return &ParserRegistry{
		parsers:         make(map[common.Address]ContractParser),
		abiParsers:      make(map[common.Address]*ABILogParser),
		abiData:         make(map[common.Address]*ContractABI),
		handlers:        make(map[string][]EventHandler),
		storageHandlers: make(map[string][]StorageHandler),
		eventBus:        eventBus,
	}
}

// RegisterParser registers a contract parser
func (r *ParserRegistry) RegisterParser(parser ContractParser) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	addr := parser.ContractAddress()
	if _, exists := r.parsers[addr]; exists {
		return fmt.Errorf("parser already registered for address: %s", addr.Hex())
	}

	r.parsers[addr] = parser
	return nil
}

// RegisterABI registers a contract ABI for dynamic parsing
func (r *ParserRegistry) RegisterABI(contractABI *ContractABI) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	addr := contractABI.Address
	if _, exists := r.abiParsers[addr]; exists {
		return fmt.Errorf("ABI already registered for address: %s", addr.Hex())
	}

	// Store ABI data for queries
	r.abiData[addr] = contractABI
	// Create parser with single responsibility (parsing only)
	r.abiParsers[addr] = NewABILogParser(contractABI)
	return nil
}

// RegisterABIFromJSON registers a contract ABI from JSON string
func (r *ParserRegistry) RegisterABIFromJSON(address common.Address, name, abiJSON string) error {
	contractABI, err := NewContractABI(address, name, abiJSON)
	if err != nil {
		return fmt.Errorf("failed to create contract ABI: %w", err)
	}
	return r.RegisterABI(contractABI)
}

// UnregisterParser removes a parser for an address
func (r *ParserRegistry) UnregisterParser(address common.Address) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.parsers, address)
	delete(r.abiParsers, address)
	delete(r.abiData, address)
}

// RegisterHandler registers an event handler for a specific event type
func (r *ParserRegistry) RegisterHandler(handler EventHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	eventName := handler.EventName()
	r.handlers[eventName] = append(r.handlers[eventName], handler)
}

// RegisterStorageHandler registers a storage handler for event types
func (r *ParserRegistry) RegisterStorageHandler(handler StorageHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, eventType := range handler.EventTypes() {
		r.storageHandlers[eventType] = append(r.storageHandlers[eventType], handler)
	}
}

// SetDefaultHandler sets the default handler for unknown events
func (r *ParserRegistry) SetDefaultHandler(handler EventHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultHandler = handler
}

// GetParser returns the parser for an address
func (r *ParserRegistry) GetParser(address common.Address) (ContractParser, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, ok := r.parsers[address]
	return parser, ok
}

// GetABI returns the ABI for an address
func (r *ParserRegistry) GetABI(address common.Address) (*ContractABI, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	abi, ok := r.abiData[address]
	return abi, ok
}

// ParseLog parses a log using registered parsers
func (r *ParserRegistry) ParseLog(ctx context.Context, log *types.Log) (*ParsedEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try custom parser first
	if parser, ok := r.parsers[log.Address]; ok {
		if parser.CanParse(log) {
			return parser.Parse(ctx, log)
		}
	}

	// Try ABI parser (uses ABILogParser with single responsibility)
	if parser, ok := r.abiParsers[log.Address]; ok {
		if parser.CanParse(log) {
			return parser.Parse(log)
		}
	}

	return nil, fmt.Errorf("no parser registered for address: %s", log.Address.Hex())
}

// HandleEvent dispatches an event to registered handlers
func (r *ParserRegistry) HandleEvent(ctx context.Context, event *ParsedEvent) error {
	r.mu.RLock()
	handlers := r.handlers[event.EventName]
	defaultHandler := r.defaultHandler
	r.mu.RUnlock()

	// Run all handlers for this event type
	for _, handler := range handlers {
		if err := handler.Handle(ctx, event); err != nil {
			return fmt.Errorf("handler error for %s: %w", event.EventName, err)
		}
	}

	// Use default handler if no specific handlers
	if len(handlers) == 0 && defaultHandler != nil {
		return defaultHandler.Handle(ctx, event)
	}

	return nil
}

// StoreEvent persists an event using registered storage handlers
func (r *ParserRegistry) StoreEvent(ctx context.Context, event *ParsedEvent) error {
	r.mu.RLock()
	handlers := r.storageHandlers[event.EventName]
	r.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler.Store(ctx, event); err != nil {
			return fmt.Errorf("storage error for %s: %w", event.EventName, err)
		}
	}

	return nil
}

// ProcessLog parses, handles, and stores a log in one operation
// Uses Pipeline pattern for clean separation of concerns (SRP compliance)
func (r *ParserRegistry) ProcessLog(ctx context.Context, log *types.Log) (*ParsedEvent, error) {
	// Parse the log
	event, err := r.ParseLog(ctx, log)
	if err != nil {
		return nil, err
	}

	// Build and execute pipeline
	pipeline := r.buildPipeline()
	if err := pipeline.Execute(ctx, event); err != nil {
		return nil, err
	}

	return event, nil
}

// buildPipeline creates the event processing pipeline
// Each stage has single responsibility (SRP)
func (r *ParserRegistry) buildPipeline() *EventPipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return NewPipelineBuilder().
		WithHandler(r.handlers, r.defaultHandler).
		WithStorage(r.storageHandlers).
		WithPublish(r.eventBus).
		Build()
}

// ListRegisteredContracts returns all registered contract addresses
func (r *ParserRegistry) ListRegisteredContracts() []common.Address {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[common.Address]bool)
	var addresses []common.Address

	for addr := range r.parsers {
		if !seen[addr] {
			addresses = append(addresses, addr)
			seen[addr] = true
		}
	}

	for addr := range r.abiData {
		if !seen[addr] {
			addresses = append(addresses, addr)
			seen[addr] = true
		}
	}

	return addresses
}

// GetContractInfo returns information about a registered contract
func (r *ParserRegistry) GetContractInfo(address common.Address) *ContractInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info := &ContractInfo{
		Address: address,
	}

	if parser, ok := r.parsers[address]; ok {
		info.Name = parser.ContractName()
		info.Events = parser.SupportedEvents()
		info.HasCustomParser = true
	}

	if abi, ok := r.abiData[address]; ok {
		info.Name = abi.Name
		info.HasABI = true
		for _, eventName := range abi.EventSigs {
			info.Events = append(info.Events, eventName)
		}
	}

	return info
}

// ContractInfo holds information about a registered contract
type ContractInfo struct {
	Address         common.Address
	Name            string
	Events          []string
	HasCustomParser bool
	HasABI          bool
}
