package storage

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// TokenHolder represents a token holder's balance
type TokenHolder struct {
	TokenAddress  common.Address `json:"tokenAddress"`  // Token contract address
	HolderAddress common.Address `json:"holderAddress"` // Holder address
	Balance       *big.Int       `json:"balance"`       // Token balance
	LastUpdatedAt uint64         `json:"lastUpdatedAt"` // Block number when last updated
}

// TokenHolderStats represents aggregate stats for a token
type TokenHolderStats struct {
	TokenAddress   common.Address `json:"tokenAddress"`   // Token contract address
	HolderCount    int            `json:"holderCount"`    // Number of unique holders
	TransferCount  int            `json:"transferCount"`  // Total number of transfers
	LastActivityAt uint64         `json:"lastActivityAt"` // Block number of last activity
}

// TokenHolderIndexReader defines read operations for token holder indexing
type TokenHolderIndexReader interface {
	// GetTokenHolders retrieves token holders sorted by balance (descending) with pagination.
	// Returns empty slice if no holders found.
	GetTokenHolders(ctx context.Context, token common.Address, limit, offset int) ([]*TokenHolder, error)

	// GetTokenHolderCount returns the number of unique holders for a token.
	GetTokenHolderCount(ctx context.Context, token common.Address) (int, error)

	// GetTokenBalance retrieves the balance of a specific holder for a token.
	// Returns ErrNotFound if the holder has no balance.
	GetTokenBalance(ctx context.Context, token, holder common.Address) (*big.Int, error)

	// GetTokenHolderStats retrieves aggregate statistics for a token.
	// Returns nil if the token has no stats recorded.
	GetTokenHolderStats(ctx context.Context, token common.Address) (*TokenHolderStats, error)

	// GetHolderTokens retrieves all tokens held by a specific address with pagination.
	// Returns empty slice if the address holds no tokens.
	GetHolderTokens(ctx context.Context, holder common.Address, limit, offset int) ([]*TokenHolder, error)
}

// TokenHolderIndexWriter defines write operations for token holder indexing
type TokenHolderIndexWriter interface {
	// UpdateTokenHolder updates the balance for a token holder.
	// Creates a new entry if the holder doesn't exist.
	// If balance becomes zero, the holder is removed from the active holders list.
	UpdateTokenHolder(ctx context.Context, holder *TokenHolder) error

	// UpdateTokenHolderStats updates the statistics for a token.
	// Creates a new entry if the token doesn't have stats yet.
	UpdateTokenHolderStats(ctx context.Context, stats *TokenHolderStats) error

	// ProcessERC20TransferForHolders processes an ERC20 transfer event and updates holder balances.
	// This is called during indexing to maintain holder balances.
	ProcessERC20TransferForHolders(ctx context.Context, transfer *ERC20Transfer) error
}

// TokenHolderIndexReaderWriter combines reader and writer interfaces
type TokenHolderIndexReaderWriter interface {
	TokenHolderIndexReader
	TokenHolderIndexWriter
}
