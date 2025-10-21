package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"

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

// EncodeBalanceSnapshot encodes a BalanceSnapshot
// Format: blockNumber (8 bytes) + balance bytes length (8 bytes) + balance bytes + delta bytes length (8 bytes) + delta bytes + txHash (32 bytes)
func EncodeBalanceSnapshot(snapshot *BalanceSnapshot) ([]byte, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot cannot be nil")
	}

	// Encode balance
	balanceBytes := []byte{}
	if snapshot.Balance != nil {
		balanceBytes = snapshot.Balance.Bytes()
	}

	// Encode delta
	deltaBytes := []byte{}
	deltaSign := byte(0)
	if snapshot.Delta != nil {
		if snapshot.Delta.Sign() < 0 {
			deltaSign = 1
			deltaBytes = new(big.Int).Abs(snapshot.Delta).Bytes()
		} else {
			deltaBytes = snapshot.Delta.Bytes()
		}
	}

	// Calculate total size
	totalSize := 8 + 8 + len(balanceBytes) + 1 + 8 + len(deltaBytes) + 32

	buf := make([]byte, totalSize)
	offset := 0

	// Write block number
	binary.BigEndian.PutUint64(buf[offset:offset+8], snapshot.BlockNumber)
	offset += 8

	// Write balance length
	binary.BigEndian.PutUint64(buf[offset:offset+8], uint64(len(balanceBytes)))
	offset += 8

	// Write balance bytes
	copy(buf[offset:offset+len(balanceBytes)], balanceBytes)
	offset += len(balanceBytes)

	// Write delta sign
	buf[offset] = deltaSign
	offset++

	// Write delta length
	binary.BigEndian.PutUint64(buf[offset:offset+8], uint64(len(deltaBytes)))
	offset += 8

	// Write delta bytes
	copy(buf[offset:offset+len(deltaBytes)], deltaBytes)
	offset += len(deltaBytes)

	// Write transaction hash
	copy(buf[offset:offset+32], snapshot.TxHash[:])

	return buf, nil
}

// DecodeBalanceSnapshot decodes a BalanceSnapshot
func DecodeBalanceSnapshot(data []byte) (*BalanceSnapshot, error) {
	if len(data) < 8+8+1+8+32 {
		return nil, fmt.Errorf("data too short: %d bytes", len(data))
	}

	offset := 0

	// Read block number
	blockNumber := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Read balance length
	balanceLen := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Read balance bytes
	var balance *big.Int
	if balanceLen > 0 {
		if offset+int(balanceLen) > len(data) {
			return nil, fmt.Errorf("invalid balance length: %d", balanceLen)
		}
		balance = new(big.Int).SetBytes(data[offset : offset+int(balanceLen)])
		offset += int(balanceLen)
	} else {
		balance = big.NewInt(0)
	}

	// Read delta sign
	if offset >= len(data) {
		return nil, fmt.Errorf("data too short for delta sign")
	}
	deltaSign := data[offset]
	offset++

	// Read delta length
	if offset+8 > len(data) {
		return nil, fmt.Errorf("data too short for delta length")
	}
	deltaLen := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Read delta bytes
	var delta *big.Int
	if deltaLen > 0 {
		if offset+int(deltaLen) > len(data) {
			return nil, fmt.Errorf("invalid delta length: %d", deltaLen)
		}
		delta = new(big.Int).SetBytes(data[offset : offset+int(deltaLen)])
		offset += int(deltaLen)
		if deltaSign == 1 {
			delta = delta.Neg(delta)
		}
	} else {
		delta = big.NewInt(0)
	}

	// Read transaction hash
	if offset+32 > len(data) {
		return nil, fmt.Errorf("data too short for tx hash")
	}
	var txHash common.Hash
	copy(txHash[:], data[offset:offset+32])

	return &BalanceSnapshot{
		BlockNumber: blockNumber,
		Balance:     balance,
		Delta:       delta,
		TxHash:      txHash,
	}, nil
}

// EncodeBigInt encodes a big.Int to bytes
func EncodeBigInt(n *big.Int) []byte {
	if n == nil {
		return []byte{}
	}
	return n.Bytes()
}

// DecodeBigInt decodes bytes to a big.Int
func DecodeBigInt(data []byte) *big.Int {
	if len(data) == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(data)
}
