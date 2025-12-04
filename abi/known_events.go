package abi

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// EventInput represents an input parameter of an event
type EventInput struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Indexed bool   `json:"indexed"`
}

// KnownEvent represents a well-known event signature
type KnownEvent struct {
	Name      string       `json:"name"`
	Signature string       `json:"signature"`
	Inputs    []EventInput `json:"inputs"`
}

// DecodedParam represents a decoded event parameter
type DecodedParam struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Value   string `json:"value"`
	Indexed bool   `json:"indexed"`
}

// DecodedEventLog represents a decoded event log for GraphQL
type DecodedEventLog struct {
	EventName      string         `json:"eventName"`
	EventSignature string         `json:"eventSignature"`
	Params         []DecodedParam `json:"params"`
}

// KnownEvents is a map of event signature hashes to their definitions
// This allows decoding common events without requiring the full ABI
var KnownEvents = map[string]KnownEvent{
	// ERC20 Transfer - 3 topics (from, to indexed), value in data
	"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef": {
		Name:      "Transfer",
		Signature: "Transfer(address,address,uint256)",
		Inputs: []EventInput{
			{Name: "from", Type: "address", Indexed: true},
			{Name: "to", Type: "address", Indexed: true},
			{Name: "value", Type: "uint256", Indexed: false},
		},
	},
	// ERC20 Approval
	"0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925": {
		Name:      "Approval",
		Signature: "Approval(address,address,uint256)",
		Inputs: []EventInput{
			{Name: "owner", Type: "address", Indexed: true},
			{Name: "spender", Type: "address", Indexed: true},
			{Name: "value", Type: "uint256", Indexed: false},
		},
	},
	// ERC721 ApprovalForAll
	"0x17307eab39ab6107e8899845ad3d59bd9653f200f220920489ca2b5937696c31": {
		Name:      "ApprovalForAll",
		Signature: "ApprovalForAll(address,address,bool)",
		Inputs: []EventInput{
			{Name: "owner", Type: "address", Indexed: true},
			{Name: "operator", Type: "address", Indexed: true},
			{Name: "approved", Type: "bool", Indexed: false},
		},
	},
	// WETH Deposit
	"0xe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c": {
		Name:      "Deposit",
		Signature: "Deposit(address,uint256)",
		Inputs: []EventInput{
			{Name: "dst", Type: "address", Indexed: true},
			{Name: "wad", Type: "uint256", Indexed: false},
		},
	},
	// WETH Withdrawal
	"0x7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b65": {
		Name:      "Withdrawal",
		Signature: "Withdrawal(address,uint256)",
		Inputs: []EventInput{
			{Name: "src", Type: "address", Indexed: true},
			{Name: "wad", Type: "uint256", Indexed: false},
		},
	},
	// Uniswap V2 Swap
	"0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822": {
		Name:      "Swap",
		Signature: "Swap(address,uint256,uint256,uint256,uint256,address)",
		Inputs: []EventInput{
			{Name: "sender", Type: "address", Indexed: true},
			{Name: "amount0In", Type: "uint256", Indexed: false},
			{Name: "amount1In", Type: "uint256", Indexed: false},
			{Name: "amount0Out", Type: "uint256", Indexed: false},
			{Name: "amount1Out", Type: "uint256", Indexed: false},
			{Name: "to", Type: "address", Indexed: true},
		},
	},
	// Uniswap V2 Sync
	"0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1": {
		Name:      "Sync",
		Signature: "Sync(uint112,uint112)",
		Inputs: []EventInput{
			{Name: "reserve0", Type: "uint112", Indexed: false},
			{Name: "reserve1", Type: "uint112", Indexed: false},
		},
	},
	// Uniswap V2 Mint (LP)
	"0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f": {
		Name:      "Mint",
		Signature: "Mint(address,uint256,uint256)",
		Inputs: []EventInput{
			{Name: "sender", Type: "address", Indexed: true},
			{Name: "amount0", Type: "uint256", Indexed: false},
			{Name: "amount1", Type: "uint256", Indexed: false},
		},
	},
	// Uniswap V2 Burn (LP)
	"0xdccd412f0b1252819cb1fd330b93224ca42612892bb3f4f789976e6d81936496": {
		Name:      "Burn",
		Signature: "Burn(address,uint256,uint256,address)",
		Inputs: []EventInput{
			{Name: "sender", Type: "address", Indexed: true},
			{Name: "amount0", Type: "uint256", Indexed: false},
			{Name: "amount1", Type: "uint256", Indexed: false},
			{Name: "to", Type: "address", Indexed: true},
		},
	},
	// OwnershipTransferred (Ownable)
	"0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0": {
		Name:      "OwnershipTransferred",
		Signature: "OwnershipTransferred(address,address)",
		Inputs: []EventInput{
			{Name: "previousOwner", Type: "address", Indexed: true},
			{Name: "newOwner", Type: "address", Indexed: true},
		},
	},
	// Paused
	"0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258": {
		Name:      "Paused",
		Signature: "Paused(address)",
		Inputs: []EventInput{
			{Name: "account", Type: "address", Indexed: false},
		},
	},
	// Unpaused
	"0x5db9ee0a495bf2e6ff9c91a7834c1ba4fdd244a5e8aa4e537bd38aeae4b073aa": {
		Name:      "Unpaused",
		Signature: "Unpaused(address)",
		Inputs: []EventInput{
			{Name: "account", Type: "address", Indexed: false},
		},
	},
}

