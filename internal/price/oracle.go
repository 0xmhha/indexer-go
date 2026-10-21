package price

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Oracle provides token price information
type Oracle interface {
	// IsAvailable checks if the price oracle is available
	IsAvailable() bool

	// GetTokenPrice returns the price of a token in native coin (wei)
	// Returns nil if price is not available
	GetTokenPrice(ctx context.Context, tokenAddress common.Address) (*big.Int, error)

	// GetNativePrice returns the price of native coin in USD (scaled by 1e8)
	// Returns nil if price is not available
	GetNativePrice(ctx context.Context) (*big.Int, error)

	// GetTokenValue calculates the value of tokens in native coin
	// amount is the token amount, decimals is the token's decimal places
	// Returns nil if price is not available
	GetTokenValue(ctx context.Context, tokenAddress common.Address, amount *big.Int, decimals uint8) (*big.Int, error)
}

// NoOpOracle is a placeholder oracle that returns no prices
// Use this when the Price Oracle contract is not deployed
type NoOpOracle struct{}

// NewNoOpOracle creates a new no-op oracle
func NewNoOpOracle() *NoOpOracle {
	return &NoOpOracle{}
}

// IsAvailable returns false - oracle is not available
func (o *NoOpOracle) IsAvailable() bool {
	return false
}

// GetTokenPrice returns nil - price not available
func (o *NoOpOracle) GetTokenPrice(ctx context.Context, tokenAddress common.Address) (*big.Int, error) {
	return nil, nil
}

// GetNativePrice returns nil - price not available
func (o *NoOpOracle) GetNativePrice(ctx context.Context) (*big.Int, error) {
	return nil, nil
}

// GetTokenValue returns nil - value calculation not available
func (o *NoOpOracle) GetTokenValue(ctx context.Context, tokenAddress common.Address, amount *big.Int, decimals uint8) (*big.Int, error) {
	return nil, nil
}
