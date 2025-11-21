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
	prefixMeta      = "/meta/"
	prefixData      = "/data/"
	prefixIndex     = "/index/"
	prefixBlocks    = "/data/blocks/"
	prefixTxs       = "/data/txs/"
	prefixReceipts  = "/data/receipts/"
	prefixTxHash    = "/index/txh/"
	prefixAddr      = "/index/addr/"
	prefixBlockHash = "/index/blockh/"

	// System contracts data prefixes
	prefixSysContracts      = "/data/syscontracts/"
	prefixSysMint           = "/data/syscontracts/mint/"
	prefixSysBurn           = "/data/syscontracts/burn/"
	prefixSysMinterConfig   = "/data/syscontracts/minterconfig/"
	prefixSysValidator      = "/data/syscontracts/validator/"
	prefixSysProposal       = "/data/syscontracts/proposal/"
	prefixSysVote           = "/data/syscontracts/vote/"
	prefixSysBlacklist      = "/data/syscontracts/blacklist/"
	prefixSysMember         = "/data/syscontracts/member/"
	prefixSysGasTip         = "/data/syscontracts/gastip/"
	prefixSysEmergency      = "/data/syscontracts/emergency/"
	prefixSysDepositMint    = "/data/syscontracts/depositmint/"

	// System contracts index prefixes
	prefixIdxSysContracts    = "/index/syscontracts/"
	prefixIdxMintMinter      = "/index/syscontracts/mint_minter/"
	prefixIdxBurnBurner      = "/index/syscontracts/burn_burner/"
	prefixIdxProposalStatus  = "/index/syscontracts/proposal_status/"
	prefixIdxBlacklistActive = "/index/syscontracts/blacklist_active/"
	prefixIdxMinterActive    = "/index/syscontracts/minter_active/"
	prefixIdxValidatorActive = "/index/syscontracts/validator_active/"
	prefixIdxTotalSupply     = "/index/syscontracts/total_supply"
)

// Metadata keys
const (
	keyLatestHeight     = "/meta/lh"
	keyBlockCount       = "/meta/bc"
	keyTransactionCount = "/meta/tc"
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

// System contract key functions

// MintEventKey returns the key for storing a mint event
// Format: /data/syscontracts/mint/{blockNumber}/{txIndex}/{logIndex}
func MintEventKey(blockNumber, txIndex, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/%d/%d", prefixSysMint, blockNumber, txIndex, logIndex))
}

// BurnEventKey returns the key for storing a burn event
// Format: /data/syscontracts/burn/{blockNumber}/{txIndex}/{logIndex}
func BurnEventKey(blockNumber, txIndex, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/%d/%d", prefixSysBurn, blockNumber, txIndex, logIndex))
}

// MinterConfigEventKey returns the key for storing a minter config event
// Format: /data/syscontracts/minterconfig/{minter}/{blockNumber}
func MinterConfigEventKey(minter common.Address, blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d", prefixSysMinterConfig, minter.Hex(), blockNumber))
}

// ValidatorChangeEventKey returns the key for storing a validator change event
// Format: /data/syscontracts/validator/{validator}/{blockNumber}
func ValidatorChangeEventKey(validator common.Address, blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d", prefixSysValidator, validator.Hex(), blockNumber))
}

// ProposalKey returns the key for storing a proposal
// Format: /data/syscontracts/proposal/{contract}/{proposalId}
func ProposalKey(contract common.Address, proposalId string) []byte {
	return []byte(fmt.Sprintf("%s%s/%s", prefixSysProposal, contract.Hex(), proposalId))
}

// ProposalVoteKey returns the key for storing a proposal vote
// Format: /data/syscontracts/vote/{contract}/{proposalId}/{voter}
func ProposalVoteKey(contract common.Address, proposalId string, voter common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/%s/%s", prefixSysVote, contract.Hex(), proposalId, voter.Hex()))
}

// BlacklistEventKey returns the key for storing a blacklist event
// Format: /data/syscontracts/blacklist/{address}/{blockNumber}
func BlacklistEventKey(address common.Address, blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d", prefixSysBlacklist, address.Hex(), blockNumber))
}

// MemberChangeEventKey returns the key for storing a member change event
// Format: /data/syscontracts/member/{contract}/{blockNumber}/{txIndex}
func MemberChangeEventKey(contract common.Address, blockNumber, txIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%d", prefixSysMember, contract.Hex(), blockNumber, txIndex))
}

// GasTipUpdateEventKey returns the key for storing a gas tip update event
// Format: /data/syscontracts/gastip/{blockNumber}/{txIndex}
func GasTipUpdateEventKey(blockNumber, txIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/%d", prefixSysGasTip, blockNumber, txIndex))
}

// EmergencyPauseEventKey returns the key for storing an emergency pause event
// Format: /data/syscontracts/emergency/{contract}/{blockNumber}/{txIndex}
func EmergencyPauseEventKey(contract common.Address, blockNumber, txIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%d", prefixSysEmergency, contract.Hex(), blockNumber, txIndex))
}

// DepositMintProposalKey returns the key for storing a deposit mint proposal
// Format: /data/syscontracts/depositmint/{proposalId}
func DepositMintProposalKey(proposalId string) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixSysDepositMint, proposalId))
}

