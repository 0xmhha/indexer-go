package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.BlockReader = (*PebbleStorage)(nil)
	_ port.BlockWriter = (*PebbleStorage)(nil)
)

// SetBlock implements BlockWriter.
func (s *PebbleStorage) SetBlock(ctx context.Context, b *model.Block) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}
	if b == nil {
		return fmt.Errorf("block cannot be nil")
	}

	encoded, err := model.EncodeBlock(b)
	if err != nil {
		return fmt.Errorf("failed to encode block %d: %w", b.Number, err)
	}
	if err := s.kv(ctx).Set(BlockKey(b.Number), encoded, pebble.NoSync); err != nil {
		return fmt.Errorf("failed to set block: %w", err)
	}
	if err := s.kv(ctx).Set(BlockHashIndexKey(b.Hash), EncodeUint64(b.Number), pebble.NoSync); err != nil {
		return fmt.Errorf("failed to set block hash index: %w", err)
	}
	if err := s.kv(ctx).Set(BlockTimestampKey(b.Time, b.Number), EncodeUint64(b.Number), pebble.NoSync); err != nil {
		return fmt.Errorf("failed to set block timestamp index: %w", err)
	}
	for i, tx := range b.Transactions {
		loc := &port.TxLocation{BlockHeight: b.Number, TxIndex: uint64(i), BlockHash: b.Hash}
		if err := s.setModelTransaction(ctx, tx, loc); err != nil {
			return fmt.Errorf("failed to store transaction %d in block %d: %w", i, b.Number, err)
		}
	}
	return nil
}

// setModelTransaction stores a transaction, its hash index and the count.
func (s *PebbleStorage) setModelTransaction(ctx context.Context, tx *model.Transaction, loc *port.TxLocation) error {
	encoded, err := model.EncodeTransaction(tx)
	if err != nil {
		return fmt.Errorf("failed to encode transaction: %w", err)
	}
	locEncoded, err := EncodeTxLocation(loc)
	if err != nil {
		return fmt.Errorf("failed to encode location: %w", err)
	}
	if err := s.kv(ctx).Set(TransactionKey(loc.BlockHeight, loc.TxIndex), encoded, pebble.NoSync); err != nil {
		return fmt.Errorf("failed to set transaction: %w", err)
	}
	if err := s.kv(ctx).Set(TransactionHashIndexKey(tx.Hash), locEncoded, pebble.NoSync); err != nil {
		return fmt.Errorf("failed to set transaction index: %w", err)
	}
	// Update transaction count using atomic counter (avoid DB read)
	newCount := s.addTxCount(ctx, 1)
	if err := s.kv(ctx).Set(TransactionCountKey(), EncodeUint64(newCount), pebble.NoSync); err != nil {
		return fmt.Errorf("failed to update transaction count: %w", err)
	}
	return nil
}

// SetReceipt implements BlockWriter.
func (s *PebbleStorage) SetReceipt(ctx context.Context, r *model.Receipt) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}
	if err := validateModelReceipt(r); err != nil {
		return err
	}
	encoded, err := model.EncodeReceipt(r)
	if err != nil {
		return fmt.Errorf("failed to encode receipt: %w", err)
	}
	if err := s.kv(ctx).Set(ReceiptKey(r.TxHash), encoded, pebble.NoSync); err != nil {
		return err
	}
	// The contract address index is kept for readers that look it up alone.
	if r.ContractAddress != nil {
		if err := s.kv(ctx).Set(ContractAddressKey(r.TxHash), r.ContractAddress.Bytes(), pebble.NoSync); err != nil {
			return fmt.Errorf("failed to store contract address: %w", err)
		}
	}
	return nil
}

func validateModelReceipt(r *model.Receipt) error {
	switch {
	case r == nil:
		return fmt.Errorf("%w: receipt cannot be nil", port.ErrInvalidReceipt)
	case r.TxHash == common.Hash{}:
		return fmt.Errorf("%w: transaction hash is not set", port.ErrInvalidReceipt)
	case r.Status > 1:
		return fmt.Errorf("%w: invalid status %d (expected 0 or 1)", port.ErrInvalidReceipt, r.Status)
	case r.CumulativeGasUsed < r.GasUsed:
		return fmt.Errorf("%w: cumulative gas used (%d) is less than gas used (%d)", port.ErrInvalidReceipt, r.CumulativeGasUsed, r.GasUsed)
	}
	return nil
}

// get reads one value; a missing key is ErrNotFound.
func (s *PebbleStorage) get(ctx context.Context, key []byte, what string) ([]byte, error) {
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get %s: %w", what, err)
	}
	defer closer.Close()
	return append([]byte(nil), value...), nil
}

// GetBlock implements BlockReader.
func (s *PebbleStorage) GetBlock(ctx context.Context, height uint64) (*model.Block, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	value, err := s.get(ctx, BlockKey(height), "block")
	if err != nil {
		return nil, err
	}
	b, err := model.DecodeBlock(value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block %d: %w", height, err)
	}
	return b, nil
}

// GetBlockByHash implements BlockReader.
func (s *PebbleStorage) GetBlockByHash(ctx context.Context, hash common.Hash) (*model.Block, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	value, err := s.get(ctx, BlockHashIndexKey(hash), "block hash index")
	if err != nil {
		return nil, err
	}
	height, err := DecodeUint64(value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block height: %w", err)
	}
	return s.GetBlock(ctx, height)
}

// GetTransaction implements BlockReader.
func (s *PebbleStorage) GetTransaction(ctx context.Context, hash common.Hash) (*model.Transaction, *port.TxLocation, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, nil, err
	}
	locValue, err := s.get(ctx, TransactionHashIndexKey(hash), "transaction location")
	if err != nil {
		return nil, nil, err
	}
	loc, err := DecodeTxLocation(locValue)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode location: %w", err)
	}
	value, err := s.get(ctx, TransactionKey(loc.BlockHeight, loc.TxIndex), "transaction")
	if err != nil {
		return nil, nil, err
	}
	tx, err := model.DecodeTransaction(value)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode transaction: %w", err)
	}
	return tx, loc, nil
}

// GetReceipt implements BlockReader.
func (s *PebbleStorage) GetReceipt(ctx context.Context, hash common.Hash) (*model.Receipt, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	value, err := s.get(ctx, ReceiptKey(hash), "receipt")
	if err != nil {
		return nil, err
	}
	r, err := model.DecodeReceipt(value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode receipt: %w", err)
	}
	return r, nil
}

// blockTxHashes returns the hashes of block height's transactions as the
// chain reports them. Loops over a block's transactions that look up
// receipts or metadata by hash must use these: the go-ethereum view of a
// block (GetBlock) rebuilds types it cannot represent, such as StableNet
// fee delegation (0x16), as another type with another hash.
func (s *PebbleStorage) blockTxHashes(ctx context.Context, height uint64) ([]common.Hash, error) {
	b, err := s.GetBlock(ctx, height)
	if err != nil {
		return nil, err
	}
	out := make([]common.Hash, len(b.Transactions))
	for i, tx := range b.Transactions {
		out[i] = tx.Hash
	}
	return out, nil
}

// GetBlocks implements BlockReader.
func (s *PebbleStorage) GetBlocks(ctx context.Context, start, end uint64) ([]*model.Block, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	if end < start {
		return nil, nil
	}
	blocks := make([]*model.Block, 0, end-start+1)
	for h := start; h <= end; h++ {
		b, err := s.GetBlock(ctx, h)
		if errors.Is(err, port.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to get block %d: %w", h, err)
		}
		blocks = append(blocks, b)
	}
	return blocks, nil
}
