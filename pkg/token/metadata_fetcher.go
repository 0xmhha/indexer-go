package token

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// MetadataFetcher fetches token metadata from contracts
type MetadataFetcher struct {
	client EthClient
	logger *zap.Logger
}

// NewMetadataFetcher creates a new metadata fetcher
func NewMetadataFetcher(client EthClient, logger *zap.Logger) *MetadataFetcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MetadataFetcher{
		client: client,
		logger: logger,
	}
}

// FetchERC20Metadata fetches metadata for an ERC-20 token
func (f *MetadataFetcher) FetchERC20Metadata(ctx context.Context, address common.Address) *MetadataResult {
	result := &MetadataResult{
		Errors: make(map[string]error),
	}

	// Fetch name
	name, err := f.callStringMethod(ctx, address, SelectorName)
	if err != nil {
		result.Errors["name"] = err
		f.logger.Debug("Failed to fetch ERC20 name",
			zap.String("address", address.Hex()),
			zap.Error(err))
	} else {
		result.Name = name
	}

	// Fetch symbol
	symbol, err := f.callStringMethod(ctx, address, SelectorSymbol)
	if err != nil {
		result.Errors["symbol"] = err
		f.logger.Debug("Failed to fetch ERC20 symbol",
			zap.String("address", address.Hex()),
			zap.Error(err))
	} else {
		result.Symbol = symbol
	}

	// Fetch decimals
	decimals, err := f.callUint8Method(ctx, address, SelectorDecimals)
	if err != nil {
		result.Errors["decimals"] = err
		// Default to 18 for ERC20 if decimals call fails
		result.Decimals = 18
		f.logger.Debug("Failed to fetch ERC20 decimals, defaulting to 18",
			zap.String("address", address.Hex()),
			zap.Error(err))
	} else {
		result.Decimals = decimals
	}

	// Fetch totalSupply (optional)
	totalSupply, err := f.callUint256Method(ctx, address, SelectorTotalSupply)
	if err != nil {
		result.Errors["totalSupply"] = err
		f.logger.Debug("Failed to fetch ERC20 totalSupply",
			zap.String("address", address.Hex()),
			zap.Error(err))
	} else {
		result.TotalSupply = totalSupply
	}

	return result
}

// FetchERC721Metadata fetches metadata for an ERC-721 token
func (f *MetadataFetcher) FetchERC721Metadata(ctx context.Context, address common.Address) *MetadataResult {
	result := &MetadataResult{
		Errors: make(map[string]error),
	}

	// Fetch name
	name, err := f.callStringMethod(ctx, address, SelectorName)
	if err != nil {
		result.Errors["name"] = err
		f.logger.Debug("Failed to fetch ERC721 name",
			zap.String("address", address.Hex()),
			zap.Error(err))
	} else {
		result.Name = name
	}

	// Fetch symbol
	symbol, err := f.callStringMethod(ctx, address, SelectorSymbol)
	if err != nil {
		result.Errors["symbol"] = err
		f.logger.Debug("Failed to fetch ERC721 symbol",
			zap.String("address", address.Hex()),
			zap.Error(err))
	} else {
		result.Symbol = symbol
	}

	// For NFTs, decimals is always 0
	result.Decimals = 0

	// Try to get baseURI by calling tokenURI(0) or tokenURI(1) and extracting base
	// This is best-effort as not all contracts support this
	baseURI := f.tryFetchBaseURI(ctx, address)
	if baseURI != "" {
		result.BaseURI = baseURI
	}

	return result
}

// FetchERC1155Metadata fetches metadata for an ERC-1155 token
func (f *MetadataFetcher) FetchERC1155Metadata(ctx context.Context, address common.Address) *MetadataResult {
	result := &MetadataResult{
		Errors:   make(map[string]error),
		Decimals: 0, // ERC1155 doesn't have decimals
	}

	// ERC1155 doesn't require name() and symbol(), but some implementations have them
	name, err := f.callStringMethod(ctx, address, SelectorName)
	if err == nil {
		result.Name = name
	}

	symbol, err := f.callStringMethod(ctx, address, SelectorSymbol)
	if err == nil {
		result.Symbol = symbol
	}

	// Try to get URI pattern by calling uri(0) or uri(1)
	baseURI := f.tryFetchERC1155URI(ctx, address)
	if baseURI != "" {
		result.BaseURI = baseURI
	}

	return result
}

// FetchMetadata fetches metadata based on the detected standard
func (f *MetadataFetcher) FetchMetadata(ctx context.Context, address common.Address, standard TokenStandard) *MetadataResult {
	switch standard {
	case StandardERC20:
		return f.FetchERC20Metadata(ctx, address)
	case StandardERC721:
		return f.FetchERC721Metadata(ctx, address)
	case StandardERC1155:
		return f.FetchERC1155Metadata(ctx, address)
	default:
		return &MetadataResult{
			Errors: map[string]error{
				"standard": fmt.Errorf("unknown token standard: %s", standard),
			},
		}
	}
}

