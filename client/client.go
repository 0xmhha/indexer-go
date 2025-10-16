package client

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
)

// Client wraps Ethereum JSON-RPC client with additional functionality
type Client struct {
	ethClient *ethclient.Client
	rpcClient *rpc.Client
	endpoint  string
	logger    *zap.Logger
}

// Config holds client configuration
type Config struct {
	Endpoint string
	Timeout  time.Duration
	Logger   *zap.Logger
}

// NewClient creates a new Ethereum client
func NewClient(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("endpoint cannot be empty")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	// Create RPC client with timeout
	ctx := context.Background()
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	rpcClient, err := rpc.DialContext(ctx, cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC endpoint: %w", err)
	}

	ethClient := ethclient.NewClient(rpcClient)

	client := &Client{
		ethClient: ethClient,
		rpcClient: rpcClient,
		endpoint:  cfg.Endpoint,
		logger:    logger,
	}

	// Verify connection
	if err := client.Ping(ctx); err != nil {
		rpcClient.Close()
		return nil, fmt.Errorf("failed to ping RPC endpoint: %w", err)
	}

	logger.Info("connected to Ethereum RPC",
		zap.String("endpoint", cfg.Endpoint))

	return client, nil
}

// Ping verifies the connection to the RPC endpoint
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.ethClient.ChainID(ctx)
	return err
}

// Close closes the client connection
func (c *Client) Close() {
	if c.ethClient != nil {
		c.ethClient.Close()
	}
}

// GetLatestBlockNumber returns the latest block number
func (c *Client) GetLatestBlockNumber(ctx context.Context) (uint64, error) {
	blockNumber, err := c.ethClient.BlockNumber(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest block number: %w", err)
	}
	return blockNumber, nil
}

// GetBlockByNumber fetches a block by its number
func (c *Client) GetBlockByNumber(ctx context.Context, number uint64) (*types.Block, error) {
	blockNum := new(big.Int).SetUint64(number)
	block, err := c.ethClient.BlockByNumber(ctx, blockNum)
	if err != nil {
		return nil, fmt.Errorf("failed to get block %d: %w", number, err)
	}
	return block, nil
}

// GetBlockByHash fetches a block by its hash
func (c *Client) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	block, err := c.ethClient.BlockByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get block %s: %w", hash.Hex(), err)
	}
	return block, nil
}

// GetTransactionByHash fetches a transaction by its hash
func (c *Client) GetTransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error) {
	tx, isPending, err := c.ethClient.TransactionByHash(ctx, hash)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get transaction %s: %w", hash.Hex(), err)
	}
	return tx, isPending, nil
}

// GetTransactionReceipt fetches a transaction receipt
func (c *Client) GetTransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	receipt, err := c.ethClient.TransactionReceipt(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get receipt for %s: %w", hash.Hex(), err)
	}
	return receipt, nil
}

// GetBlockReceipts fetches all receipts for a block
func (c *Client) GetBlockReceipts(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	blockNum := new(big.Int).SetUint64(blockNumber)

	// Use BlockReceipts method from ethclient
	receipts, err := c.ethClient.BlockReceipts(ctx, rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(blockNum.Int64())))
	if err != nil {
		return nil, fmt.Errorf("failed to get receipts for block %d: %w", blockNumber, err)
	}

	return receipts, nil
}

// GetChainID returns the chain ID
func (c *Client) GetChainID(ctx context.Context) (*big.Int, error) {
	chainID, err := c.ethClient.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}
	return chainID, nil
}

// GetNetworkID returns the network ID
func (c *Client) GetNetworkID(ctx context.Context) (*big.Int, error) {
	networkID, err := c.ethClient.NetworkID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get network ID: %w", err)
	}
	return networkID, nil
}

// SubscribeNewHead subscribes to new block headers
func (c *Client) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
	sub, err := c.ethClient.SubscribeNewHead(ctx, ch)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to new heads: %w", err)
	}
	return sub, nil
}

// BatchGetBlocks fetches multiple blocks in a single batch request
func (c *Client) BatchGetBlocks(ctx context.Context, numbers []uint64) ([]*types.Block, error) {
	if len(numbers) == 0 {
		return nil, nil
	}

	blocks := make([]*types.Block, len(numbers))
	batch := make([]rpc.BatchElem, len(numbers))

	for i, num := range numbers {
		batch[i] = rpc.BatchElem{
			Method: "eth_getBlockByNumber",
			Args:   []interface{}{fmt.Sprintf("0x%x", num), true}, // true to include transactions
			Result: &blocks[i],
		}
	}

	if err := c.rpcClient.BatchCallContext(ctx, batch); err != nil {
		return nil, fmt.Errorf("batch call failed: %w", err)
	}

	// Check for individual errors
	for i, elem := range batch {
		if elem.Error != nil {
			c.logger.Error("failed to fetch block in batch",
				zap.Uint64("block_number", numbers[i]),
				zap.Error(elem.Error))
			return nil, fmt.Errorf("failed to fetch block %d: %w", numbers[i], elem.Error)
		}
	}

	return blocks, nil
}

// BatchGetReceipts fetches multiple transaction receipts in a single batch request
func (c *Client) BatchGetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	receipts := make([]*types.Receipt, len(hashes))
	batch := make([]rpc.BatchElem, len(hashes))

	for i, hash := range hashes {
		batch[i] = rpc.BatchElem{
			Method: "eth_getTransactionReceipt",
			Args:   []interface{}{hash},
			Result: &receipts[i],
		}
	}

	if err := c.rpcClient.BatchCallContext(ctx, batch); err != nil {
		return nil, fmt.Errorf("batch call failed: %w", err)
	}

	// Check for individual errors
	for i, elem := range batch {
		if elem.Error != nil {
			c.logger.Error("failed to fetch receipt in batch",
				zap.String("tx_hash", hashes[i].Hex()),
				zap.Error(elem.Error))
			return nil, fmt.Errorf("failed to fetch receipt for %s: %w", hashes[i].Hex(), elem.Error)
		}
	}

	return receipts, nil
}
