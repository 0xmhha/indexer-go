package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// ContractRegistration represents a registered contract
type ContractRegistration struct {
	Address      common.Address `json:"address"`
	Name         string         `json:"name"`
	ABI          string         `json:"abi"`
	RegisteredAt time.Time      `json:"registeredAt"`
	BlockNumber  uint64         `json:"blockNumber"`
	IsVerified   bool           `json:"isVerified"`
	Events       []string       `json:"events"`
}

// ContractRegistrationService manages contract registrations
type ContractRegistrationService struct {
	mu            sync.RWMutex
	registrations map[common.Address]*ContractRegistration
	parser        *DynamicEventParser
	storage       ContractRegistrationStorage
}

// ContractRegistrationStorage interface for persisting registrations
type ContractRegistrationStorage interface {
	SaveContractRegistration(ctx context.Context, reg *ContractRegistration) error
	GetContractRegistration(ctx context.Context, address common.Address) (*ContractRegistration, error)
	ListContractRegistrations(ctx context.Context) ([]*ContractRegistration, error)
	DeleteContractRegistration(ctx context.Context, address common.Address) error
}

// NewContractRegistrationService creates a new registration service
func NewContractRegistrationService(parser *DynamicEventParser, storage ContractRegistrationStorage) *ContractRegistrationService {
	return &ContractRegistrationService{
		registrations: make(map[common.Address]*ContractRegistration),
		parser:        parser,
		storage:       storage,
	}
}

// RegisterContractInput represents input for contract registration
type RegisterContractInput struct {
	Address     string `json:"address"`
	Name        string `json:"name"`
	ABI         string `json:"abi"`
	BlockNumber uint64 `json:"blockNumber"`
}

// RegisterContract registers a new contract for event parsing
func (s *ContractRegistrationService) RegisterContract(ctx context.Context, input RegisterContractInput) (*ContractRegistration, error) {
	address := common.HexToAddress(input.Address)

	// Check if already registered
	s.mu.RLock()
	if _, exists := s.registrations[address]; exists {
		s.mu.RUnlock()
		return nil, fmt.Errorf("contract already registered: %s", address.Hex())
	}
	s.mu.RUnlock()

	// Validate ABI JSON
	var abiData []interface{}
	if err := json.Unmarshal([]byte(input.ABI), &abiData); err != nil {
		return nil, fmt.Errorf("invalid ABI JSON: %w", err)
	}

	// Register with parser
	if err := s.parser.RegisterContractABI(address, input.Name, input.ABI); err != nil {
		return nil, fmt.Errorf("failed to register parser: %w", err)
	}

	// Get event list
	info := s.parser.GetContractInfo(address)

	// Create registration
	reg := &ContractRegistration{
		Address:      address,
		Name:         input.Name,
		ABI:          input.ABI,
		RegisteredAt: time.Now(),
		BlockNumber:  input.BlockNumber,
		IsVerified:   true,
		Events:       info.Events,
	}

	// Save to storage
	if s.storage != nil {
		if err := s.storage.SaveContractRegistration(ctx, reg); err != nil {
			// Rollback parser registration
			s.parser.UnregisterContract(address)
			return nil, fmt.Errorf("failed to save registration: %w", err)
		}
	}

	// Store in memory
	s.mu.Lock()
	s.registrations[address] = reg
	s.mu.Unlock()

	return reg, nil
}

// UnregisterContract removes a contract registration
func (s *ContractRegistrationService) UnregisterContract(ctx context.Context, address common.Address) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.registrations[address]; !exists {
		return fmt.Errorf("contract not registered: %s", address.Hex())
	}

	// Remove from parser
	s.parser.UnregisterContract(address)

	// Remove from storage
	if s.storage != nil {
		if err := s.storage.DeleteContractRegistration(ctx, address); err != nil {
			return fmt.Errorf("failed to delete registration: %w", err)
		}
	}

	// Remove from memory
	delete(s.registrations, address)

	return nil
}

// GetContract returns a registered contract
func (s *ContractRegistrationService) GetContract(ctx context.Context, address common.Address) (*ContractRegistration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if reg, exists := s.registrations[address]; exists {
		return reg, nil
	}

	// Try storage
	if s.storage != nil {
		return s.storage.GetContractRegistration(ctx, address)
	}

	return nil, fmt.Errorf("contract not found: %s", address.Hex())
}

// ListContracts returns all registered contracts
func (s *ContractRegistrationService) ListContracts(ctx context.Context) ([]*ContractRegistration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*ContractRegistration, 0, len(s.registrations))
	for _, reg := range s.registrations {
		result = append(result, reg)
	}

	return result, nil
}

// IsRegistered checks if a contract is registered
func (s *ContractRegistrationService) IsRegistered(address common.Address) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.registrations[address]
	return exists
}

// LoadRegistrations loads registrations from storage
func (s *ContractRegistrationService) LoadRegistrations(ctx context.Context) error {
	if s.storage == nil {
		return nil
	}

	registrations, err := s.storage.ListContractRegistrations(ctx)
	if err != nil {
		return fmt.Errorf("failed to load registrations: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, reg := range registrations {
		// Register with parser
		if err := s.parser.RegisterContractABI(reg.Address, reg.Name, reg.ABI); err != nil {
			// Log error but continue
			continue
		}
		s.registrations[reg.Address] = reg
	}

	return nil
}

// ContractEvent represents an event from a registered contract
type ContractEvent struct {
	Contract    common.Address         `json:"contract"`
	EventName   string                 `json:"eventName"`
	BlockNumber uint64                 `json:"blockNumber"`
	TxHash      common.Hash            `json:"txHash"`
	LogIndex    uint                   `json:"logIndex"`
	Data        map[string]interface{} `json:"data"`
	Timestamp   uint64                 `json:"timestamp"`
}

// ContractEventFilter for querying contract events
type ContractEventFilter struct {
	Contract    *common.Address `json:"contract"`
	EventNames  []string        `json:"eventNames"`
	FromBlock   uint64          `json:"fromBlock"`
	ToBlock     uint64          `json:"toBlock"`
	Limit       int             `json:"limit"`
	Offset      int             `json:"offset"`
}

// ValidateABI validates an ABI JSON string
func ValidateABI(abiJSON string) error {
	var abiData []interface{}
	if err := json.Unmarshal([]byte(abiJSON), &abiData); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	for _, item := range abiData {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		itemType, ok := itemMap["type"].(string)
		if !ok {
			continue
		}

		// Validate event entries
		if itemType == "event" {
			if _, hasName := itemMap["name"]; !hasName {
				return fmt.Errorf("event missing name field")
			}
		}
	}

	return nil
}

// ExtractEventsFromABI extracts event names from ABI JSON
func ExtractEventsFromABI(abiJSON string) ([]string, error) {
	var abiData []interface{}
	if err := json.Unmarshal([]byte(abiJSON), &abiData); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	var events []string
	for _, item := range abiData {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		itemType, ok := itemMap["type"].(string)
		if !ok || itemType != "event" {
			continue
		}

		name, ok := itemMap["name"].(string)
		if ok {
			events = append(events, name)
		}
	}

	return events, nil
}
