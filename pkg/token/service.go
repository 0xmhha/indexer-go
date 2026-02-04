package token

import (
	"context"
	"time"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// TokenMetadataStorage defines the storage interface for token metadata
type TokenMetadataStorage interface {
	// GetTokenMetadata retrieves token metadata by contract address
	GetTokenMetadata(ctx context.Context, address common.Address) (*TokenMetadata, error)

	// SaveTokenMetadata saves or updates token metadata
	SaveTokenMetadata(ctx context.Context, metadata *TokenMetadata) error

	// DeleteTokenMetadata removes token metadata by address
	DeleteTokenMetadata(ctx context.Context, address common.Address) error

	// ListTokensByStandard retrieves tokens filtered by standard with pagination
	ListTokensByStandard(ctx context.Context, standard TokenStandard, limit, offset int) ([]*TokenMetadata, error)

	// GetTokensCount returns the count of tokens, optionally filtered by standard
	GetTokensCount(ctx context.Context, standard TokenStandard) (int, error)

	// SearchTokens searches for tokens by name or symbol
	SearchTokens(ctx context.Context, query string, limit int) ([]*TokenMetadata, error)
}

// Service provides token detection and metadata fetching capabilities
type Service struct {
	detector *Detector
	fetcher  *MetadataFetcher
	storage  TokenMetadataStorage
	logger   *zap.Logger
}

// NewService creates a new token service
func NewService(client EthClient, storage TokenMetadataStorage, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		detector: NewDetector(client, logger),
		fetcher:  NewMetadataFetcher(client, logger),
		storage:  storage,
		logger:   logger,
	}
}

// DetectAndFetch detects the token standard and fetches metadata for a contract
func (s *Service) DetectAndFetch(ctx context.Context, address common.Address, blockHeight uint64) (*TokenMetadata, error) {
	// Detect token standard
	detection := s.detector.DetectStandard(ctx, address)
	if detection.Error != nil {
		return nil, detection.Error
	}

	// If standard could not be detected, return minimal metadata
	if detection.Standard == StandardUnknown {
		s.logger.Debug("Could not detect token standard",
			zap.String("address", address.Hex()))
		return nil, nil
	}

	// Fetch metadata based on detected standard
	metadataResult := s.fetcher.FetchMetadata(ctx, address, detection.Standard)

	// Create token metadata
	now := time.Now()
	metadata := &TokenMetadata{
		Address:            address,
		Standard:           detection.Standard,
		Name:               metadataResult.Name,
		Symbol:             metadataResult.Symbol,
		Decimals:           metadataResult.Decimals,
		TotalSupply:        metadataResult.TotalSupply,
		BaseURI:            metadataResult.BaseURI,
		DetectedAt:         blockHeight,
		CreatedAt:          now,
		UpdatedAt:          now,
		SupportsERC165:     detection.SupportsERC165,
		SupportsMetadata:   detection.SupportsMetadata,
		SupportsEnumerable: detection.SupportsEnumerable,
	}

	// Log detection result
	s.logger.Info("Detected token contract",
		zap.String("address", address.Hex()),
		zap.String("standard", string(detection.Standard)),
		zap.String("name", metadata.Name),
		zap.String("symbol", metadata.Symbol),
		zap.Uint8("decimals", metadata.Decimals),
		zap.Float64("confidence", detection.Confidence),
		zap.Bool("supportsERC165", detection.SupportsERC165))

	return metadata, nil
}

