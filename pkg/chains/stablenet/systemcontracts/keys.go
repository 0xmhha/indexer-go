package systemcontracts

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

// Key prefixes of system contract data.
const (
	// System contracts data prefixes
	prefixSysContracts          = "/data/syscontracts/"
	prefixSysMint               = "/data/syscontracts/mint/"
	prefixSysBurn               = "/data/syscontracts/burn/"
	prefixSysMinterConfig       = "/data/syscontracts/minterconfig/"
	prefixSysValidator          = "/data/syscontracts/validator/"
	prefixSysProposal           = "/data/syscontracts/proposal/"
	prefixSysVote               = "/data/syscontracts/vote/"
	prefixSysBlacklist          = "/data/syscontracts/blacklist/"
	prefixSysMember             = "/data/syscontracts/member/"
	prefixSysGasTip             = "/data/syscontracts/gastip/"
	prefixSysEmergency          = "/data/syscontracts/emergency/"
	prefixSysDepositMint        = "/data/syscontracts/depositmint/"
	prefixSysMaxProposals       = "/data/syscontracts/maxproposals/"
	prefixSysProposalSkipped    = "/data/syscontracts/proposalskipped/"
	prefixSysAuthorizedAccounts = "/data/syscontracts/authorizedaccounts/"

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

// KeyspaceOwner is the keyspace owner of system contract data (the
// stablenet.system_contracts feature).
const KeyspaceOwner = "stablenet.system_contracts"

func init() {
	storage.RegisterKeyspace(KeyspaceOwner, storage.ChainData, prefixSysContracts, prefixIdxSysContracts)
}

// System contract key functions

// MintEventKey returns the key for storing a mint event
// Format: /data/syscontracts/mint/{blockNumber}/{txIndex}/{logIndex}
func MintEventKey(blockNumber, txIndex, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/%06d/%06d", prefixSysMint, blockNumber, txIndex, logIndex))
}

// BurnEventKey returns the key for storing a burn event
// Format: /data/syscontracts/burn/{blockNumber}/{txIndex}/{logIndex}
func BurnEventKey(blockNumber, txIndex, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/%06d/%06d", prefixSysBurn, blockNumber, txIndex, logIndex))
}

// MinterConfigEventKey returns the key for storing a minter config event
// Format: /data/syscontracts/minterconfig/{minter}/{blockNumber}
func MinterConfigEventKey(minter common.Address, blockNumber, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixSysMinterConfig, minter.Hex(), blockNumber, logIndex))
}

// ValidatorChangeEventKey returns the key for storing a validator change event
// Format: /data/syscontracts/validator/{validator}/{blockNumber}
func ValidatorChangeEventKey(validator common.Address, blockNumber, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixSysValidator, validator.Hex(), blockNumber, logIndex))
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
func BlacklistEventKey(address common.Address, blockNumber, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixSysBlacklist, address.Hex(), blockNumber, logIndex))
}

// MemberChangeEventKey returns the key for storing a member change event
// Format: /data/syscontracts/member/{contract}/{blockNumber}/{txIndex}
func MemberChangeEventKey(contract common.Address, blockNumber, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixSysMember, contract.Hex(), blockNumber, logIndex))
}

// GasTipUpdateEventKey returns the key for storing a gas tip update event
// Format: /data/syscontracts/gastip/{blockNumber}/{txIndex}
func GasTipUpdateEventKey(blockNumber, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/%06d", prefixSysGasTip, blockNumber, logIndex))
}

// EmergencyPauseEventKey returns the key for storing an emergency pause event
// Format: /data/syscontracts/emergency/{contract}/{blockNumber}/{txIndex}
func EmergencyPauseEventKey(contract common.Address, blockNumber, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixSysEmergency, contract.Hex(), blockNumber, logIndex))
}

// DepositMintProposalKey returns the key for storing a deposit mint proposal
// Format: /data/syscontracts/depositmint/{proposalId}
func DepositMintProposalKey(proposalId string) []byte {
	return []byte(fmt.Sprintf("%s%s", prefixSysDepositMint, proposalId))
}

// MaxProposalsUpdateEventKey returns the key for storing a max proposals update event
// Format: /data/syscontracts/maxproposals/{contract}/{blockNumber}/{txIndex}
func MaxProposalsUpdateEventKey(contract common.Address, blockNumber, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixSysMaxProposals, contract.Hex(), blockNumber, logIndex))
}

// MaxProposalsUpdateEventKeyPrefix returns the prefix for max proposals update events by contract
func MaxProposalsUpdateEventKeyPrefix(contract common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixSysMaxProposals, contract.Hex()))
}

// ProposalExecutionSkippedEventKey returns the key for storing a proposal execution skipped event
// Format: /data/syscontracts/proposalskipped/{contract}/{blockNumber}/{txIndex}
func ProposalExecutionSkippedEventKey(contract common.Address, blockNumber, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixSysProposalSkipped, contract.Hex(), blockNumber, logIndex))
}

// ProposalExecutionSkippedEventKeyPrefix returns the prefix for proposal execution skipped events by contract
func ProposalExecutionSkippedEventKeyPrefix(contract common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixSysProposalSkipped, contract.Hex()))
}

// AuthorizedAccountEventKey returns the key for an authorized account event
// Format: /data/syscontracts/authorizedaccounts/{contract}/{blockNumber}/{txIndex}
func AuthorizedAccountEventKey(contract common.Address, blockNumber, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d", prefixSysAuthorizedAccounts, contract.Hex(), blockNumber, logIndex))
}

// AuthorizedAccountEventKeyPrefix returns the prefix for authorized account events by contract
func AuthorizedAccountEventKeyPrefix(contract common.Address) []byte {
	return []byte(fmt.Sprintf("%s%s/", prefixSysAuthorizedAccounts, contract.Hex()))
}

// System contract index key functions

// MintMinterIndexKey returns the index key for mints by minter
// Format: /index/syscontracts/mint_minter/{minter}/{blockNumber}
func MintMinterIndexKey(minter common.Address, blockNumber, txIndex, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d/%06d", prefixIdxMintMinter, minter.Hex(), blockNumber, txIndex, logIndex))
}

// MintMinterIndexBound returns the first possible mint index key of minter at
// blockNumber, for range scans.
func MintMinterIndexBound(minter common.Address, blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/", prefixIdxMintMinter, minter.Hex(), blockNumber))
}

// BurnBurnerIndexKey returns the index key for burns by burner
// Format: /index/syscontracts/burn_burner/{burner}/{blockNumber}
func BurnBurnerIndexKey(burner common.Address, blockNumber, txIndex, logIndex uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/%06d/%06d", prefixIdxBurnBurner, burner.Hex(), blockNumber, txIndex, logIndex))
}

// BurnBurnerIndexBound returns the first possible burn index key of burner at
// blockNumber, for range scans.
func BurnBurnerIndexBound(burner common.Address, blockNumber uint64) []byte {
	return []byte(fmt.Sprintf("%s%s/%020d/", prefixIdxBurnBurner, burner.Hex(), blockNumber))
}

// ProposalStatusIndexKey returns the index key for proposals by status
// Format: /index/syscontracts/proposal_status/{contract}/{status}/{proposalId}
func ProposalStatusIndexKey(contract common.Address, status uint8, proposalId string) []byte {
	return []byte(fmt.Sprintf("%s%s/%03d/%s", prefixIdxProposalStatus, contract.Hex(), status, proposalId))
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
	return []byte(fmt.Sprintf("%s%s/%03d/", prefixIdxProposalStatus, contract.Hex(), status))
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
