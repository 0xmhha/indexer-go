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
	prefixSysContracts    = "/data/syscontracts/"
	prefixSysMint         = "/data/syscontracts/mint/"
	prefixSysBurn         = "/data/syscontracts/burn/"
	prefixSysMinterConfig = "/data/syscontracts/minterconfig/"
	prefixSysValidator    = "/data/syscontracts/validator/"
	prefixSysProposal     = "/data/syscontracts/proposal/"
	prefixSysVote         = "/data/syscontracts/vote/"
	prefixSysBlacklist    = "/data/syscontracts/blacklist/"
	prefixSysMember       = "/data/syscontracts/member/"
	prefixSysGasTip       = "/data/syscontracts/gastip/"
	prefixSysEmergency    = "/data/syscontracts/emergency/"
	prefixSysDepositMint  = "/data/syscontracts/depositmint/"

	// WBFT data prefixes
	prefixWBFT                  = "/data/wbft/"
	prefixWBFTExtra             = "/data/wbft/extra/"
	prefixWBFTEpoch             = "/data/wbft/epoch/"
	prefixWBFTValidatorStats    = "/data/wbft/validator/stats/"
	prefixWBFTValidatorActivity = "/data/wbft/validator/activity/"

	// WBFT index prefixes
	prefixIdxWBFT              = "/index/wbft/"
	prefixIdxWBFTSignerPrepare = "/index/wbft/signers/prepare/"
	prefixIdxWBFTSignerCommit  = "/index/wbft/signers/commit/"

	// System contracts index prefixes
	prefixIdxSysContracts    = "/index/syscontracts/"
	prefixIdxMintMinter      = "/index/syscontracts/mint_minter/"
	prefixIdxBurnBurner      = "/index/syscontracts/burn_burner/"
	prefixIdxProposalStatus  = "/index/syscontracts/proposal_status/"
	prefixIdxBlacklistActive = "/index/syscontracts/blacklist_active/"
	prefixIdxMinterActive    = "/index/syscontracts/minter_active/"
	prefixIdxValidatorActive = "/index/syscontracts/validator_active/"
	prefixIdxTotalSupply     = "/index/syscontracts/total_supply"

	// Address indexing data prefixes
	prefixContractCreation = "/data/contract/creation/"
	prefixInternalTx       = "/data/internal/"
	prefixERC20Transfer    = "/data/erc20/transfer/"
	prefixERC721Transfer   = "/data/erc721/transfer/"

	// Address indexing index prefixes
	prefixIdxContractCreator  = "/index/contract/creator/"
	prefixIdxContractBlock    = "/index/contract/block/"
	prefixIdxInternalFrom     = "/index/internal/from/"
	prefixIdxInternalTo       = "/index/internal/to/"
	prefixIdxInternalBlock    = "/index/internal/block/"
	prefixIdxERC20Token       = "/index/erc20/token/"
	prefixIdxERC20From        = "/index/erc20/from/"
	prefixIdxERC20To          = "/index/erc20/to/"
	prefixIdxERC721Token      = "/index/erc721/token/"
	prefixIdxERC721From       = "/index/erc721/from/"
	prefixIdxERC721To         = "/index/erc721/to/"
	prefixIdxERC721TokenOwner = "/index/erc721/tokenowner/"

	// Event log data prefixes
	prefixLogs = "/data/logs/"

	// Event log index prefixes
	prefixIdxLogsAddr   = "/index/logs/addr/"
	prefixIdxLogsTopic0 = "/index/logs/topic0/"
	prefixIdxLogsTopic1 = "/index/logs/topic1/"
	prefixIdxLogsTopic2 = "/index/logs/topic2/"
	prefixIdxLogsTopic3 = "/index/logs/topic3/"
	prefixIdxLogsBlock  = "/index/logs/block/"

	// ABI data prefixes
	prefixABI = "/data/abi/"

	// Contract verification data prefixes
	prefixContractVerification = "/data/verification/"
	prefixIdxVerifiedContracts = "/index/verification/verified/"
)

