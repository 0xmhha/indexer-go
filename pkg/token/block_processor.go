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

// HashEthClient reads a specific fork's state, without selecting by height.
type HashEthClient interface {
	CallContractAtHash(context.Context, ethereum.CallMsg, common.Hash) ([]byte, error)
	CodeAtHash(context.Context, common.Address, common.Hash) ([]byte, error)
}

func (a *EthClientAdapter) CallContractAtHash(ctx context.Context, call ethereum.CallMsg, hash common.Hash) ([]byte, error) {
	return a.client.CallContractAtHash(ctx, call, hash)
}
func (a *EthClientAdapter) CodeAtHash(ctx context.Context, addr common.Address, hash common.Hash) ([]byte, error) {
	return a.client.CodeAtHash(ctx, addr, hash)
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
	client           EthClient
	storage          TokenMetadataStore
	logger           *zap.Logger
	allowPrunedState bool
}

// NewContractIndexer returns an indexer that requires historical state from client.
func NewContractIndexer(client EthClient, stor TokenMetadataStore, logger *zap.Logger) *ContractIndexer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ContractIndexer{client: client, storage: stor, logger: logger}
}

// SetAllowPrunedState permits latest-state fallback when historical state is
// unavailable. Configure this before indexing; the default is strict.
func (p *ContractIndexer) SetAllowPrunedState(allow bool) {
	p.allowPrunedState = allow
}

// IndexContract stores the metadata of the contract at address, created in
// block blockNumber at blockTime, if it is a token not indexed yet. The
// contract is read as of that block, so live indexing and a later backfill
// store the same metadata (docs/SDK.md, determinism rules). Missing state
// fails the block unless SetAllowPrunedState explicitly permits a warned
// latest-state fallback. Unanswered reads and storage errors fail the block.
func (p *ContractIndexer) IndexContract(ctx context.Context, address common.Address, blockNumber, blockTime uint64) error {
	existing, err := p.storage.GetTokenMetadata(ctx, address)
	if err == nil && existing != nil {
		return nil // already indexed
	}

	metadata, err := p.ReadContract(ctx, address, blockNumber, blockTime)
	if err != nil {
		return err
	}
	if metadata == nil {
		return nil
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

// ReadContract acquires creation-block metadata without reading or writing
// storage. Its result may be passed to the ordered writer for an atomic save.
func (p *ContractIndexer) ReadContract(ctx context.Context, address common.Address, blockNumber, blockTime uint64) (*port.TokenMetadata, error) {
	return p.readContract(ctx, address, blockNumber, blockTime, nil)
}

// ReadContractAtHash acquires metadata from the exact creation block's state.
func (p *ContractIndexer) ReadContractAtHash(ctx context.Context, address common.Address, blockNumber, blockTime uint64, hash common.Hash) (*port.TokenMetadata, error) {
	if _, ok := p.client.(HashEthClient); !ok {
		return nil, fmt.Errorf("client does not support contract reads by block hash")
	}
	return p.readContract(ctx, address, blockNumber, blockTime, &hash)
}

func (p *ContractIndexer) readContract(ctx context.Context, address common.Address, blockNumber, blockTime uint64, hash *common.Hash) (*port.TokenMetadata, error) {
	node := &blockReader{client: p.client, block: new(big.Int).SetUint64(blockNumber), allowPrunedState: p.allowPrunedState}
	node.hash = hash
	defer func() {
		if node.latest {
			p.logger.Warn("The node keeps no state of the token's creation block; read its latest state",
				zap.String("address", address.Hex()), zap.Uint64("block", blockNumber))
		}
	}()
	detection := NewDetector(node, p.logger).DetectStandard(ctx, address)
	if node.failed != nil {
		return nil, fmt.Errorf("read contract %s at block %d: %w", address.Hex(), blockNumber, node.failed)
	}
	if detection.Error != nil {
		p.logger.Debug("Failed to detect token standard",
			zap.String("address", address.Hex()),
			zap.Error(detection.Error))
		return nil, nil
	}
	if detection.Standard == StandardUnknown {
		return nil, nil
	}

	metadataResult := NewMetadataFetcher(node, p.logger).FetchMetadata(ctx, address, detection.Standard)
	if node.failed != nil {
		return nil, fmt.Errorf("read token %s at block %d: %w", address.Hex(), blockNumber, node.failed)
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
	return metadata, nil
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
// archive node) is asked for latest state only when allowPrunedState is set.
// Unanswered errors (connection, timeout, cancellation), and in strict mode
// missing-state or code-read errors, are kept in failed. Contract-call reverts
// remain detector answers: they can indicate a method the contract lacks.
type blockReader struct {
	hash             *common.Hash
	client           EthClient
	block            *big.Int
	latest           bool
	failed           error
	allowPrunedState bool
}

func (r *blockReader) CallContract(ctx context.Context, call ethereum.CallMsg, _ interface{}) ([]byte, error) {
	var out []byte
	var err error
	if r.hash != nil {
		out, err = r.client.(HashEthClient).CallContractAtHash(ctx, call, *r.hash)
	} else {
		out, err = r.client.CallContract(ctx, call, r.block)
	}
	if r.allowPrunedState && missingState(err) {
		r.latest = true
		out, err = r.client.CallContract(ctx, call, nil)
	}
	return out, r.note(err)
}

func (r *blockReader) CodeAt(ctx context.Context, contract common.Address, _ interface{}) ([]byte, error) {
	var out []byte
	var err error
	if r.hash != nil {
		out, err = r.client.(HashEthClient).CodeAtHash(ctx, contract, *r.hash)
	} else {
		out, err = r.client.CodeAt(ctx, contract, r.block)
	}
	if r.allowPrunedState && missingState(err) {
		r.latest = true
		out, err = r.client.CodeAt(ctx, contract, nil)
	}
	// Unlike a contract call, a code read cannot be a contract-level revert.
	// A failed strict code read must not silently classify a token as absent.
	if err != nil && !r.allowPrunedState && r.failed == nil {
		r.failed = err
	}
	return out, r.note(err)
}

func (r *blockReader) note(err error) error {
	var answered rpc.Error
	if err != nil && (!errors.As(err, &answered) || !r.allowPrunedState && missingState(err) || r.hash != nil && !contractRevert(answered)) && r.failed == nil {
		r.failed = err
	}
	return err
}

func contractRevert(err rpc.Error) bool {
	if err == nil {
		return false
	}
	if err.ErrorCode() == 3 {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, vmError := range []string{"execution reverted", "out of gas", "invalid opcode", "invalid jump destination", "stack underflow", "stack limit reached", "write protection", "return data out of bounds", "gas uint64 overflow", "vm execution error"} {
		if strings.Contains(message, vmError) {
			return true
		}
	}
	return false
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
