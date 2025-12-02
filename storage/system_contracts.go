package storage

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ProposalStatus represents the status of a governance proposal
type ProposalStatus uint8

const (
	ProposalStatusAll      ProposalStatus = 0xFF // Special value for querying all statuses
	ProposalStatusNone     ProposalStatus = iota
	ProposalStatusVoting
	ProposalStatusApproved
	ProposalStatusExecuted
	ProposalStatusCancelled
	ProposalStatusExpired
	ProposalStatusFailed
	ProposalStatusRejected
)

// String returns a human-readable string representation of ProposalStatus
func (s ProposalStatus) String() string {
	switch s {
	case ProposalStatusNone:
		return "none"
	case ProposalStatusVoting:
		return "voting"
	case ProposalStatusApproved:
		return "approved"
	case ProposalStatusExecuted:
		return "executed"
	case ProposalStatusCancelled:
		return "cancelled"
	case ProposalStatusExpired:
		return "expired"
	case ProposalStatusFailed:
		return "failed"
	case ProposalStatusRejected:
		return "rejected"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// MintEvent represents a Mint event from NativeCoinAdapter
type MintEvent struct {
	BlockNumber uint64
	TxHash      common.Hash
	Minter      common.Address
	To          common.Address
	Amount      *big.Int
	Timestamp   uint64
}

// BurnEvent represents a Burn event
type BurnEvent struct {
	BlockNumber uint64
	TxHash      common.Hash
	Burner      common.Address
	Amount      *big.Int
	Timestamp   uint64
	// WithdrawalID is used for GovMinter burn events
	WithdrawalID string
}

// MinterConfigEvent represents Minter configuration changes
type MinterConfigEvent struct {
	BlockNumber uint64
	TxHash      common.Hash
	Minter      common.Address
	Allowance   *big.Int
	Action      string // "configured" or "removed"
	Timestamp   uint64
}

// Proposal represents a governance proposal
type Proposal struct {
	Contract          common.Address
	ProposalID        *big.Int
	Proposer          common.Address
	ActionType        [32]byte
	CallData          []byte
	MemberVersion     *big.Int
	RequiredApprovals uint32
	Approved          uint32
	Rejected          uint32
	Status            ProposalStatus
	CreatedAt         uint64
	ExecutedAt        *uint64
	BlockNumber       uint64
	TxHash            common.Hash
}

// ProposalVote represents a vote on a proposal
type ProposalVote struct {
	Contract    common.Address
	ProposalID  *big.Int
	Voter       common.Address
	Approval    bool
	BlockNumber uint64
	TxHash      common.Hash
	Timestamp   uint64
}

// GasTipUpdateEvent represents a gas tip update from GovValidator
type GasTipUpdateEvent struct {
	BlockNumber uint64
	TxHash      common.Hash
	OldTip      *big.Int
	NewTip      *big.Int
	Updater     common.Address
	Timestamp   uint64
}

// BlacklistEvent represents blacklist changes from GovCouncil
type BlacklistEvent struct {
	BlockNumber uint64
	TxHash      common.Hash
	Account     common.Address
	Action      string // "blacklisted" or "unblacklisted"
	ProposalID  *big.Int
	Timestamp   uint64
}

// ValidatorChangeEvent represents validator changes
type ValidatorChangeEvent struct {
	BlockNumber  uint64
	TxHash       common.Hash
	Validator    common.Address
	Action       string // "added", "removed", "changed"
	OldValidator *common.Address
	Timestamp    uint64
}

// MemberChangeEvent represents member changes in Gov contracts
type MemberChangeEvent struct {
	Contract     common.Address
	BlockNumber  uint64
	TxHash       common.Hash
	Member       common.Address
	Action       string // "added", "removed", "changed"
	OldMember    *common.Address
	TotalMembers uint64
	NewQuorum    uint32
	Timestamp    uint64
}

// EmergencyPauseEvent represents emergency pause/unpause events
type EmergencyPauseEvent struct {
	Contract    common.Address
	BlockNumber uint64
	TxHash      common.Hash
	ProposalID  *big.Int
	Action      string // "paused" or "unpaused"
	Timestamp   uint64
}

// DepositMintProposal represents a deposit mint proposal from GovMinter
type DepositMintProposal struct {
	ProposalID    *big.Int
	Requester     common.Address // The member who proposed the mint
	Beneficiary   common.Address // The recipient of minted tokens
	Amount        *big.Int
	DepositID     string // Note: indexed string is hashed in topics, may need proposal lookup
	BankReference string
	Status        ProposalStatus
	BlockNumber   uint64
	TxHash        common.Hash
	Timestamp     uint64
}

// MaxProposalsUpdateEvent represents MaxProposalsPerMemberUpdated event from GovBase
type MaxProposalsUpdateEvent struct {
	Contract    common.Address
	BlockNumber uint64
	TxHash      common.Hash
	OldMax      uint64
	NewMax      uint64
	Timestamp   uint64
}

// ProposalExecutionSkippedEvent represents ProposalExecutionSkipped event from GovCouncil
type ProposalExecutionSkippedEvent struct {
	Contract    common.Address
	BlockNumber uint64
	TxHash      common.Hash
	Account     common.Address
	ProposalID  *big.Int
	Reason      string
	Timestamp   uint64
}

// SystemContractReader provides read-only access to system contract events
type SystemContractReader interface {
	// NativeCoinAdapter queries
	GetTotalSupply(ctx context.Context) (*big.Int, error)
	GetMintEvents(ctx context.Context, fromBlock, toBlock uint64, minter common.Address, limit, offset int) ([]*MintEvent, error)
	GetBurnEvents(ctx context.Context, fromBlock, toBlock uint64, burner common.Address, limit, offset int) ([]*BurnEvent, error)
	GetActiveMinters(ctx context.Context) ([]common.Address, error)
	GetMinterAllowance(ctx context.Context, minter common.Address) (*big.Int, error)
	GetMinterHistory(ctx context.Context, minter common.Address) ([]*MinterConfigEvent, error)

	// GovValidator queries
	GetActiveValidators(ctx context.Context) ([]common.Address, error)
	GetGasTipHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*GasTipUpdateEvent, error)
	GetValidatorHistory(ctx context.Context, validator common.Address) ([]*ValidatorChangeEvent, error)

	// GovMasterMinter queries
	GetMinterConfigHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*MinterConfigEvent, error)
	GetEmergencyPauseHistory(ctx context.Context, contract common.Address) ([]*EmergencyPauseEvent, error)

	// GovMinter queries
	GetDepositMintProposals(ctx context.Context, fromBlock, toBlock uint64, status ProposalStatus) ([]*DepositMintProposal, error)
	GetBurnHistory(ctx context.Context, fromBlock, toBlock uint64, user common.Address) ([]*BurnEvent, error)

	// GovCouncil queries
	GetBlacklistedAddresses(ctx context.Context) ([]common.Address, error)
	GetBlacklistHistory(ctx context.Context, address common.Address) ([]*BlacklistEvent, error)
	GetAuthorizedAccounts(ctx context.Context) ([]common.Address, error)

	// Generic governance queries
	GetProposals(ctx context.Context, contract common.Address, status ProposalStatus, limit, offset int) ([]*Proposal, error)
	GetProposalById(ctx context.Context, contract common.Address, proposalId *big.Int) (*Proposal, error)
	GetProposalVotes(ctx context.Context, contract common.Address, proposalId *big.Int) ([]*ProposalVote, error)
	GetMemberHistory(ctx context.Context, contract common.Address) ([]*MemberChangeEvent, error)
}