// Metadata keys
const (
	keyLatestHeight     = "/meta/lh"
	keyBlockCount       = "/meta/bc"
	keyTransactionCount = "/meta/tc"
	keyLatestEpoch      = "/meta/wbft/latest_epoch"
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

// WBFT key functions

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

// ========== Address Indexing Key Functions ==========

// Contract Creation Keys

// ContractCreationKey returns the key for storing contract creation data
// Format: /data/contract/creation/{contractAddress}
func ContractCreationKey(contractAddress common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixContractCreation, contractAddress.Hex()))
}

// ContractCreatorIndexKey returns the index key for contracts by creator
// Format: /index/contract/creator/{creatorAddress}/{blockNumber}/{txHash}
func ContractCreatorIndexKey(creator common.Address, blockNumber uint64, txHash common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%s", prefixIdxContractCreator, creator.Hex(), blockNumber, txHash.Hex()))
}

// ContractBlockIndexKey returns the index key for contracts by block
// Format: /index/contract/block/{blockNumber}/{contractAddress}
func ContractBlockIndexKey(blockNumber uint64, contractAddress common.Address) []byte {
	return []byte(fmt.Sprintf("%s%020d/%s", prefixIdxContractBlock, blockNumber, contractAddress.Hex()))
}

// ContractCreatorIndexKeyPrefix returns the prefix for contracts by creator
func ContractCreatorIndexKeyPrefix(creator common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxContractCreator, creator.Hex()))
}

// Internal Transaction Keys

// InternalTransactionKey returns the key for storing internal transaction data
// Format: /data/internal/{txHash}/{index}
func InternalTransactionKey(txHash common.Hash, index int) []byte {
	return []byte(fmt.Sprintf("%s%s/%06d", prefixInternalTx, txHash.Hex(), index))
}

// InternalTransactionKeyPrefix returns the prefix for all internal transactions of a tx
func InternalTransactionKeyPrefix(txHash common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixInternalTx, txHash.Hex()))
}

// InternalTxFromIndexKey returns the index key for internal transactions by from address
// Format: /index/internal/from/{fromAddress}/{blockNumber}/{txHash}
func InternalTxFromIndexKey(from common.Address, blockNumber uint64, txHash common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%s", prefixIdxInternalFrom, from.Hex(), blockNumber, txHash.Hex()))
}

// InternalTxToIndexKey returns the index key for internal transactions by to address
// Format: /index/internal/to/{toAddress}/{blockNumber}/{txHash}
func InternalTxToIndexKey(to common.Address, blockNumber uint64, txHash common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%s", prefixIdxInternalTo, to.Hex(), blockNumber, txHash.Hex()))
}

// InternalTxBlockIndexKey returns the index key for internal transactions by block
// Format: /index/internal/block/{blockNumber}/{txHash}
func InternalTxBlockIndexKey(blockNumber uint64, txHash common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%020d/%s", prefixIdxInternalBlock, blockNumber, txHash.Hex()))
}

// InternalTxFromIndexKeyPrefix returns the prefix for internal transactions by from address
func InternalTxFromIndexKeyPrefix(from common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxInternalFrom, from.Hex()))
}

// InternalTxToIndexKeyPrefix returns the prefix for internal transactions by to address
func InternalTxToIndexKeyPrefix(to common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxInternalTo, to.Hex()))
}

// ERC20 Transfer Keys

// ERC20TransferKey returns the key for storing ERC20 transfer data
// Format: /data/erc20/transfer/{txHash}/{logIndex}
func ERC20TransferKey(txHash common.Hash, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%06d", prefixERC20Transfer, txHash.Hex(), logIndex))
}

// ERC20TokenIndexKey returns the index key for ERC20 transfers by token
// Format: /index/erc20/token/{contractAddress}/{blockNumber}/{logIndex}
func ERC20TokenIndexKey(tokenAddress common.Address, blockNumber uint64, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixIdxERC20Token, tokenAddress.Hex(), blockNumber, logIndex))
}

// ERC20FromIndexKey returns the index key for ERC20 transfers by from address
// Format: /index/erc20/from/{fromAddress}/{blockNumber}/{logIndex}
func ERC20FromIndexKey(from common.Address, blockNumber uint64, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixIdxERC20From, from.Hex(), blockNumber, logIndex))
}

// ERC20ToIndexKey returns the index key for ERC20 transfers by to address
// Format: /index/erc20/to/{toAddress}/{blockNumber}/{logIndex}
func ERC20ToIndexKey(to common.Address, blockNumber uint64, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixIdxERC20To, to.Hex(), blockNumber, logIndex))
}

