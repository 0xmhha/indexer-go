package events

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// EventType represents the type of blockchain event
type EventType string

const (
	// EventTypeBlock represents a new block event
	EventTypeBlock EventType = "block"

	// EventTypeTransaction represents a new transaction event
	EventTypeTransaction EventType = "transaction"
)

// Event is the base interface for all blockchain events
type Event interface {
	// Type returns the event type
	Type() EventType

	// Timestamp returns when the event was created
	Timestamp() time.Time
}

// BlockEvent represents a new block event
type BlockEvent struct {
	// Block data
	Block *types.Block

	// Block number
	Number uint64

	// Block hash
	Hash common.Hash

	// Timestamp when this event was created
	CreatedAt time.Time

	// Number of transactions in the block
	TxCount int
}

// Type implements Event interface
func (e *BlockEvent) Type() EventType {
	return EventTypeBlock
}

// Timestamp implements Event interface
func (e *BlockEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// TransactionEvent represents a new transaction event
type TransactionEvent struct {
	// Transaction data
	Tx *types.Transaction

	// Transaction hash
	Hash common.Hash

	// Block number containing this transaction
	BlockNumber uint64

	// Block hash containing this transaction
	BlockHash common.Hash

	// Transaction index in the block
	Index uint

	// From address (sender)
	From common.Address

	// To address (receiver, nil for contract creation)
	To *common.Address

	// Value transferred
	Value string // big.Int as string to avoid serialization issues

	// Receipt data (optional, may be nil)
	Receipt *types.Receipt

	// Timestamp when this event was created
	CreatedAt time.Time
}

// Type implements Event interface
func (e *TransactionEvent) Type() EventType {
	return EventTypeTransaction
}

// Timestamp implements Event interface
func (e *TransactionEvent) Timestamp() time.Time {
	return e.CreatedAt
}

// NewBlockEvent creates a new block event from a block
func NewBlockEvent(block *types.Block) *BlockEvent {
	return &BlockEvent{
		Block:     block,
		Number:    block.NumberU64(),
		Hash:      block.Hash(),
		CreatedAt: time.Now(),
		TxCount:   len(block.Transactions()),
	}
}

// NewTransactionEvent creates a new transaction event
func NewTransactionEvent(
	tx *types.Transaction,
	blockNumber uint64,
	blockHash common.Hash,
	index uint,
	from common.Address,
	receipt *types.Receipt,
) *TransactionEvent {
	var to *common.Address
	if tx.To() != nil {
		toAddr := *tx.To()
		to = &toAddr
	}

	return &TransactionEvent{
		Tx:          tx,
		Hash:        tx.Hash(),
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
		Index:       index,
		From:        from,
		To:          to,
		Value:       tx.Value().String(),
		Receipt:     receipt,
		CreatedAt:   time.Now(),
	}
}
