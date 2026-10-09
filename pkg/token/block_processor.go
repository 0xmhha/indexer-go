package token

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
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
	client  EthClient
	storage TokenMetadataStore
	logger  *zap.Logger
}

// NewContractIndexer returns an indexer that reads contracts through client.
func NewContractIndexer(client EthClient, stor TokenMetadataStore, logger *zap.Logger) *ContractIndexer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ContractIndexer{client: client, storage: stor, logger: logger}
}

// IndexContract stores the metadata of the contract at address, created in
// block blockNumber at blockTime, if it is a token not indexed yet. The
// contract is read as of that block, so live indexing and a later backfill
// store the same metadata (docs/SDK.md, determinism rules); a node that no
// longer keeps that block's state is read at its latest state, with a
// warning. A read the node did not answer (connection, timeout) is returned
// as an error, so the block is retried instead of indexed without the
// token; storage errors are returned too.
func (p *ContractIndexer) IndexContract(ctx context.Context, address common.Address, blockNumber, blockTime uint64) error {
	existing, err := p.storage.GetTokenMetadata(ctx, address)
	if err == nil && existing != nil {
		return nil // already indexed
	}

	node := &blockReader{client: p.client, block: new(big.Int).SetUint64(blockNumber)}
	defer func() {
		if node.latest {
			p.logger.Warn("The node keeps no state of the token's creation block; read its latest state",
				zap.String("address", address.Hex()), zap.Uint64("block", blockNumber))
		}
	}()
	detection := NewDetector(node, p.logger).DetectStandard(ctx, address)
	if node.failed != nil {
		return fmt.Errorf("read contract %s at block %d: %w", address.Hex(), blockNumber, node.failed)
	}
	if detection.Error != nil {
		p.logger.Debug("Failed to detect token standard",
			zap.String("address", address.Hex()),
			zap.Error(detection.Error))
		return nil
	}
	if detection.Standard == StandardUnknown {
		return nil
	}

	metadataResult := NewMetadataFetcher(node, p.logger).FetchMetadata(ctx, address, detection.Standard)
	if node.failed != nil {
		return fmt.Errorf("read token %s at block %d: %w", address.Hex(), blockNumber, node.failed)
	}

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

// blockReader reads the node as of one block, whatever block the caller
// asks for. A node that answers it keeps no state of that block (not an
// archive node) is asked for its latest state instead, and latest is set.
// The first error the node did not answer itself (connection, timeout,
// cancellation) is kept in failed: unlike an answered error (a revert, a
// method the contract lacks) it says nothing about the contract.
type blockReader struct {
	client EthClient
	block  *big.Int
	latest bool
	failed error
}

func (r *blockReader) CallContract(ctx context.Context, call ethereum.CallMsg, _ interface{}) ([]byte, error) {
	out, err := r.client.CallContract(ctx, call, r.block)
	if missingState(err) {
		r.latest = true
		out, err = r.client.CallContract(ctx, call, nil)
	}
	return out, r.note(err)
}

func (r *blockReader) CodeAt(ctx context.Context, contract common.Address, _ interface{}) ([]byte, error) {
	out, err := r.client.CodeAt(ctx, contract, r.block)
	if missingState(err) {
		r.latest = true
		out, err = r.client.CodeAt(ctx, contract, nil)
	}
	return out, r.note(err)
}

func (r *blockReader) note(err error) error {
	var answered rpc.Error
	if err != nil && !errors.As(err, &answered) && r.failed == nil {
		r.failed = err
	}
	return err
}

// missingState reports whether the node answered that it keeps no state of
// the block asked for.
func missingState(err error) bool {
	var answered rpc.Error
	if err == nil || !errors.As(err, &answered) {
		return false
	}
	msg := strings.ToLower(answered.Error())
	for _, s := range []string{"missing trie node", "historical state", "state not available", "header not found", "state is not available"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
