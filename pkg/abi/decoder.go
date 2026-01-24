package abi

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ContractABI wraps the go-ethereum ABI with additional metadata
type ContractABI struct {
	Address common.Address `json:"address"`
	Name    string         `json:"name"`
	ABI     string         `json:"abi"` // JSON string of the ABI
	parsed  *abi.ABI       `json:"-"`   // Parsed ABI (not serialized)
}

// DecodedLog represents a decoded event log
type DecodedLog struct {
	// Original log data
	Address     common.Address `json:"address"`
	Topics      []common.Hash  `json:"topics"`
	Data        []byte         `json:"data"`
	BlockNumber uint64         `json:"blockNumber"`
	TxHash      common.Hash    `json:"txHash"`
	TxIndex     uint           `json:"txIndex"`
	BlockHash   common.Hash    `json:"blockHash"`
	LogIndex    uint           `json:"logIndex"`
	Removed     bool           `json:"removed"`

	// Decoded data
	EventName string                 `json:"eventName"`
	Args      map[string]interface{} `json:"args"`
}

// DecodedTxInput represents a decoded transaction input
type DecodedTxInput struct {
	// Original data
	To    *common.Address `json:"to"`
	Input []byte          `json:"input"`

	// Decoded data
	MethodName string                 `json:"methodName"`
	Args       map[string]interface{} `json:"args"`
}

// Decoder handles ABI decoding operations
type Decoder struct {
	contracts map[common.Address]*ContractABI
}

// NewDecoder creates a new ABI decoder
func NewDecoder() *Decoder {
	return &Decoder{
		contracts: make(map[common.Address]*ContractABI),
	}
}

// LoadABI loads and parses an ABI for a contract
func (d *Decoder) LoadABI(address common.Address, name string, abiJSON string) error {
	// Parse ABI
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Store contract ABI
	d.contracts[address] = &ContractABI{
		Address: address,
		Name:    name,
		ABI:     abiJSON,
		parsed:  &parsed,
	}

	return nil
}

// UnloadABI removes an ABI from the decoder
func (d *Decoder) UnloadABI(address common.Address) {
	delete(d.contracts, address)
}

// HasABI checks if an ABI is loaded for a contract
func (d *Decoder) HasABI(address common.Address) bool {
	_, exists := d.contracts[address]
	return exists
}

// GetABI returns the ABI for a contract
func (d *Decoder) GetABI(address common.Address) (*ContractABI, error) {
	contractABI, exists := d.contracts[address]
	if !exists {
		return nil, fmt.Errorf("ABI not found for contract %s", address.Hex())
	}
	return contractABI, nil
}

// DecodeLog decodes an event log using the contract's ABI
func (d *Decoder) DecodeLog(log *types.Log) (*DecodedLog, error) {
	// Get contract ABI
	contractABI, exists := d.contracts[log.Address]
	if !exists {
		return nil, fmt.Errorf("ABI not found for contract %s", log.Address.Hex())
	}

	// Logs must have at least one topic (the event signature)
	if len(log.Topics) == 0 {
		return nil, fmt.Errorf("log has no topics")
	}

	// Find the event by topic0 (event signature hash)
	eventID := log.Topics[0]
	event, err := contractABI.parsed.EventByID(eventID)
	if err != nil {
		return nil, fmt.Errorf("event not found for topic %s: %w", eventID.Hex(), err)
	}

	// Decode the log data
	args := make(map[string]interface{})

	// Decode indexed parameters from topics
	var indexed abi.Arguments
	for _, input := range event.Inputs {
		if input.Indexed {
			indexed = append(indexed, input)
		}
	}

	// Topics[1:] contain indexed parameters
	if len(indexed) > 0 {
		err = abi.ParseTopicsIntoMap(args, indexed, log.Topics[1:])
		if err != nil {
			return nil, fmt.Errorf("failed to parse indexed parameters: %w", err)
		}
	}

	// Decode non-indexed parameters from data
	var nonIndexed abi.Arguments
	for _, input := range event.Inputs {
		if !input.Indexed {
			nonIndexed = append(nonIndexed, input)
		}
	}

	if len(nonIndexed) > 0 {
		err = nonIndexed.UnpackIntoMap(args, log.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse non-indexed parameters: %w", err)
		}
	}

	// Convert complex types to JSON-serializable format
	serializedArgs := serializeArgs(args)

	return &DecodedLog{
		Address:     log.Address,
		Topics:      log.Topics,
		Data:        log.Data,
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		TxIndex:     log.TxIndex,
		BlockHash:   log.BlockHash,
		LogIndex:    log.Index,
		Removed:     log.Removed,
		EventName:   event.RawName,
		Args:        serializedArgs,
	}, nil
}

