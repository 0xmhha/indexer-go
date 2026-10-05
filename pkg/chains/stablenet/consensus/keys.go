package consensus

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

// Key prefixes of WBFT data.
const (
	prefixWBFTExtra             = "/data/wbft/extra/"
	prefixWBFTEpoch             = "/data/wbft/epoch/"
	prefixWBFTValidatorStats    = "/data/wbft/validator/stats/"
	prefixWBFTValidatorActivity = "/data/wbft/validator/activity/"
	prefixIdxWBFTSignerPrepare  = "/index/wbft/signers/prepare/"
	prefixIdxWBFTSignerCommit   = "/index/wbft/signers/commit/"
	keyLatestEpoch              = "/meta/wbft/latest_epoch"
)

// KeyspaceOwner is the keyspace owner of WBFT data (the stablenet.wbft
// feature).
const KeyspaceOwner = "stablenet.wbft"

func init() {
	storage.RegisterKeyspace(KeyspaceOwner, storage.ChainData, "/data/wbft/", "/index/wbft/", "/meta/wbft/")
}

// WBFTBlockExtraKey returns the key for storing WBFT extra data for a block
// Format: /data/wbft/extra/{blockNumber}
func WBFTBlockExtraKey(blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", prefixWBFTExtra, blockNumber))
}

// WBFTEpochKey returns the key for storing epoch information
// Format: /data/wbft/epoch/{epochNumber}
func WBFTEpochKey(epochNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", prefixWBFTEpoch, epochNumber))
}

// LatestEpochKey returns the key for storing latest epoch number
func LatestEpochKey() []byte {
	return []byte(keyLatestEpoch)
}

// WBFTValidatorStatsKey returns the key for validator signing statistics
// Format: /data/wbft/validator/stats/{validator}/{fromBlock}_{toBlock}
func WBFTValidatorStatsKey(validator common.Address, fromBlock, toBlock uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d_%020d", prefixWBFTValidatorStats, validator.Hex(), fromBlock, toBlock))
}

// WBFTValidatorActivityKey returns the key for validator signing activity at a block
// Format: /data/wbft/validator/activity/{validator}/{blockNumber}
func WBFTValidatorActivityKey(validator common.Address, blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d", prefixWBFTValidatorActivity, validator.Hex(), blockNumber))
}

// WBFTSignerPrepareIndexKey returns the index key for prepare phase signers
// Format: /index/wbft/signers/prepare/{blockNumber}/{validator}
func WBFTSignerPrepareIndexKey(blockNumber uint64, validator common.Address) []byte {
	return []byte(fmt.Sprintf("%s%020d/%s", prefixIdxWBFTSignerPrepare, blockNumber, validator.Hex()))
}

// WBFTSignerCommitIndexKey returns the index key for commit phase signers
// Format: /index/wbft/signers/commit/{blockNumber}/{validator}
func WBFTSignerCommitIndexKey(blockNumber uint64, validator common.Address) []byte {
	return []byte(fmt.Sprintf("%s%020d/%s", prefixIdxWBFTSignerCommit, blockNumber, validator.Hex()))
}

// WBFT key prefix functions for range queries

// WBFTBlockExtraKeyPrefix returns the prefix for all WBFT extra data
func WBFTBlockExtraKeyPrefix() []byte {
	return []byte(prefixWBFTExtra)
}

// WBFTEpochKeyPrefix returns the prefix for all epoch data
func WBFTEpochKeyPrefix() []byte {
	return []byte(prefixWBFTEpoch)
}

// WBFTValidatorStatsKeyPrefix returns the prefix for validator stats by validator
func WBFTValidatorStatsKeyPrefix(validator common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixWBFTValidatorStats, validator.Hex()))
}

// WBFTValidatorActivityKeyPrefix returns the prefix for validator activity by validator
func WBFTValidatorActivityKeyPrefix(validator common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixWBFTValidatorActivity, validator.Hex()))
}

// WBFTValidatorActivityAllKeyPrefix returns the prefix for all validator activities
func WBFTValidatorActivityAllKeyPrefix() []byte {
	return []byte(prefixWBFTValidatorActivity)
}

// WBFTSignerPrepareIndexKeyPrefix returns the prefix for prepare signers by block
func WBFTSignerPrepareIndexKeyPrefix(blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/", prefixIdxWBFTSignerPrepare, blockNumber))
}

// WBFTSignerCommitIndexKeyPrefix returns the prefix for commit signers by block
func WBFTSignerCommitIndexKeyPrefix(blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/", prefixIdxWBFTSignerCommit, blockNumber))
}
