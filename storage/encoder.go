package storage

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// TxLocation represents the location of a transaction in the blockchain
type TxLocation struct {
	BlockHeight uint64
	TxIndex     uint64
	BlockHash   common.Hash
}

// EncodeBlock encodes a block using RLP
func EncodeBlock(block *types.Block) ([]byte, error) {
	if block == nil {
		return nil, fmt.Errorf("block cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, block); err != nil {
		return nil, fmt.Errorf("failed to encode block: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeBlock decodes a block from RLP
func DecodeBlock(data []byte) (*types.Block, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var block types.Block
	if err := rlp.DecodeBytes(data, &block); err != nil {
		return nil, fmt.Errorf("failed to decode block: %w", err)
	}

	return &block, nil
}

// EncodeTransaction encodes a transaction using RLP
func EncodeTransaction(tx *types.Transaction) ([]byte, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, tx); err != nil {
		return nil, fmt.Errorf("failed to encode transaction: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeTransaction decodes a transaction from RLP
func DecodeTransaction(data []byte) (*types.Transaction, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var tx types.Transaction
	if err := rlp.DecodeBytes(data, &tx); err != nil {
		return nil, fmt.Errorf("failed to decode transaction: %w", err)
	}

	return &tx, nil
}

// EncodeReceipt encodes a receipt using RLP
func EncodeReceipt(receipt *types.Receipt) ([]byte, error) {
	if receipt == nil {
		return nil, fmt.Errorf("receipt cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, receipt); err != nil {
		return nil, fmt.Errorf("failed to encode receipt: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeReceipt decodes a receipt from RLP
func DecodeReceipt(data []byte) (*types.Receipt, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var receipt types.Receipt
	if err := rlp.DecodeBytes(data, &receipt); err != nil {
		return nil, fmt.Errorf("failed to decode receipt: %w", err)
	}

	return &receipt, nil
}

// EncodeTxLocation encodes a TxLocation using RLP
func EncodeTxLocation(loc *TxLocation) ([]byte, error) {
	if loc == nil {
		return nil, fmt.Errorf("location cannot be nil")
	}

	var buf bytes.Buffer
	if err := rlp.Encode(&buf, loc); err != nil {
		return nil, fmt.Errorf("failed to encode location: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeTxLocation decodes a TxLocation from RLP
func DecodeTxLocation(data []byte) (*TxLocation, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var loc TxLocation
	if err := rlp.DecodeBytes(data, &loc); err != nil {
		return nil, fmt.Errorf("failed to decode location: %w", err)
	}

	return &loc, nil
}
