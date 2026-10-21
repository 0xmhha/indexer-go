package token

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// EthClient defines the interface for Ethereum client operations needed by Detector
type EthClient interface {
	// CallContract executes a contract call
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber interface{}) ([]byte, error)
	// CodeAt returns the code of the given account
	CodeAt(ctx context.Context, contract common.Address, blockNumber interface{}) ([]byte, error)
}

// Detector detects token contract standards
type Detector struct {
	client EthClient
	logger *zap.Logger
}

// NewDetector creates a new token standard detector
func NewDetector(client EthClient, logger *zap.Logger) *Detector {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Detector{
		client: client,
		logger: logger,
	}
}

// DetectStandard detects the token standard of a contract
func (d *Detector) DetectStandard(ctx context.Context, address common.Address) *DetectionResult {
	result := &DetectionResult{
		Standard:   StandardUnknown,
		Confidence: 0.0,
	}

	// First, check if contract has code
	code, err := d.client.CodeAt(ctx, address, nil)
	if err != nil {
		result.Error = fmt.Errorf("failed to get contract code: %w", err)
		return result
	}
	if len(code) == 0 {
		result.Error = fmt.Errorf("no code at address %s", address.Hex())
		return result
	}

	// Try ERC-165 detection first (most reliable)
	if d.supportsInterface(ctx, address, InterfaceIDERC165) {
		result.SupportsERC165 = true

		// Check ERC-1155 first (more specific)
		if d.supportsInterface(ctx, address, InterfaceIDERC1155) {
			result.Standard = StandardERC1155
			result.Confidence = 1.0
			result.SupportsMetadata = d.supportsInterface(ctx, address, InterfaceIDERC1155MetadataURI)
			d.logger.Debug("Detected ERC1155 via ERC165",
				zap.String("address", address.Hex()),
				zap.Bool("supportsMetadata", result.SupportsMetadata))
			return result
		}

		// Check ERC-721
		if d.supportsInterface(ctx, address, InterfaceIDERC721) {
			result.Standard = StandardERC721
			result.Confidence = 1.0
			result.SupportsMetadata = d.supportsInterface(ctx, address, InterfaceIDERC721Metadata)
			result.SupportsEnumerable = d.supportsInterface(ctx, address, InterfaceIDERC721Enumerable)
			d.logger.Debug("Detected ERC721 via ERC165",
				zap.String("address", address.Hex()),
				zap.Bool("supportsMetadata", result.SupportsMetadata),
				zap.Bool("supportsEnumerable", result.SupportsEnumerable))
			return result
		}
	}

	// Fallback: Check bytecode for function signatures
	codeHex := hex.EncodeToString(code)

	// Check for ERC-721 specific functions (ownerOf is unique to ERC721)
	if d.hasFunctionSelector(codeHex, SelectorOwnerOf) &&
		d.hasFunctionSelector(codeHex, SelectorBalanceOf) &&
		d.hasFunctionSelector(codeHex, SelectorTransferFrom) {
		result.Standard = StandardERC721
		result.Confidence = 0.8
		result.SupportsMetadata = d.hasFunctionSelector(codeHex, SelectorTokenURI)
		d.logger.Debug("Detected ERC721 via bytecode analysis",
			zap.String("address", address.Hex()),
			zap.Float64("confidence", result.Confidence))
		return result
	}

	// Check for ERC-1155 specific functions (uri is common indicator)
	if d.hasFunctionSelector(codeHex, SelectorURI) &&
		d.hasFunctionSelector(codeHex, SelectorBalanceOf) {
		// Additional check: ERC1155 balanceOf has different signature than ERC20/721
		result.Standard = StandardERC1155
		result.Confidence = 0.7
		result.SupportsMetadata = true
		d.logger.Debug("Detected ERC1155 via bytecode analysis",
			zap.String("address", address.Hex()),
			zap.Float64("confidence", result.Confidence))
		return result
	}

	// Check for ERC-20 functions
	if d.hasFunctionSelector(codeHex, SelectorTransfer) &&
		d.hasFunctionSelector(codeHex, SelectorBalanceOf) &&
		d.hasFunctionSelector(codeHex, SelectorTotalSupply) {
		result.Standard = StandardERC20
		result.Confidence = 0.8
		// Check if it has name/symbol/decimals
		result.SupportsMetadata = d.hasFunctionSelector(codeHex, SelectorName) &&
			d.hasFunctionSelector(codeHex, SelectorSymbol) &&
			d.hasFunctionSelector(codeHex, SelectorDecimals)
		d.logger.Debug("Detected ERC20 via bytecode analysis",
			zap.String("address", address.Hex()),
			zap.Float64("confidence", result.Confidence),
			zap.Bool("supportsMetadata", result.SupportsMetadata))
		return result
	}

	d.logger.Debug("Could not detect token standard",
		zap.String("address", address.Hex()))
	return result
}

// supportsInterface checks if a contract supports a given ERC-165 interface
func (d *Detector) supportsInterface(ctx context.Context, address common.Address, interfaceID string) bool {
	// Build calldata: supportsInterface(bytes4)
	// Function selector: 0x01ffc9a7
	interfaceBytes, err := hex.DecodeString(strings.TrimPrefix(interfaceID, "0x"))
	if err != nil {
		return false
	}

	// Pad to 32 bytes (bytes4 is right-padded in ABI encoding)
	callData := make([]byte, 36)
	selectorBytes, _ := hex.DecodeString("01ffc9a7")
	copy(callData[0:4], selectorBytes)
	copy(callData[4:8], interfaceBytes)
	// Rest is zero-padded

	result, err := d.client.CallContract(ctx, ethereum.CallMsg{
		To:   &address,
		Data: callData,
	}, nil)

	if err != nil {
		d.logger.Debug("supportsInterface call failed",
			zap.String("address", address.Hex()),
			zap.String("interfaceID", interfaceID),
			zap.Error(err))
		return false
	}

	// Result should be a bool encoded as uint256
	// true = 0x0000...0001
	if len(result) < 32 {
		return false
	}

	// Check if the last byte is 1
	return result[31] == 1
}

// hasFunctionSelector checks if bytecode contains a function selector
func (d *Detector) hasFunctionSelector(codeHex string, selector string) bool {
	// Remove 0x prefix if present
	selector = strings.TrimPrefix(selector, "0x")
	return strings.Contains(codeHex, selector)
}

// IsTokenContract is a quick check to see if a contract might be a token
// This can be used as a pre-filter before full detection
func (d *Detector) IsTokenContract(ctx context.Context, address common.Address) bool {
	code, err := d.client.CodeAt(ctx, address, nil)
	if err != nil || len(code) == 0 {
		return false
	}

	codeHex := hex.EncodeToString(code)

	// Check for common token function signatures
	hasTransfer := d.hasFunctionSelector(codeHex, SelectorTransfer) ||
		d.hasFunctionSelector(codeHex, SelectorTransferFrom)
	hasBalanceOf := d.hasFunctionSelector(codeHex, SelectorBalanceOf)

	return hasTransfer && hasBalanceOf
}