// ERC721 specific events (same signature hash as ERC20 but different parameter structure)
var ERC721Events = map[string]KnownEvent{
	// ERC721 Transfer - 4 topics (from, to, tokenId all indexed)
	"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef": {
		Name:      "Transfer",
		Signature: "Transfer(address,address,uint256)",
		Inputs: []EventInput{
			{Name: "from", Type: "address", Indexed: true},
			{Name: "to", Type: "address", Indexed: true},
			{Name: "tokenId", Type: "uint256", Indexed: true},
		},
	},
	// ERC721 Approval - 4 topics (owner, approved, tokenId all indexed)
	"0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925": {
		Name:      "Approval",
		Signature: "Approval(address,address,uint256)",
		Inputs: []EventInput{
			{Name: "owner", Type: "address", Indexed: true},
			{Name: "approved", Type: "address", Indexed: true},
			{Name: "tokenId", Type: "uint256", Indexed: true},
		},
	},
}

// IsERC721Transfer checks if a log is an ERC721 Transfer event
// ERC721 Transfer has 4 topics (sig + from + to + tokenId), while ERC20 has 3 topics
func IsERC721Transfer(log *types.Log) bool {
	if len(log.Topics) == 0 {
		return false
	}
	transferSig := "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	return log.Topics[0].Hex() == transferSig && len(log.Topics) == 4
}

// IsERC20Transfer checks if a log is an ERC20 Transfer event
// ERC20 Transfer has 3 topics (sig + from + to), value in data
func IsERC20Transfer(log *types.Log) bool {
	if len(log.Topics) == 0 {
		return false
	}
	transferSig := "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	return log.Topics[0].Hex() == transferSig && len(log.Topics) == 3
}

// IsERC721Approval checks if a log is an ERC721 Approval event
func IsERC721Approval(log *types.Log) bool {
	if len(log.Topics) == 0 {
		return false
	}
	approvalSig := "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925"
	return log.Topics[0].Hex() == approvalSig && len(log.Topics) == 4
}

// DecodeKnownEvent attempts to decode a log using known event signatures
// Returns nil if the event is not recognized
func DecodeKnownEvent(log *types.Log) *DecodedEventLog {
	if len(log.Topics) == 0 {
		return nil
	}

	eventSigHash := strings.ToLower(log.Topics[0].Hex())

	// Check for ERC721 events first (have 4 topics for Transfer/Approval)
	if IsERC721Transfer(log) || IsERC721Approval(log) {
		if event, ok := ERC721Events[eventSigHash]; ok {
			return decodeWithKnownEvent(log, event)
		}
	}

	// Check standard known events
	if event, ok := KnownEvents[eventSigHash]; ok {
		return decodeWithKnownEvent(log, event)
	}

	return nil
}