// IndexToken detects, fetches metadata, and stores it
func (s *Service) IndexToken(ctx context.Context, address common.Address, blockHeight uint64) (*TokenMetadata, error) {
	// Check if we already have metadata for this token
	existing, err := s.storage.GetTokenMetadata(ctx, address)
	if err == nil && existing != nil {
		// Token already indexed
		s.logger.Debug("Token already indexed",
			zap.String("address", address.Hex()),
			zap.String("standard", string(existing.Standard)))
		return existing, nil
	}

	// Detect and fetch metadata
	metadata, err := s.DetectAndFetch(ctx, address, blockHeight)
	if err != nil {
		return nil, err
	}

	// If no token detected, skip storage
	if metadata == nil {
		return nil, nil
	}

	// Save to storage
	if err := s.storage.SaveTokenMetadata(ctx, metadata); err != nil {
		s.logger.Error("Failed to save token metadata",
			zap.String("address", address.Hex()),
			zap.Error(err))
		return nil, err
	}

	return metadata, nil
}

// TokenIndexerAdapter wraps the Service to implement the fetch.TokenIndexer interface.
// This adapter allows the token service to be used with the fetcher's contract creation hook.
type TokenIndexerAdapter struct {
	service *Service
}

// NewTokenIndexerAdapter creates a new adapter for the token service
func NewTokenIndexerAdapter(service *Service) *TokenIndexerAdapter {
	return &TokenIndexerAdapter{service: service}
}

// IndexToken implements the fetch.TokenIndexer interface
// It delegates to the service's IndexToken method, discarding the metadata return value
func (a *TokenIndexerAdapter) IndexToken(ctx context.Context, address common.Address, blockHeight uint64) error {
	_, err := a.service.IndexToken(ctx, address, blockHeight)
	return err
}

// StorageTokenMetadataFetcher implements storage.TokenMetadataFetcher interface
// This adapter allows on-demand token metadata fetching from chain for GetTokenBalances
type StorageTokenMetadataFetcher struct {
	detector *Detector
	fetcher  *MetadataFetcher
	logger   *zap.Logger
}

// NewStorageTokenMetadataFetcher creates a fetcher that implements storage.TokenMetadataFetcher
func NewStorageTokenMetadataFetcher(client EthClient, logger *zap.Logger) *StorageTokenMetadataFetcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &StorageTokenMetadataFetcher{
		detector: NewDetector(client, logger),
		fetcher:  NewMetadataFetcher(client, logger),
		logger:   logger,
	}
}

// NewStorageTokenMetadataFetcherFromEthClient creates a fetcher from ethclient.Client
func NewStorageTokenMetadataFetcherFromEthClient(ethClient interface{}, logger *zap.Logger) *StorageTokenMetadataFetcher {
	// Type assert to get the underlying ethclient
	if ec, ok := ethClient.(interface {
		CallContract(ctx context.Context, call interface{}, blockNumber interface{}) ([]byte, error)
		CodeAt(ctx context.Context, contract common.Address, blockNumber interface{}) ([]byte, error)
	}); ok {
		// Create adapter
		adapter := &ethClientWrapper{client: ec}
		return NewStorageTokenMetadataFetcher(adapter, logger)
	}
	return nil
}

// ethClientWrapper wraps an interface to implement EthClient
type ethClientWrapper struct {
	client interface {
		CallContract(ctx context.Context, call interface{}, blockNumber interface{}) ([]byte, error)
		CodeAt(ctx context.Context, contract common.Address, blockNumber interface{}) ([]byte, error)
	}
}

func (w *ethClientWrapper) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber interface{}) ([]byte, error) {
	return w.client.CallContract(ctx, call, blockNumber)
}

func (w *ethClientWrapper) CodeAt(ctx context.Context, contract common.Address, blockNumber interface{}) ([]byte, error) {
	return w.client.CodeAt(ctx, contract, blockNumber)
}

