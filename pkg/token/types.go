package token

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// TokenStandard represents the type of token contract
type TokenStandard string

const (
	// StandardUnknown indicates the contract standard could not be determined
	StandardUnknown TokenStandard = "UNKNOWN"
	// StandardERC20 indicates an ERC-20 fungible token
	StandardERC20 TokenStandard = "ERC20"
	// StandardERC721 indicates an ERC-721 non-fungible token
	StandardERC721 TokenStandard = "ERC721"
	// StandardERC1155 indicates an ERC-1155 multi-token
	StandardERC1155 TokenStandard = "ERC1155"
)

// ERC165 Interface IDs for standard detection
// These are calculated as XOR of all function selectors in the interface
const (
	// InterfaceIDERC165 is the interface ID for ERC-165 itself
	// supportsInterface(bytes4)
	InterfaceIDERC165 = "0x01ffc9a7"

	// InterfaceIDERC721 is the interface ID for ERC-721
	// balanceOf(address), ownerOf(uint256), safeTransferFrom(address,address,uint256,bytes),
	// safeTransferFrom(address,address,uint256), transferFrom(address,address,uint256),
	// approve(address,uint256), setApprovalForAll(address,bool), getApproved(uint256),
	// isApprovedForAll(address,address)
	InterfaceIDERC721 = "0x80ac58cd"

	// InterfaceIDERC721Metadata is the interface ID for ERC-721 Metadata extension
	// name(), symbol(), tokenURI(uint256)
	InterfaceIDERC721Metadata = "0x5b5e139f"

	// InterfaceIDERC721Enumerable is the interface ID for ERC-721 Enumerable extension
	// totalSupply(), tokenOfOwnerByIndex(address,uint256), tokenByIndex(uint256)
	InterfaceIDERC721Enumerable = "0x780e9d63"

	// InterfaceIDERC1155 is the interface ID for ERC-1155
	// balanceOf(address,uint256), balanceOfBatch(address[],uint256[]),
	// setApprovalForAll(address,bool), isApprovedForAll(address,address),
	// safeTransferFrom(address,address,uint256,uint256,bytes),
	// safeBatchTransferFrom(address,address,uint256[],uint256[],bytes)
	InterfaceIDERC1155 = "0xd9b67a26"

	// InterfaceIDERC1155MetadataURI is the interface ID for ERC-1155 Metadata URI extension
	// uri(uint256)
	InterfaceIDERC1155MetadataURI = "0x0e89341c"
)

// Function selectors for ERC-20 detection (fallback when ERC-165 not supported)
const (
	// SelectorName is the function selector for name()
	SelectorName = "0x06fdde03"
	// SelectorSymbol is the function selector for symbol()
	SelectorSymbol = "0x95d89b41"
	// SelectorDecimals is the function selector for decimals()
	SelectorDecimals = "0x313ce567"
	// SelectorTotalSupply is the function selector for totalSupply()
	SelectorTotalSupply = "0x18160ddd"
	// SelectorBalanceOf is the function selector for balanceOf(address)
	SelectorBalanceOf = "0x70a08231"
	// SelectorTransfer is the function selector for transfer(address,uint256)
	SelectorTransfer = "0xa9059cbb"
	// SelectorTransferFrom is the function selector for transferFrom(address,address,uint256)
	SelectorTransferFrom = "0x23b872dd"
	// SelectorApprove is the function selector for approve(address,uint256)
	SelectorApprove = "0x095ea7b3"
	// SelectorAllowance is the function selector for allowance(address,address)
	SelectorAllowance = "0xdd62ed3e"

	// SelectorOwnerOf is the function selector for ownerOf(uint256) - ERC721 specific
	SelectorOwnerOf = "0x6352211e"
	// SelectorTokenURI is the function selector for tokenURI(uint256) - ERC721 specific
	SelectorTokenURI = "0xc87b56dd"
	// SelectorURI is the function selector for uri(uint256) - ERC1155 specific
	SelectorURI = "0x0e89341c"
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
	return m.Standard == StandardERC20
}

// IsERC721 returns true if the token is an ERC-721 token
func (m *TokenMetadata) IsERC721() bool {
	return m.Standard == StandardERC721
}

// IsERC1155 returns true if the token is an ERC-1155 token
func (m *TokenMetadata) IsERC1155() bool {
	return m.Standard == StandardERC1155
}

// IsNFT returns true if the token is an NFT (ERC-721 or ERC-1155)
func (m *TokenMetadata) IsNFT() bool {
	return m.Standard == StandardERC721 || m.Standard == StandardERC1155
}

// IsFungible returns true if the token is fungible (ERC-20)
func (m *TokenMetadata) IsFungible() bool {
	return m.Standard == StandardERC20
}

// DetectionResult holds the result of token standard detection
type DetectionResult struct {
	// Standard is the detected token standard
	Standard TokenStandard

	// SupportsERC165 indicates if ERC-165 was used for detection
	SupportsERC165 bool

	// SupportsMetadata indicates if metadata extension is supported
	SupportsMetadata bool

	// SupportsEnumerable indicates if enumerable extension is supported (ERC721 only)
	SupportsEnumerable bool

	// Confidence indicates detection confidence (0.0 - 1.0)
	// 1.0 = ERC165 confirmed, < 1.0 = bytecode/signature fallback
	Confidence float64

	// Error holds any error encountered during detection
	Error error
}

// MetadataResult holds the result of metadata fetching
type MetadataResult struct {
	// Name is the token name
	Name string

	// Symbol is the token symbol
	Symbol string

	// Decimals is the number of decimals (ERC20 only)
	Decimals uint8

	// TotalSupply is the total supply (ERC20 only)
	TotalSupply *big.Int

	// BaseURI is the base URI for metadata (NFTs only)
	BaseURI string

	// Errors holds any errors encountered during fetching (partial success allowed)
	Errors map[string]error
}

// HasErrors returns true if there were any errors during metadata fetching
func (r *MetadataResult) HasErrors() bool {
	return len(r.Errors) > 0
}