// ERC20TokenIndexKeyPrefix returns the prefix for ERC20 transfers by token
func ERC20TokenIndexKeyPrefix(tokenAddress common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxERC20Token, tokenAddress.Hex()))
}

// ERC20FromIndexKeyPrefix returns the prefix for ERC20 transfers by from address
func ERC20FromIndexKeyPrefix(from common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxERC20From, from.Hex()))
}

// ERC20ToIndexKeyPrefix returns the prefix for ERC20 transfers by to address
func ERC20ToIndexKeyPrefix(to common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxERC20To, to.Hex()))
}

// ERC721 Transfer Keys

// ERC721TransferKey returns the key for storing ERC721 transfer data
// Format: /data/erc721/transfer/{txHash}/{logIndex}
func ERC721TransferKey(txHash common.Hash, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%06d", prefixERC721Transfer, txHash.Hex(), logIndex))
}

// ERC721TokenIndexKey returns the index key for ERC721 transfers by token
// Format: /index/erc721/token/{contractAddress}/{blockNumber}/{logIndex}
func ERC721TokenIndexKey(tokenAddress common.Address, blockNumber uint64, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixIdxERC721Token, tokenAddress.Hex(), blockNumber, logIndex))
}

// ERC721FromIndexKey returns the index key for ERC721 transfers by from address
// Format: /index/erc721/from/{fromAddress}/{blockNumber}/{logIndex}
func ERC721FromIndexKey(from common.Address, blockNumber uint64, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixIdxERC721From, from.Hex(), blockNumber, logIndex))
}

// ERC721ToIndexKey returns the index key for ERC721 transfers by to address
// Format: /index/erc721/to/{toAddress}/{blockNumber}/{logIndex}
func ERC721ToIndexKey(to common.Address, blockNumber uint64, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixIdxERC721To, to.Hex(), blockNumber, logIndex))
}

// ERC721TokenOwnerKey returns the key for storing current NFT owner
// Format: /index/erc721/tokenowner/{contractAddress}/{tokenId}
func ERC721TokenOwnerKey(tokenAddress common.Address, tokenId string) []byte {
	return []byte(fmt.Sprintf("%s%s/%s", prefixIdxERC721TokenOwner, tokenAddress.Hex(), tokenId))
}

// ERC721TokenIndexKeyPrefix returns the prefix for ERC721 transfers by token
func ERC721TokenIndexKeyPrefix(tokenAddress common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxERC721Token, tokenAddress.Hex()))
}

// ERC721FromIndexKeyPrefix returns the prefix for ERC721 transfers by from address
func ERC721FromIndexKeyPrefix(from common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxERC721From, from.Hex()))
}

// ERC721ToIndexKeyPrefix returns the prefix for ERC721 transfers by to address
func ERC721ToIndexKeyPrefix(to common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxERC721To, to.Hex()))
}

// ========== Event Log Key Functions ==========

// LogKey returns the key for storing a log entry
// Format: /data/logs/{blockNumber}/{txIndex}/{logIndex}
func LogKey(blockNumber uint64, txIndex uint, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%020d/%06d/%06d", prefixLogs, blockNumber, txIndex, logIndex))
}

// LogKeyPrefix returns the prefix for all logs
func LogKeyPrefix() []byte {
	return []byte(prefixLogs)
}

// LogBlockKeyPrefix returns the prefix for logs in a specific block
func LogBlockKeyPrefix(blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/", prefixLogs, blockNumber))
}

// LogAddressIndexKey returns the index key for logs by contract address
// Format: /index/logs/addr/{address}/{blockNumber}/{txIndex}/{logIndex}
func LogAddressIndexKey(address common.Address, blockNumber uint64, txIndex uint, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d/%06d", prefixIdxLogsAddr, address.Hex(), blockNumber, txIndex, logIndex))
}

// LogAddressIndexKeyPrefix returns the prefix for logs by contract address
func LogAddressIndexKeyPrefix(address common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxLogsAddr, address.Hex()))
}

