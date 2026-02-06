package storage

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// SetCodeAuthorizationRecord represents an EIP-7702 SetCode authorization
// that was included in a transaction.
type SetCodeAuthorizationRecord struct {
	// Transaction reference
	TxHash      common.Hash `json:"txHash"`
	BlockNumber uint64      `json:"blockNumber"`
	BlockHash   common.Hash `json:"blockHash"`
	TxIndex     uint64      `json:"txIndex"`
	AuthIndex   int         `json:"authIndex"` // Index within AuthList

	// Authorization data
	TargetAddress    common.Address `json:"targetAddress"`    // Address field - code delegation source
	AuthorityAddress common.Address `json:"authorityAddress"` // Recovered signer address
	ChainID          *big.Int       `json:"chainId"`
	Nonce            uint64         `json:"nonce"`

	// Signature components
	YParity uint8    `json:"yParity"`
	R       *big.Int `json:"r"`
	S       *big.Int `json:"s"`

	// Validation result
	Applied bool   `json:"applied"` // Was authorization successfully applied?
	Error   string `json:"error"`   // Validation error message if not applied

	// Timestamp
	Timestamp time.Time `json:"timestamp"`
}

// AddressDelegationState represents the current delegation state for an address.
// An address has a delegation if its code starts with the delegation prefix (0xef0100).
type AddressDelegationState struct {
	Address           common.Address  `json:"address"`
	HasDelegation     bool            `json:"hasDelegation"`
	DelegationTarget  *common.Address `json:"delegationTarget,omitempty"` // nil if no delegation
	LastUpdatedBlock  uint64          `json:"lastUpdatedBlock"`
	LastUpdatedTxHash common.Hash     `json:"lastUpdatedTxHash"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

// AddressSetCodeStats represents SetCode activity statistics for an address.
type AddressSetCodeStats struct {
	Address           common.Address  `json:"address"`
	AsTargetCount     int             `json:"asTargetCount"`     // Times this address was delegation target
	AsAuthorityCount  int             `json:"asAuthorityCount"`  // Times this address signed authorization
	CurrentDelegation *common.Address `json:"currentDelegation"` // Current delegation target (nil if none)
	LastActivityBlock uint64          `json:"lastActivityBlock"`
	LastActivityTime  time.Time       `json:"lastActivityTime"`
}

// SetCodeIndexReader defines read operations for EIP-7702 SetCode indexing
type SetCodeIndexReader interface {
	// GetSetCodeAuthorization retrieves a specific authorization by transaction hash and index.
	// Returns ErrNotFound if the authorization does not exist.
	GetSetCodeAuthorization(ctx context.Context, txHash common.Hash, authIndex int) (*SetCodeAuthorizationRecord, error)

	// GetSetCodeAuthorizationsByTx retrieves all authorizations in a transaction.
	// Returns empty slice if no authorizations found.
	GetSetCodeAuthorizationsByTx(ctx context.Context, txHash common.Hash) ([]*SetCodeAuthorizationRecord, error)

	// GetSetCodeAuthorizationsByTarget retrieves authorizations where address is the target.
	// Results are ordered by block number descending (newest first).
	GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, limit, offset int) ([]*SetCodeAuthorizationRecord, error)

	// GetSetCodeAuthorizationsByAuthority retrieves authorizations where address is the authority (signer).
	// Results are ordered by block number descending (newest first).
	GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, limit, offset int) ([]*SetCodeAuthorizationRecord, error)

	// GetSetCodeAuthorizationsByBlock retrieves all authorizations in a specific block.
	GetSetCodeAuthorizationsByBlock(ctx context.Context, blockNumber uint64) ([]*SetCodeAuthorizationRecord, error)

	// GetAddressSetCodeStats retrieves SetCode statistics for an address.
	// Returns zero-value stats if the address has no SetCode activity.
	GetAddressSetCodeStats(ctx context.Context, address common.Address) (*AddressSetCodeStats, error)

	// GetAddressDelegationState retrieves the current delegation state for an address.
	// Returns state with HasDelegation=false if the address has no delegation.
	GetAddressDelegationState(ctx context.Context, address common.Address) (*AddressDelegationState, error)

	// GetSetCodeAuthorizationsCountByTarget returns the count of authorizations for a target address.
	GetSetCodeAuthorizationsCountByTarget(ctx context.Context, target common.Address) (int, error)

	// GetSetCodeAuthorizationsCountByAuthority returns the count of authorizations by an authority address.
	GetSetCodeAuthorizationsCountByAuthority(ctx context.Context, authority common.Address) (int, error)

	// GetSetCodeTransactionCount returns the total count of SetCode transactions indexed.
	GetSetCodeTransactionCount(ctx context.Context) (int, error)

	// GetRecentSetCodeAuthorizations retrieves the most recent SetCode authorizations.
	// Results are ordered by block number descending (newest first).
	GetRecentSetCodeAuthorizations(ctx context.Context, limit int) ([]*SetCodeAuthorizationRecord, error)
}

// SetCodeIndexWriter defines write operations for EIP-7702 SetCode indexing
type SetCodeIndexWriter interface {
	// SaveSetCodeAuthorization saves a SetCode authorization record.
	// Creates all necessary indexes (target, authority, block, tx).
	SaveSetCodeAuthorization(ctx context.Context, record *SetCodeAuthorizationRecord) error

	// SaveSetCodeAuthorizations saves multiple authorization records in a batch.
	// More efficient than calling SaveSetCodeAuthorization multiple times.
	SaveSetCodeAuthorizations(ctx context.Context, records []*SetCodeAuthorizationRecord) error

	// UpdateAddressDelegationState updates the delegation state for an address.
	UpdateAddressDelegationState(ctx context.Context, state *AddressDelegationState) error

	// IncrementSetCodeStats increments SetCode statistics for an address.
	// asTarget: increment AsTargetCount
	// asAuthority: increment AsAuthorityCount
	IncrementSetCodeStats(ctx context.Context, address common.Address, asTarget, asAuthority bool, blockNumber uint64) error
}

// DelegationPrefix is the 3-byte prefix for EIP-7702 delegation code.
// A delegation is stored as: [0xef, 0x01, 0x00] + [20-byte target address]
var DelegationPrefix = []byte{0xef, 0x01, 0x00}

// DelegationCodeLength is the total length of delegation code (prefix + address)
const DelegationCodeLength = 23

// ParseDelegation checks if the given code is a delegation and returns the target address.
// Returns the target address and true if it's a delegation, otherwise zero address and false.
func ParseDelegation(code []byte) (common.Address, bool) {
	if len(code) != DelegationCodeLength {
		return common.Address{}, false
	}
	if code[0] != DelegationPrefix[0] || code[1] != DelegationPrefix[1] || code[2] != DelegationPrefix[2] {
		return common.Address{}, false
	}
	return common.BytesToAddress(code[3:]), true
}

// AddressToDelegation creates delegation code for a target address.
// The resulting code is 23 bytes: [0xef, 0x01, 0x00] + [20-byte address]
func AddressToDelegation(target common.Address) []byte {
	code := make([]byte, DelegationCodeLength)
	copy(code[:3], DelegationPrefix)
	copy(code[3:], target.Bytes())
	return code
}

// IsDelegation checks if the given code is a valid delegation.
func IsDelegation(code []byte) bool {
	_, ok := ParseDelegation(code)
	return ok
}

// SetCode authorization validation error codes
const (
	SetCodeErrNone                    = ""
	SetCodeErrWrongChainID            = "wrong_chain_id"
	SetCodeErrNonceOverflow           = "nonce_overflow"
	SetCodeErrInvalidSignature        = "invalid_signature"
	SetCodeErrDestinationHasCode      = "destination_has_code"
	SetCodeErrNonceMismatch           = "nonce_mismatch"
	SetCodeErrAuthorityBlacklisted    = "authority_blacklisted"
	SetCodeErrRecoveryFailed          = "recovery_failed"
)