// SystemContractWriter provides write access for system contract event indexing
type SystemContractWriter interface {
	// IndexSystemContractEvent indexes a single system contract event from a log
	IndexSystemContractEvent(ctx context.Context, log *types.Log) error

	// IndexSystemContractEvents indexes multiple system contract events from logs (batch operation)
	IndexSystemContractEvents(ctx context.Context, logs []*types.Log) error

	// Event storage methods
	StoreMintEvent(ctx context.Context, event *MintEvent) error
	StoreBurnEvent(ctx context.Context, event *BurnEvent) error
	StoreMinterConfigEvent(ctx context.Context, event *MinterConfigEvent) error
	StoreProposal(ctx context.Context, proposal *Proposal) error
	UpdateProposalStatus(ctx context.Context, contract common.Address, proposalID *big.Int, status ProposalStatus, executedAt uint64) error
	StoreProposalVote(ctx context.Context, vote *ProposalVote) error
	StoreGasTipUpdateEvent(ctx context.Context, event *GasTipUpdateEvent) error
	StoreBlacklistEvent(ctx context.Context, event *BlacklistEvent) error
	StoreValidatorChangeEvent(ctx context.Context, event *ValidatorChangeEvent) error
	StoreMemberChangeEvent(ctx context.Context, event *MemberChangeEvent) error
	StoreEmergencyPauseEvent(ctx context.Context, event *EmergencyPauseEvent) error
	StoreDepositMintProposal(ctx context.Context, proposal *DepositMintProposal) error
	StoreMaxProposalsUpdateEvent(ctx context.Context, event *MaxProposalsUpdateEvent) error
	StoreProposalExecutionSkippedEvent(ctx context.Context, event *ProposalExecutionSkippedEvent) error
	UpdateTotalSupply(ctx context.Context, delta *big.Int) error
	UpdateActiveMinter(ctx context.Context, minter common.Address, allowance *big.Int, active bool) error
	UpdateActiveValidator(ctx context.Context, validator common.Address, active bool) error
	UpdateBlacklistStatus(ctx context.Context, address common.Address, blacklisted bool) error
}

// SystemContractStorage combines system contract read and write interfaces
type SystemContractStorage interface {
	SystemContractReader
	SystemContractWriter
}