// LogTopic0IndexKey returns the index key for logs by topic 0
// Format: /index/logs/topic0/{topic}/{blockNumber}/{txIndex}/{logIndex}
func LogTopic0IndexKey(topic common.Hash, blockNumber uint64, txIndex uint, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d/%06d", prefixIdxLogsTopic0, topic.Hex(), blockNumber, txIndex, logIndex))
}

// LogTopic0IndexKeyPrefix returns the prefix for logs by topic 0
func LogTopic0IndexKeyPrefix(topic common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxLogsTopic0, topic.Hex()))
}

// LogTopic1IndexKey returns the index key for logs by topic 1
// Format: /index/logs/topic1/{topic}/{blockNumber}/{txIndex}/{logIndex}
func LogTopic1IndexKey(topic common.Hash, blockNumber uint64, txIndex uint, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d/%06d", prefixIdxLogsTopic1, topic.Hex(), blockNumber, txIndex, logIndex))
}

// LogTopic1IndexKeyPrefix returns the prefix for logs by topic 1
func LogTopic1IndexKeyPrefix(topic common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxLogsTopic1, topic.Hex()))
}

// LogTopic2IndexKey returns the index key for logs by topic 2
// Format: /index/logs/topic2/{topic}/{blockNumber}/{txIndex}/{logIndex}
func LogTopic2IndexKey(topic common.Hash, blockNumber uint64, txIndex uint, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d/%06d", prefixIdxLogsTopic2, topic.Hex(), blockNumber, txIndex, logIndex))
}

// LogTopic2IndexKeyPrefix returns the prefix for logs by topic 2
func LogTopic2IndexKeyPrefix(topic common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxLogsTopic2, topic.Hex()))
}

// LogTopic3IndexKey returns the index key for logs by topic 3
// Format: /index/logs/topic3/{topic}/{blockNumber}/{txIndex}/{logIndex}
func LogTopic3IndexKey(topic common.Hash, blockNumber uint64, txIndex uint, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d/%06d", prefixIdxLogsTopic3, topic.Hex(), blockNumber, txIndex, logIndex))
}

// LogTopic3IndexKeyPrefix returns the prefix for logs by topic 3
func LogTopic3IndexKeyPrefix(topic common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixIdxLogsTopic3, topic.Hex()))
}

// LogBlockIndexKey returns the index key for logs by block number
// Format: /index/logs/block/{blockNumber}/{txIndex}/{logIndex}
func LogBlockIndexKey(blockNumber uint64, txIndex uint, logIndex uint) []byte {
	return []byte(fmt.Sprintf("%s%020d/%06d/%06d", prefixIdxLogsBlock, blockNumber, txIndex, logIndex))
}

// LogBlockIndexKeyPrefix returns the prefix for logs in a block
func LogBlockIndexKeyPrefix(blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/", prefixIdxLogsBlock, blockNumber))
}

// LogBlockRangeIndexKeyPrefix returns the prefix for logs in a block range
func LogBlockRangeIndexKeyPrefix(fromBlock uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/", prefixIdxLogsBlock, fromBlock))
}

// ========== ABI Keys ==========

// ABIKey returns the key for storing an ABI definition
// Format: /data/abi/{address}
func ABIKey(address common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixABI, address.Hex()))
}

// ABIKeyPrefix returns the prefix for all ABIs
func ABIKeyPrefix() []byte {
	return []byte(prefixABI)
}

// ========== Contract Verification Keys ==========

// ContractVerificationKey returns the key for storing contract verification data
// Format: /data/verification/{address}
func ContractVerificationKey(address common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixContractVerification, address.Hex()))
}

// ContractVerificationKeyPrefix returns the prefix for all verified contracts
func ContractVerificationKeyPrefix() []byte {
	return []byte(prefixContractVerification)
}

// VerifiedContractIndexKey returns the index key for verified contracts list
// Format: /index/verification/verified/{verifiedAt_timestamp}/{address}
func VerifiedContractIndexKey(verifiedAt int64, address common.Address) []byte {
	return []byte(fmt.Sprintf("%s%020d/%s", prefixIdxVerifiedContracts, verifiedAt, address.Hex()))
}

// VerifiedContractIndexKeyPrefix returns the prefix for all verified contracts index
func VerifiedContractIndexKeyPrefix() []byte {
	return []byte(prefixIdxVerifiedContracts)
}
