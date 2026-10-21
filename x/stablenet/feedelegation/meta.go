package feedelegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// Key prefixes of fee delegation metadata.
const (
	prefixMeta       = "/data/feedelegation/"
	prefixPayerIndex = "/index/feedelegation/payer/"
)

// KeyspaceOwner is the keyspace owner of fee delegation metadata (the
// stablenet.fee_delegation feature).
const KeyspaceOwner = "stablenet.fee_delegation"

func init() {
	storage.RegisterKeyspace(KeyspaceOwner, storage.ChainData, "/data/feedelegation/", "/index/feedelegation/")
}

// MetaKey returns the key of a transaction's metadata.
// Format: /data/feedelegation/{txHash}
func MetaKey(txHash common.Hash) []byte {
	return []byte(prefixMeta + txHash.Hex())
}

// PayerIndexKey returns the key of a transaction in its fee payer's index.
// Format: /index/feedelegation/payer/{feePayer}/{blockNumber}/{txHash}
func PayerIndexKey(feePayer common.Address, blockNumber uint64, txHash common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s/%016x/%s", prefixPayerIndex, feePayer.Hex(), blockNumber, txHash.Hex()))
}

// payerPrefix returns the prefix of a fee payer's index.
func payerPrefix(feePayer common.Address) []byte {
	return []byte(prefixPayerIndex + feePayer.Hex() + "/")
}

// TxMeta is the fee delegation part of a fee delegation transaction
// (type 0x16): who paid the gas and the payer's signature.
type TxMeta struct {
	TxHash       common.Hash
	BlockNumber  uint64
	OriginalType uint8 // 0x16
	FeePayer     common.Address
	FeePayerV    *big.Int
	FeePayerR    *big.Int
	FeePayerS    *big.Int
}

// MetaStore keeps fee delegation metadata in the indexer's storage. Inside
// a block transaction (ctx from BeginBlock) its writes commit and roll back
// with the block.
type MetaStore struct {
	db port.KV
}

// NewMetaStore returns a metadata store over db.
func NewMetaStore(db port.KV) *MetaStore { return &MetaStore{db: db} }

// OpenMetaStore returns a metadata store over s, which must provide
// key-value access (the Pebble storage does).
func OpenMetaStore(s any) (*MetaStore, error) {
	db, ok := s.(port.KV)
	if !ok {
		return nil, fmt.Errorf("storage %T does not support fee delegation metadata", s)
	}
	return NewMetaStore(db), nil
}

// SetTxMeta stores the metadata of a transaction and indexes it under its
// fee payer.
func (m *MetaStore) SetTxMeta(ctx context.Context, meta *TxMeta) error {
	if meta == nil {
		return fmt.Errorf("meta cannot be nil")
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal fee delegation meta: %w", err)
	}
	if err := m.db.Put(ctx, MetaKey(meta.TxHash), data); err != nil {
		return fmt.Errorf("failed to store fee delegation meta: %w", err)
	}
	if err := m.db.Put(ctx, PayerIndexKey(meta.FeePayer, meta.BlockNumber, meta.TxHash), meta.TxHash.Bytes()); err != nil {
		return fmt.Errorf("failed to store fee payer index: %w", err)
	}
	return nil
}

// TxMeta returns the metadata of a transaction, or nil when it is not a
// fee delegation transaction.
func (m *MetaStore) TxMeta(ctx context.Context, txHash common.Hash) (*TxMeta, error) {
	raw, err := m.db.Get(ctx, MetaKey(txHash))
	if errors.Is(err, port.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get fee delegation meta: %w", err)
	}
	var meta TxMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fee delegation meta: %w", err)
	}
	return &meta, nil
}

// PayerTx is one transaction in a fee payer's index.
type PayerTx struct {
	TxHash      common.Hash
	BlockNumber uint64
}

// FeePayerTxs returns one page of the transactions a fee payer paid for,
// oldest first, and the cursor that continues after it ("" after the last
// one). Page.After is such a cursor; a Limit of 0 or less means 100.
func (m *MetaStore) FeePayerTxs(ctx context.Context, feePayer common.Address, page port.Page) ([]PayerTx, string, error) {
	limit := page.Limit
	if limit <= 0 {
		limit = 100
	}
	prefix := payerPrefix(feePayer)
	lower := prefix
	if page.After != "" {
		// Index keys never contain 0x00: start right after the cursor's key.
		lower = append([]byte(string(prefix)+page.After), 0)
	}
	var (
		out     []PayerTx
		skipped int
		last    string
		more    bool
		bad     error
	)
	err := m.db.Scan(ctx, lower, storage.PrefixEnd(prefix), false, func(key, _ []byte) bool {
		if skipped < page.Offset {
			skipped++
			return true
		}
		if len(out) == limit {
			more = true
			return false
		}
		suffix := string(key[len(prefix):])
		tx, err := parsePayerEntry(suffix)
		if err != nil {
			bad = err
			return false
		}
		out = append(out, tx)
		last = suffix
		return true
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to scan fee payer index: %w", err)
	}
	if bad != nil {
		return nil, "", bad
	}
	if !more {
		last = ""
	}
	return out, last, nil
}

// parsePayerEntry reads "{blockNumber:%016x}/{txHash}", the part of a fee
// payer index key after the payer.
func parsePayerEntry(suffix string) (PayerTx, error) {
	block, hash, ok := strings.Cut(suffix, "/")
	n, err := strconv.ParseUint(block, 16, 64)
	if !ok || err != nil || !strings.HasPrefix(hash, "0x") || len(hash) != 2+2*common.HashLength {
		return PayerTx{}, fmt.Errorf("malformed fee payer index entry %q", suffix)
	}
	return PayerTx{TxHash: common.HexToHash(hash), BlockNumber: n}, nil
}