// DecodeTxInput decodes transaction input data using the contract's ABI
func (d *Decoder) DecodeTxInput(to *common.Address, input []byte) (*DecodedTxInput, error) {
	if to == nil {
		return nil, fmt.Errorf("contract address is nil (contract creation)")
	}

	// Get contract ABI
	contractABI, exists := d.contracts[*to]
	if !exists {
		return nil, fmt.Errorf("ABI not found for contract %s", to.Hex())
	}

	// Input must be at least 4 bytes (function selector)
	if len(input) < 4 {
		return nil, fmt.Errorf("input too short: %d bytes", len(input))
	}

	// Extract method ID (first 4 bytes)
	methodID := input[:4]

	// Find method by ID
	method, err := contractABI.parsed.MethodById(methodID)
	if err != nil {
		return nil, fmt.Errorf("method not found for selector %x: %w", methodID, err)
	}

	// Decode parameters
	args := make(map[string]interface{})
	err = method.Inputs.UnpackIntoMap(args, input[4:])
	if err != nil {
		return nil, fmt.Errorf("failed to decode parameters: %w", err)
	}

	// Convert complex types to JSON-serializable format
	serializedArgs := serializeArgs(args)

	return &DecodedTxInput{
		To:         to,
		Input:      input,
		MethodName: method.RawName,
		Args:       serializedArgs,
	}, nil
}

// serializeArgs converts ABI types to JSON-serializable types
func serializeArgs(args map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range args {
		result[key] = serializeValue(value)
	}

	return result
}

// serializeValue converts a single value to JSON-serializable format
func serializeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case *big.Int:
		return v.String()
	case common.Address:
		return v.Hex()
	case common.Hash:
		return v.Hex()
	case []byte:
		return common.Bytes2Hex(v)
	case []interface{}:
		// Array
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = serializeValue(item)
		}
		return result
	case map[string]interface{}:
		// Nested map
		return serializeArgs(v)
	default:
		return value
	}
}

// ValidateABI validates an ABI JSON string
func ValidateABI(abiJSON string) error {
	_, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return fmt.Errorf("invalid ABI: %w", err)
	}
	return nil
}

// GetEventSignature returns the event signature hash for an event
func GetEventSignature(abiJSON string, eventName string) (common.Hash, error) {
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to parse ABI: %w", err)
	}

	event, ok := parsed.Events[eventName]
	if !ok {
		return common.Hash{}, fmt.Errorf("event %s not found in ABI", eventName)
	}

	return event.ID, nil
}

// GetMethodSelector returns the method selector (first 4 bytes) for a function
func GetMethodSelector(abiJSON string, methodName string) ([]byte, error) {
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	method, ok := parsed.Methods[methodName]
	if !ok {
		return nil, fmt.Errorf("method %s not found in ABI", methodName)
	}

	return method.ID, nil
}

// ExtractEventsFromABI extracts event names and signatures from an ABI
func ExtractEventsFromABI(abiJSON string) (map[string]common.Hash, error) {
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	events := make(map[string]common.Hash)
	for name, event := range parsed.Events {
		events[name] = event.ID
	}

	return events, nil
}

// ExtractMethodsFromABI extracts method names and selectors from an ABI
func ExtractMethodsFromABI(abiJSON string) (map[string][]byte, error) {
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	methods := make(map[string][]byte)
	for name, method := range parsed.Methods {
		methods[name] = method.ID
	}

	return methods, nil
}

// MarshalContractABI serializes a ContractABI to JSON
func MarshalContractABI(c *ContractABI) ([]byte, error) {
	return json.Marshal(c)
}

// UnmarshalContractABI deserializes a ContractABI from JSON and parses the ABI
func UnmarshalContractABI(data []byte) (*ContractABI, error) {
	var c ContractABI
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ContractABI: %w", err)
	}

	// Parse the ABI
	parsed, err := abi.JSON(strings.NewReader(c.ABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	c.parsed = &parsed
	return &c, nil
}
