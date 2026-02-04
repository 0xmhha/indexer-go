package token

import (
	"context"
	"math/big"
	"time"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// EthClientAdapter wraps ethclient.Client to implement the EthClient interface
type EthClientAdapter struct {
	client *ethclient.Client
}

// NewEthClientAdapter creates a new adapter from an ethclient.Client
func NewEthClientAdapter(client *ethclient.Client) *EthClientAdapter {
	return &EthClientAdapter{client: client}
}

// CallContract implements EthClient.CallContract
func (a *EthClientAdapter) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber interface{}) ([]byte, error) {
	var blockNum *big.Int
	if blockNumber != nil {
		switch v := blockNumber.(type) {
		case *big.Int:
			blockNum = v
		case int64:
			blockNum = big.NewInt(v)
		case uint64:
			blockNum = new(big.Int).SetUint64(v)
		}
	}
	return a.client.CallContract(ctx, call, blockNum)
}

// CodeAt implements EthClient.CodeAt
func (a *EthClientAdapter) CodeAt(ctx context.Context, contract common.Address, blockNumber interface{}) ([]byte, error) {
	var blockNum *big.Int
	if blockNumber != nil {
		switch v := blockNumber.(type) {
		case *big.Int:
			blockNum = v
		case int64:
			blockNum = big.NewInt(v)
		case uint64:
			blockNum = new(big.Int).SetUint64(v)
		}
	}
	return a.client.CodeAt(ctx, contract, blockNum)
}

// BlockProcessor implements the fetch.BlockProcessor interface
// to detect and index token metadata when new contracts are deployed
type BlockProcessor struct {
	detector *Detector
	fetcher  *MetadataFetcher
	storage  storage.TokenMetadataWriter
	reader   storage.TokenMetadataReader
	logger   *zap.Logger
}

// NewBlockProcessor creates a new token block processor
func NewBlockProcessor(client EthClient, stor storage.Storage, logger *zap.Logger) *BlockProcessor {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &BlockProcessor{
		detector: NewDetector(client, logger),
		fetcher:  NewMetadataFetcher(client, logger),
		storage:  stor,
		reader:   stor,
		logger:   logger,
	}
}

// NewBlockProcessorFromEthClient creates a new token block processor from an ethclient.Client
// This is a convenience function for integrating with the standard go-ethereum client
func NewBlockProcessorFromEthClient(ethClient *ethclient.Client, stor storage.Storage, logger *zap.Logger) *BlockProcessor {
	adapter := NewEthClientAdapter(ethClient)
	return NewBlockProcessor(adapter, stor, logger)
}

// ProcessBlock implements fetch.BlockProcessor interface
// It scans the block's receipts for contract creations and indexes token metadata
func (p *BlockProcessor) ProcessBlock(ctx context.Context, chainID string, block *types.Block, receipts []*types.Receipt) error {
	if block == nil {
		return nil
	}

	blockNumber := block.NumberU64()

	// Scan receipts for contract creations
	for _, receipt := range receipts {
		if receipt == nil {
			continue
		}

		// Contract creation is indicated by ContractAddress being non-zero
		if receipt.ContractAddress == (common.Address{}) {
			continue
		}

		// Contract was created - attempt to detect and index token metadata
		p.indexContractIfToken(ctx, receipt.ContractAddress, blockNumber)
	}

	return nil
}

// indexContractIfToken checks if a contract is a token and indexes its metadata
func (p *BlockProcessor) indexContractIfToken(ctx context.Context, address common.Address, blockNumber uint64) {
	// Check if we already have metadata for this token
	existing, err := p.reader.GetTokenMetadata(ctx, address)
	if err == nil && existing != nil {
		// Token already indexed
		p.logger.Debug("Token already indexed",
			zap.String("address", address.Hex()),
			zap.String("standard", string(existing.Standard)))
		return
	}

	// Detect token standard
	detection := p.detector.DetectStandard(ctx, address)
	if detection.Error != nil {
		p.logger.Debug("Failed to detect token standard",
			zap.String("address", address.Hex()),
			zap.Error(detection.Error))
		return
	}

	// If standard could not be detected, skip
	if detection.Standard == StandardUnknown {
		p.logger.Debug("Contract is not a recognized token",
			zap.String("address", address.Hex()))
		return
	}

	// Fetch metadata based on detected standard
	metadataResult := p.fetcher.FetchMetadata(ctx, address, detection.Standard)

	// Create storage token metadata
	now := time.Now()
	metadata := &storage.TokenMetadata{
		Address:            address,
		Standard:           convertStandard(detection.Standard),
		Name:               metadataResult.Name,
		Symbol:             metadataResult.Symbol,
		Decimals:           metadataResult.Decimals,
		TotalSupply:        metadataResult.TotalSupply,
		BaseURI:            metadataResult.BaseURI,
		DetectedAt:         blockNumber,
		CreatedAt:          now,
		UpdatedAt:          now,
		SupportsERC165:     detection.SupportsERC165,
		SupportsMetadata:   detection.SupportsMetadata,
		SupportsEnumerable: detection.SupportsEnumerable,
	}

	// Save to storage
	if err := p.storage.SaveTokenMetadata(ctx, metadata); err != nil {
		p.logger.Error("Failed to save token metadata",
			zap.String("address", address.Hex()),
			zap.Error(err))
		return
	}

	// Log successful indexing
	p.logger.Info("Indexed token contract",
		zap.String("address", address.Hex()),
		zap.String("standard", string(metadata.Standard)),
		zap.String("name", metadata.Name),
		zap.String("symbol", metadata.Symbol),
		zap.Uint8("decimals", metadata.Decimals),
		zap.Float64("confidence", detection.Confidence),
		zap.Uint64("blockNumber", blockNumber))
}

// convertStandard converts token.TokenStandard to storage.TokenStandard
func convertStandard(standard TokenStandard) storage.TokenStandard {
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