// FetchTokenMetadata implements storage.TokenMetadataFetcher interface
// It detects if the contract is a token and fetches its metadata from chain
func (f *StorageTokenMetadataFetcher) FetchTokenMetadata(ctx context.Context, address common.Address) (*storage.TokenMetadata, error) {
	// Detect token standard
	detection := f.detector.DetectStandard(ctx, address)
	if detection.Error != nil {
		f.logger.Debug("Failed to detect token standard",
			zap.String("address", address.Hex()),
			zap.Error(detection.Error))
		return nil, detection.Error
	}

	// If standard could not be detected, return nil (not a token)
	if detection.Standard == StandardUnknown {
		f.logger.Debug("Contract is not a recognized token",
			zap.String("address", address.Hex()))
		return nil, nil
	}

	// Fetch metadata based on detected standard
	metadataResult := f.fetcher.FetchMetadata(ctx, address, detection.Standard)

	// Create storage token metadata
	now := time.Now()
	metadata := &storage.TokenMetadata{
		Address:            address,
		Standard:           convertStandardToStorage(detection.Standard),
		Name:               metadataResult.Name,
		Symbol:             metadataResult.Symbol,
		Decimals:           metadataResult.Decimals,
		TotalSupply:        metadataResult.TotalSupply,
		BaseURI:            metadataResult.BaseURI,
		DetectedAt:         0, // Unknown block height for on-demand fetch
		CreatedAt:          now,
		UpdatedAt:          now,
		SupportsERC165:     detection.SupportsERC165,
		SupportsMetadata:   detection.SupportsMetadata,
		SupportsEnumerable: detection.SupportsEnumerable,
	}

	f.logger.Info("Fetched token metadata on-demand",
		zap.String("address", address.Hex()),
		zap.String("standard", string(metadata.Standard)),
		zap.String("name", metadata.Name),
		zap.String("symbol", metadata.Symbol),
		zap.Uint8("decimals", metadata.Decimals))

	return metadata, nil
}

// convertStandardToStorage converts token.TokenStandard to storage.TokenStandard
func convertStandardToStorage(standard TokenStandard) storage.TokenStandard {
	switch standard {
	case StandardERC20:
		return storage.TokenStandardERC20
	case StandardERC721:
		return storage.TokenStandardERC721
	case StandardERC1155:
		return storage.TokenStandardERC1155
	default:
		return storage.TokenStandardUnknown
	}
}

// RefreshMetadata re-fetches and updates metadata for an existing token
func (s *Service) RefreshMetadata(ctx context.Context, address common.Address) (*TokenMetadata, error) {
	// Get existing metadata
	existing, err := s.storage.GetTokenMetadata(ctx, address)
	if err != nil {
		return nil, err
	}

	// Fetch fresh metadata
	metadataResult := s.fetcher.FetchMetadata(ctx, address, existing.Standard)

	// Update metadata
	existing.Name = metadataResult.Name
	existing.Symbol = metadataResult.Symbol
	existing.Decimals = metadataResult.Decimals
	existing.TotalSupply = metadataResult.TotalSupply
	existing.BaseURI = metadataResult.BaseURI
	existing.UpdatedAt = time.Now()

	// Save updated metadata
	if err := s.storage.SaveTokenMetadata(ctx, existing); err != nil {
		return nil, err
	}

	return existing, nil
}

// IsTokenContract performs a quick check to see if an address might be a token contract
func (s *Service) IsTokenContract(ctx context.Context, address common.Address) bool {
	return s.detector.IsTokenContract(ctx, address)
}

// GetTokenMetadata retrieves cached token metadata
func (s *Service) GetTokenMetadata(ctx context.Context, address common.Address) (*TokenMetadata, error) {
	return s.storage.GetTokenMetadata(ctx, address)
}

// ListTokens lists tokens with optional standard filter and pagination
func (s *Service) ListTokens(ctx context.Context, standard TokenStandard, limit, offset int) ([]*TokenMetadata, error) {
	return s.storage.ListTokensByStandard(ctx, standard, limit, offset)
}

// SearchTokens searches tokens by name or symbol
func (s *Service) SearchTokens(ctx context.Context, query string, limit int) ([]*TokenMetadata, error) {
	return s.storage.SearchTokens(ctx, query, limit)
}

// GetTokensCount returns the count of indexed tokens
func (s *Service) GetTokensCount(ctx context.Context, standard TokenStandard) (int, error) {
	return s.storage.GetTokensCount(ctx, standard)
}
