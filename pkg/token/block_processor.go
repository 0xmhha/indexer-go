package token

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
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

// TokenMetadataStore is the storage the block processor reads and writes
// token metadata in.
type TokenMetadataStore interface {
	port.TokenMetadataReader
	port.TokenMetadataWriter
}

// ContractIndexer detects whether a new contract is a token and stores its
// metadata. It is used by the token.metadata feature.
type ContractIndexer struct {
	detector *Detector
	fetcher  *MetadataFetcher
	storage  TokenMetadataStore
	logger   *zap.Logger
}

// NewContractIndexer returns an indexer that reads contracts through client.
func NewContractIndexer(client EthClient, stor TokenMetadataStore, logger *zap.Logger) *ContractIndexer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ContractIndexer{
		detector: NewDetector(client, logger),
		fetcher:  NewMetadataFetcher(client, logger),
		storage:  stor,
		logger:   logger,
	}
}

// IndexContract stores the metadata of the contract at address, created in
// block blockNumber at blockTime, if it is a token not indexed yet. Node
// reads that fail leave the contract unindexed (they are logged); storage
// errors are returned.
func (p *ContractIndexer) IndexContract(ctx context.Context, address common.Address, blockNumber, blockTime uint64) error {
	existing, err := p.storage.GetTokenMetadata(ctx, address)
	if err == nil && existing != nil {
		return nil // already indexed
	}

	detection := p.detector.DetectStandard(ctx, address)
	if detection.Error != nil {
		p.logger.Debug("Failed to detect token standard",
			zap.String("address", address.Hex()),
			zap.Error(detection.Error))
		return nil
	}
	if detection.Standard == StandardUnknown {
		return nil
	}

	metadataResult := p.fetcher.FetchMetadata(ctx, address, detection.Standard)

	// Times are the block's, so that reprocessing a block stores the same
	// record.
	at := time.Unix(int64(blockTime), 0).UTC()
	metadata := &port.TokenMetadata{
		Address:            address,
		Standard:           convertStandard(detection.Standard),
		Name:               metadataResult.Name,
		Symbol:             metadataResult.Symbol,
		Decimals:           metadataResult.Decimals,
		TotalSupply:        metadataResult.TotalSupply,
		BaseURI:            metadataResult.BaseURI,
		DetectedAt:         blockNumber,
		CreatedAt:          at,
		UpdatedAt:          at,
		SupportsERC165:     detection.SupportsERC165,
		SupportsMetadata:   detection.SupportsMetadata,
		SupportsEnumerable: detection.SupportsEnumerable,
	}
	if err := p.storage.SaveTokenMetadata(ctx, metadata); err != nil {
		return fmt.Errorf("save token metadata of %s: %w", address.Hex(), err)
	}
	p.logger.Debug("Indexed token contract",
		zap.String("address", address.Hex()),
		zap.String("standard", string(metadata.Standard)),
		zap.String("name", metadata.Name),
		zap.String("symbol", metadata.Symbol),
		zap.Uint64("blockNumber", blockNumber))
	return nil
}

// convertStandard converts token.TokenStandard to port.TokenStandard
func convertStandard(standard TokenStandard) port.TokenStandard {
	switch standard {
	case StandardERC20:
		return port.TokenStandardERC20
	case StandardERC721:
		return port.TokenStandardERC721
	case StandardERC1155:
		return port.TokenStandardERC1155
	default:
		return port.TokenStandardUnknown
	}
}