// System contract index key functions

// MintMinterIndexKey returns the index key for mints by minter
// Format: /index/syscontracts/mint_minter/{minter}/{blockNumber}
func MintMinterIndexKey(minter common.Address, blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d", prefixIdxMintMinter, minter.Hex(), blockNumber))
}

// BurnBurnerIndexKey returns the index key for burns by burner
// Format: /index/syscontracts/burn_burner/{burner}/{blockNumber}
func BurnBurnerIndexKey(burner common.Address, blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d", prefixIdxBurnBurner, burner.Hex(), blockNumber))
}

// ProposalStatusIndexKey returns the index key for proposals by status
// Format: /index/syscontracts/proposal_status/{contract}/{status}/{proposalId}
func ProposalStatusIndexKey(contract common.Address, status uint8, proposalId string) []byte {
	return []byte(fmt.Sprintf("%s%s/%d/%s", prefixIdxProposalStatus, contract.Hex(), status, proposalId))
}

// BlacklistActiveIndexKey returns the index key for active blacklist
// Format: /index/syscontracts/blacklist_active/{address}
func BlacklistActiveIndexKey(address common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixIdxBlacklistActive, address.Hex()))
}

// MinterActiveIndexKey returns the index key for active minters
// Format: /index/syscontracts/minter_active/{address}
func MinterActiveIndexKey(address common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixIdxMinterActive, address.Hex()))
}

// ValidatorActiveIndexKey returns the index key for active validators
// Format: /index/syscontracts/validator_active/{address}
func ValidatorActiveIndexKey(address common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixIdxValidatorActive, address.Hex()))
}

// TotalSupplyKey returns the key for total supply
func TotalSupplyKey() []byte {
	return []byte(prefixIdxTotalSupply)
}

// Key prefix functions for range queries

// MintEventKeyPrefix returns the prefix for all mint events
func MintEventKeyPrefix() []byte {
	return []byte(prefixSysMint)
}

// BurnEventKeyPrefix returns the prefix for all burn events
func BurnEventKeyPrefix() []byte {
	return []byte(prefixSysBurn)
}

// MinterConfigEventKeyPrefix returns the prefix for minter config events by minter
func MinterConfigEventKeyPrefix(minter common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixSysMinterConfig, minter.Hex()))
}

// ValidatorChangeEventKeyPrefix returns the prefix for validator change events by validator
func ValidatorChangeEventKeyPrefix(validator common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixSysValidator, validator.Hex()))
}

// ProposalKeyPrefix returns the prefix for proposals by contract
func ProposalKeyPrefix(contract common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixSysProposal, contract.Hex()))
}

// ProposalVoteKeyPrefix returns the prefix for proposal votes by contract and proposal
func ProposalVoteKeyPrefix(contract common.Address, proposalId string) []byte {
	return []byte(fmt.Sprintf("%s%s/%s/", prefixSysVote, contract.Hex(), proposalId))
}

// BlacklistEventKeyPrefix returns the prefix for blacklist events by address
func BlacklistEventKeyPrefix(address common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixSysBlacklist, address.Hex()))
}

// MemberChangeEventKeyPrefix returns the prefix for member change events by contract
func MemberChangeEventKeyPrefix(contract common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixSysMember, contract.Hex()))
}

// GasTipUpdateEventKeyPrefix returns the prefix for all gas tip update events
func GasTipUpdateEventKeyPrefix() []byte {
	return []byte(prefixSysGasTip)
}

// EmergencyPauseEventKeyPrefix returns the prefix for emergency pause events by contract
func EmergencyPauseEventKeyPrefix(contract common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixSysEmergency, contract.Hex()))
}

// MintMinterIndexKeyPrefix returns the prefix for mint index by minter
func MintMinterIndexKeyPrefix(minter common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxMintMinter, minter.Hex()))
}

// BurnBurnerIndexKeyPrefix returns the prefix for burn index by burner
func BurnBurnerIndexKeyPrefix(burner common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxBurnBurner, burner.Hex()))
}

// ProposalStatusIndexKeyPrefix returns the prefix for proposal status index by contract and status
func ProposalStatusIndexKeyPrefix(contract common.Address, status uint8) []byte {
	return []byte(fmt.Sprintf("%s%s/%d/", prefixIdxProposalStatus, contract.Hex(), status))
}

// BlacklistActiveIndexKeyPrefix returns the prefix for all active blacklist indexes
func BlacklistActiveIndexKeyPrefix() []byte {
	return []byte(prefixIdxBlacklistActive)
}

// MinterActiveIndexKeyPrefix returns the prefix for all active minter indexes
func MinterActiveIndexKeyPrefix() []byte {
	return []byte(prefixIdxMinterActive)
}

// ValidatorActiveIndexKeyPrefix returns the prefix for all active validator indexes
func ValidatorActiveIndexKeyPrefix() []byte {
	return []byte(prefixIdxValidatorActive)
}
