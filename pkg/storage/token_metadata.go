package storage

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// TokenStandard represents the type of token contract
type TokenStandard string

const (
	// TokenStandardUnknown indicates the contract standard could not be determined
	TokenStandardUnknown TokenStandard = "UNKNOWN"
	// TokenStandardERC20 indicates an ERC-20 fungible token
	TokenStandardERC20 TokenStandard = "ERC20"
	// TokenStandardERC721 indicates an ERC-721 non-fungible token
	TokenStandardERC721 TokenStandard = "ERC721"
	// TokenStandardERC1155 indicates an ERC-1155 multi-token
	TokenStandardERC1155 TokenStandard = "ERC1155"
)

// TokenMetadata represents cached metadata for a token contract
type TokenMetadata struct {
	// Address is the token contract address
	Address common.Address `json:"address"`

	// Standard is the detected token standard (ERC20, ERC721, ERC1155, UNKNOWN)
	Standard TokenStandard `json:"standard"`

	// Name is the token name (from name() function)
	Name string `json:"name"`

	// Symbol is the token symbol (from symbol() function)
	Symbol string `json:"symbol"`

	// Decimals is the number of decimals (ERC20 only, 0 for NFTs)
	Decimals uint8 `json:"decimals"`

	// TotalSupply is the total supply (ERC20 only, may be nil if not available)
	TotalSupply *big.Int `json:"totalSupply,omitempty"`

	// BaseURI is the base URI for token metadata (ERC721/ERC1155, optional)
	BaseURI string `json:"baseURI,omitempty"`

	// DetectedAt is the block height when the token was first detected
	DetectedAt uint64 `json:"detectedAt"`

	// CreatedAt is when this metadata record was created
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when this metadata was last updated
	UpdatedAt time.Time `json:"updatedAt"`

	// SupportsERC165 indicates if the contract supports ERC-165 interface detection
	SupportsERC165 bool `json:"supportsERC165"`

	// SupportsMetadata indicates if the contract supports metadata extension
	SupportsMetadata bool `json:"supportsMetadata"`

	// SupportsEnumerable indicates if ERC721 contract supports enumerable extension
	SupportsEnumerable bool `json:"supportsEnumerable,omitempty"`
}

// IsERC20 returns true if the token is an ERC-20 token
func (m *TokenMetadata) IsERC20() bool {
	return m.Standard == TokenStandardERC20
}

// IsERC721 returns true if the token is an ERC-721 token
func (m *TokenMetadata) IsERC721() bool {
	return m.Standard == TokenStandardERC721
}

// IsERC1155 returns true if the token is an ERC-1155 token
func (m *TokenMetadata) IsERC1155() bool {
	return m.Standard == TokenStandardERC1155
}

// IsNFT returns true if the token is an NFT (ERC-721 or ERC-1155)
func (m *TokenMetadata) IsNFT() bool {
	return m.Standard == TokenStandardERC721 || m.Standard == TokenStandardERC1155
}

// IsFungible returns true if the token is fungible (ERC-20)
func (m *TokenMetadata) IsFungible() bool {
	return m.Standard == TokenStandardERC20
}

// TokenMetadataReader provides read access to token metadata
type TokenMetadataReader interface {
	// GetTokenMetadata retrieves token metadata by contract address
	// Returns ErrNotFound if the token metadata does not exist
	GetTokenMetadata(ctx context.Context, address common.Address) (*TokenMetadata, error)

	// ListTokensByStandard retrieves tokens filtered by standard with pagination
	// If standard is empty, returns all tokens
	ListTokensByStandard(ctx context.Context, standard TokenStandard, limit, offset int) ([]*TokenMetadata, error)

	// GetTokensCount returns the count of tokens, optionally filtered by standard
	// If standard is empty, returns total count
	GetTokensCount(ctx context.Context, standard TokenStandard) (int, error)

	// SearchTokens searches for tokens by name or symbol (case-insensitive partial match)
	SearchTokens(ctx context.Context, query string, limit int) ([]*TokenMetadata, error)
}

// TokenMetadataWriter provides write access to token metadata
type TokenMetadataWriter interface {
	// SaveTokenMetadata saves or updates token metadata
	SaveTokenMetadata(ctx context.Context, metadata *TokenMetadata) error

	// DeleteTokenMetadata removes token metadata by address
	DeleteTokenMetadata(ctx context.Context, address common.Address) error
}

// TokenMetadataFetcher provides on-demand token metadata fetching from chain
// This is used when token metadata is not found in storage and needs to be fetched in real-time
type TokenMetadataFetcher interface {
	// FetchTokenMetadata fetches token metadata from the blockchain
	// Returns nil if the address is not a token contract
	// The fetcher should detect the token standard and fetch name, symbol, decimals, etc.
	FetchTokenMetadata(ctx context.Context, address common.Address) (*TokenMetadata, error)
}
