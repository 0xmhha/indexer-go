package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// Key prefixes for different data types
const (
	prefixMeta       = "/meta/"
	prefixData       = "/data/"
	prefixIndex      = "/index/"
	prefixBlocks     = "/data/blocks/"
	prefixTxs        = "/data/txs/"
	prefixReceipts   = "/data/receipts/"
	prefixTxHash     = "/index/txh/"
	prefixAddr       = "/index/addr/"
	prefixBlockHash  = "/index/blockh/"
)

// Metadata keys
const (
	keyLatestHeight      = "/meta/lh"
	keyBlockCount        = "/meta/bc"
	keyTransactionCount  = "/meta/tc"
)

// LatestHeightKey returns the key for storing latest indexed height
func LatestHeightKey() []byte {
	return []byte(keyLatestHeight)
}

// BlockKey returns the key for storing a block at given height
// Format: /data/blocks/{height}
func BlockKey(height uint64) []byte {
	return []byte(fmt.Sprintf("%s%d", prefixBlocks, height))
}

// TransactionKey returns the key for storing a transaction
// Format: /data/txs/{height}/{index}
func TransactionKey(height uint64, txIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%d/%d", prefixTxs, height, txIndex))
}

// ReceiptKey returns the key for storing a transaction receipt
// Format: /data/receipts/{txhash}
func ReceiptKey(txHash common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixReceipts, txHash.Hex()))
}

// TransactionHashIndexKey returns the key for transaction hash index
// Format: /index/txh/{txhash}
func TransactionHashIndexKey(txHash common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixTxHash, txHash.Hex()))
}

// BlockHashIndexKey returns the key for block hash index
// Format: /index/blockh/{blockhash}
func BlockHashIndexKey(blockHash common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixBlockHash, blockHash.Hex()))
}

// AddressTransactionKey returns the key for address-transaction index
// Format: /index/addr/{address}/{seq}
// Uses zero-padded fixed-width format for proper lexicographic sorting
func AddressTransactionKey(addr common.Address, seq uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d", prefixAddr, addr.Hex(), seq))
}

// ParseBlockKey parses a block key and returns the height
func ParseBlockKey(key []byte) (uint64, error) {
	keyStr := string(key)
	if !strings.HasPrefix(keyStr, prefixBlocks) {
		return 0, fmt.Errorf("invalid block key prefix: %s", keyStr)
	}

	heightStr := strings.TrimPrefix(keyStr, prefixBlocks)
	if heightStr == "" {
		return 0, fmt.Errorf("invalid block key: missing height")
	}

	height, err := strconv.ParseUint(heightStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid block key: %w", err)
	}

	return height, nil
}

// ParseTransactionKey parses a transaction key and returns height and index
func ParseTransactionKey(key []byte) (uint64, uint64, error) {
	keyStr := string(key)
	if !strings.HasPrefix(keyStr, prefixTxs) {
		return 0, 0, fmt.Errorf("invalid transaction key prefix: %s", keyStr)
	}

	parts := strings.TrimPrefix(keyStr, prefixTxs)
	segments := strings.Split(parts, "/")
	if len(segments) != 2 {
		return 0, 0, fmt.Errorf("invalid transaction key format: %s", keyStr)
	}

	height, err := strconv.ParseUint(segments[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid transaction key height: %w", err)
	}

	txIndex, err := strconv.ParseUint(segments[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid transaction key index: %w", err)
	}

	return height, txIndex, nil
}

// EncodeUint64 encodes uint64 to bytes in big-endian format
func EncodeUint64(n uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, n)
	return buf
}

// DecodeUint64 decodes bytes to uint64 in big-endian format
func DecodeUint64(data []byte) (uint64, error) {
	if len(data) != 8 {
		return 0, fmt.Errorf("invalid uint64 data length: %d", len(data))
	}
	return binary.BigEndian.Uint64(data), nil
}

// BlockKeyRange returns the key range for iterating blocks
// Returns [start, end) where end is exclusive
func BlockKeyRange(startHeight, endHeight uint64) ([]byte, []byte) {
	start := BlockKey(startHeight)
	end := BlockKey(endHeight + 1)
	return start, end
}

// AddressTransactionKeyPrefix returns the key prefix for an address
// Used for iterating all transactions for an address
func AddressTransactionKeyPrefix(addr common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixAddr, addr.Hex()))
}

// BlockTimestampKey returns the key for timestamp index
// Format: /index/time/{timestamp}/{height}
// Uses zero-padded fixed-width format for proper lexicographic sorting
func BlockTimestampKey(timestamp uint64, height uint64) []byte {
	return []byte(fmt.Sprintf("/index/time/%020d/%020d", timestamp, height))
}

// BlockTimestampKeyPrefix returns the prefix for all timestamp indexes
func BlockTimestampKeyPrefix() []byte {
	return []byte("/index/time/")
}

// AddressBalanceKey returns the key for an address balance at a specific block
// Format: /index/balance/{address}/history/{seq}
// Uses zero-padded fixed-width format for proper lexicographic sorting
func AddressBalanceKey(addr common.Address, seq uint64) []byte {
	return []byte(fmt.Sprintf("/index/balance/%s/history/%020d", addr.Hex(), seq))
}

// AddressBalanceLatestKey returns the key for the latest balance of an address
// Format: /index/balance/{address}/latest
func AddressBalanceLatestKey(addr common.Address) []byte {
	return []byte(fmt.Sprintf("/index/balance/%s/latest", addr.Hex()))
}

// AddressBalanceKeyPrefix returns the prefix for balance history of an address
func AddressBalanceKeyPrefix(addr common.Address) []byte {
	return []byte(fmt.Sprintf("/index/balance/%s/history/", addr.Hex()))
}

// BlockCountKey returns the key for total block count
func BlockCountKey() []byte {
	return []byte(keyBlockCount)
}

// TransactionCountKey returns the key for total transaction count
func TransactionCountKey() []byte {
	return []byte(keyTransactionCount)
}

// HasPrefix checks if key has the given prefix
func HasPrefix(key, prefix []byte) bool {
	return bytes.HasPrefix(key, prefix)
}

// IsMetadataKey checks if key is a metadata key
func IsMetadataKey(key []byte) bool {
	return HasPrefix(key, []byte(prefixMeta))
}

// IsDataKey checks if key is a data key
func IsDataKey(key []byte) bool {
	return HasPrefix(key, []byte(prefixData))
}

// IsIndexKey checks if key is an index key
func IsIndexKey(key []byte) bool {
	return HasPrefix(key, []byte(prefixIndex))
}