// callStringMethod calls a contract method that returns a string
func (f *MetadataFetcher) callStringMethod(ctx context.Context, address common.Address, selector string) (string, error) {
	selectorBytes, err := hex.DecodeString(strings.TrimPrefix(selector, "0x"))
	if err != nil {
		return "", fmt.Errorf("invalid selector: %w", err)
	}

	result, err := f.client.CallContract(ctx, ethereum.CallMsg{
		To:   &address,
		Data: selectorBytes,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("contract call failed: %w", err)
	}

	return f.decodeString(result)
}

// callUint8Method calls a contract method that returns a uint8
func (f *MetadataFetcher) callUint8Method(ctx context.Context, address common.Address, selector string) (uint8, error) {
	selectorBytes, err := hex.DecodeString(strings.TrimPrefix(selector, "0x"))
	if err != nil {
		return 0, fmt.Errorf("invalid selector: %w", err)
	}

	result, err := f.client.CallContract(ctx, ethereum.CallMsg{
		To:   &address,
		Data: selectorBytes,
	}, nil)
	if err != nil {
		return 0, fmt.Errorf("contract call failed: %w", err)
	}

	if len(result) < 32 {
		return 0, fmt.Errorf("invalid result length: %d", len(result))
	}

	return result[31], nil
}

// callUint256Method calls a contract method that returns a uint256
func (f *MetadataFetcher) callUint256Method(ctx context.Context, address common.Address, selector string) (*big.Int, error) {
	selectorBytes, err := hex.DecodeString(strings.TrimPrefix(selector, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid selector: %w", err)
	}

	result, err := f.client.CallContract(ctx, ethereum.CallMsg{
		To:   &address,
		Data: selectorBytes,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("contract call failed: %w", err)
	}

	if len(result) < 32 {
		return nil, fmt.Errorf("invalid result length: %d", len(result))
	}

	return new(big.Int).SetBytes(result), nil
}

// callTokenURI calls tokenURI(uint256) for ERC721
func (f *MetadataFetcher) callTokenURI(ctx context.Context, address common.Address, tokenID *big.Int) (string, error) {
	selectorBytes, err := hex.DecodeString(strings.TrimPrefix(SelectorTokenURI, "0x"))
	if err != nil {
		return "", fmt.Errorf("invalid selector: %w", err)
	}

	// Encode tokenID as uint256
	tokenIDBytes := make([]byte, 32)
	tokenID.FillBytes(tokenIDBytes)

	callData := append(selectorBytes, tokenIDBytes...)

	result, err := f.client.CallContract(ctx, ethereum.CallMsg{
		To:   &address,
		Data: callData,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("contract call failed: %w", err)
	}

	return f.decodeString(result)
}

// callURI calls uri(uint256) for ERC1155
func (f *MetadataFetcher) callURI(ctx context.Context, address common.Address, tokenID *big.Int) (string, error) {
	selectorBytes, err := hex.DecodeString(strings.TrimPrefix(SelectorURI, "0x"))
	if err != nil {
		return "", fmt.Errorf("invalid selector: %w", err)
	}

	// Encode tokenID as uint256
	tokenIDBytes := make([]byte, 32)
	tokenID.FillBytes(tokenIDBytes)

	callData := append(selectorBytes, tokenIDBytes...)

	result, err := f.client.CallContract(ctx, ethereum.CallMsg{
		To:   &address,
		Data: callData,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("contract call failed: %w", err)
	}

	return f.decodeString(result)
}

// tryFetchBaseURI tries to fetch base URI by calling tokenURI with token ID 0 or 1
func (f *MetadataFetcher) tryFetchBaseURI(ctx context.Context, address common.Address) string {
	// Try tokenURI(1) first (token 0 might not exist)
	uri, err := f.callTokenURI(ctx, address, big.NewInt(1))
	if err == nil && uri != "" {
		return extractBaseURI(uri)
	}

	// Try tokenURI(0)
	uri, err = f.callTokenURI(ctx, address, big.NewInt(0))
	if err == nil && uri != "" {
		return extractBaseURI(uri)
	}

	return ""
}

// tryFetchERC1155URI tries to fetch URI pattern for ERC1155
func (f *MetadataFetcher) tryFetchERC1155URI(ctx context.Context, address common.Address) string {
	// Try uri(0) first
	uri, err := f.callURI(ctx, address, big.NewInt(0))
	if err == nil && uri != "" {
		return uri
	}

	// Try uri(1)
	uri, err = f.callURI(ctx, address, big.NewInt(1))
	if err == nil && uri != "" {
		return uri
	}

	return ""
}

// decodeString decodes a string from ABI-encoded bytes
func (f *MetadataFetcher) decodeString(data []byte) (string, error) {
	if len(data) < 64 {
		// Might be a non-standard implementation returning raw string
		// Try to decode as raw bytes (remove null bytes)
		return strings.TrimRight(string(data), "\x00"), nil
	}

	// Standard ABI encoding: offset (32 bytes) + length (32 bytes) + data
	// Get offset
	offset := new(big.Int).SetBytes(data[0:32]).Uint64()
	if offset >= uint64(len(data)) {
		return "", fmt.Errorf("invalid string offset")
	}

	// Get length
	if offset+32 > uint64(len(data)) {
		return "", fmt.Errorf("invalid string data")
	}
	length := new(big.Int).SetBytes(data[offset : offset+32]).Uint64()

	// Get string data
	start := offset + 32
	end := start + length
	if end > uint64(len(data)) {
		end = uint64(len(data))
	}

	return string(data[start:end]), nil
}

// extractBaseURI extracts base URI from a full token URI
// e.g., "https://example.com/tokens/123" -> "https://example.com/tokens/"
func extractBaseURI(uri string) string {
	// Find the last / and keep everything before it
	lastSlash := strings.LastIndex(uri, "/")
	if lastSlash == -1 {
		return uri
	}
	return uri[:lastSlash+1]
}