// decodeWithKnownEvent decodes a log using a known event definition
func decodeWithKnownEvent(log *types.Log, event KnownEvent) *DecodedEventLog {
	decoded := &DecodedEventLog{
		EventName:      event.Name,
		EventSignature: event.Signature,
		Params:         make([]DecodedParam, 0, len(event.Inputs)),
	}

	topicIndex := 1 // topics[0] is event signature
	dataOffset := 0

	for _, input := range event.Inputs {
		param := DecodedParam{
			Name:    input.Name,
			Type:    input.Type,
			Indexed: input.Indexed,
		}

		if input.Indexed {
			// Indexed parameters are in topics
			if topicIndex < len(log.Topics) {
				param.Value = decodeTopicValue(log.Topics[topicIndex], input.Type)
				topicIndex++
			} else {
				param.Value = ""
			}
		} else {
			// Non-indexed parameters are in data
			if len(log.Data) >= dataOffset+32 {
				param.Value = decodeDataValue(log.Data, dataOffset, input.Type)
				dataOffset += 32
			} else {
				param.Value = ""
			}
		}

		decoded.Params = append(decoded.Params, param)
	}

	return decoded
}

// decodeTopicValue decodes a value from a topic based on its type
func decodeTopicValue(topic common.Hash, typeName string) string {
	switch typeName {
	case "address":
		// Address is stored in the lower 20 bytes
		return common.BytesToAddress(topic[12:]).Hex()
	case "uint256", "uint128", "uint112", "uint96", "uint64", "uint32", "uint16", "uint8":
		// Unsigned integer
		value := new(big.Int).SetBytes(topic[:])
		return value.String()
	case "int256", "int128", "int64", "int32", "int16", "int8":
		// Signed integer (two's complement)
		value := new(big.Int).SetBytes(topic[:])
		// Check if negative (high bit set)
		if topic[0]&0x80 != 0 {
			// Two's complement: subtract 2^256
			max := new(big.Int).Lsh(big.NewInt(1), 256)
			value.Sub(value, max)
		}
		return value.String()
	case "bool":
		// Boolean is in the last byte
		if topic[31] == 0 {
			return "false"
		}
		return "true"
	case "bytes32":
		return topic.Hex()
	default:
		// Default: return hex representation
		return topic.Hex()
	}
}

// decodeDataValue decodes a value from log data based on its type and offset
func decodeDataValue(data []byte, offset int, typeName string) string {
	if len(data) < offset+32 {
		return ""
	}

	chunk := data[offset : offset+32]

	switch typeName {
	case "address":
		// Address is stored in the lower 20 bytes
		return common.BytesToAddress(chunk[12:]).Hex()
	case "uint256", "uint128", "uint112", "uint96", "uint64", "uint32", "uint16", "uint8":
		// Unsigned integer
		value := new(big.Int).SetBytes(chunk)
		return value.String()
	case "int256", "int128", "int64", "int32", "int16", "int8":
		// Signed integer (two's complement)
		value := new(big.Int).SetBytes(chunk)
		// Check if negative (high bit set)
		if chunk[0]&0x80 != 0 {
			// Two's complement: subtract 2^256
			max := new(big.Int).Lsh(big.NewInt(1), 256)
			value.Sub(value, max)
		}
		return value.String()
	case "bool":
		// Boolean is in the last byte
		if chunk[31] == 0 {
			return "false"
		}
		return "true"
	case "bytes32":
		return fmt.Sprintf("0x%x", chunk)
	default:
		// Default: return hex representation
		return fmt.Sprintf("0x%x", chunk)
	}
}

// GetKnownEventBySignature returns a known event by its signature hash
func GetKnownEventBySignature(sigHash string) (*KnownEvent, bool) {
	sigHash = strings.ToLower(sigHash)
	if event, ok := KnownEvents[sigHash]; ok {
		return &event, true
	}
	return nil, false
}

// GetAllKnownEventSignatures returns all known event signature hashes
func GetAllKnownEventSignatures() []string {
	sigs := make([]string, 0, len(KnownEvents))
	for sig := range KnownEvents {
		sigs = append(sigs, sig)
	}
	return sigs
}
