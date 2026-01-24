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

// BatchReceiptError represents an error for a single receipt in a batch operation
type BatchReceiptError struct {
	TxHash common.Hash
	Error  error
}

// BatchReceiptResult contains the results of a batch receipt fetch operation
type BatchReceiptResult struct {
	Receipts      []*types.Receipt    // Successfully fetched receipts (nil for failed ones)
	Errors        []BatchReceiptError // List of errors encountered
	SuccessCount  int                 // Number of successfully fetched receipts
	FailureCount  int                 // Number of failed fetches
	TotalRequests int                 // Total number of requests made
}

// HasErrors returns true if any errors occurred during the batch operation
func (r *BatchReceiptResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// AllSucceeded returns true if all receipts were fetched successfully
func (r *BatchReceiptResult) AllSucceeded() bool {
	return r.FailureCount == 0
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

// EthClient returns the underlying ethclient.Client
func (c *Client) EthClient() *ethclient.Client {
	return c.ethClient
}

// RPCClient returns the underlying rpc.Client
func (c *Client) RPCClient() *rpc.Client {
	return c.rpcClient
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
func (c *Client) GetBlockReceipts(ctx context.Context, blockNumber uint64) (types.Receipts, error) {
	blockNum := new(big.Int).SetUint64(blockNumber)

	// Use BlockReceipts method from ethclient
	receipts, err := c.ethClient.BlockReceipts(ctx, rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(blockNum.Int64())))
	if err != nil {
		return nil, fmt.Errorf("failed to get receipts for block %d: %w", blockNumber, err)
	}

	return types.Receipts(receipts), nil
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

// BalanceAt returns the balance of an account at a specific block number
// If blockNumber is nil, returns the balance at the latest block
func (c *Client) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	balance, err := c.ethClient.BalanceAt(ctx, account, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance for %s at block %v: %w", account.Hex(), blockNumber, err)
	}
	return balance, nil
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

// BatchGetReceiptsWithDetails fetches multiple transaction receipts and returns detailed results
// including partial successes and individual error tracking
func (c *Client) BatchGetReceiptsWithDetails(ctx context.Context, hashes []common.Hash) (*BatchReceiptResult, error) {
	result := &BatchReceiptResult{
		Receipts:      make([]*types.Receipt, len(hashes)),
		Errors:        make([]BatchReceiptError, 0),
		TotalRequests: len(hashes),
	}

	if len(hashes) == 0 {
		return result, nil
	}

	batch := make([]rpc.BatchElem, len(hashes))
	for i, hash := range hashes {
		batch[i] = rpc.BatchElem{
			Method: "eth_getTransactionReceipt",
			Args:   []interface{}{hash},
			Result: &result.Receipts[i],
		}
	}

	if err := c.rpcClient.BatchCallContext(ctx, batch); err != nil {
		return nil, fmt.Errorf("batch call failed: %w", err)
	}

	// Check for individual errors and track them
	for i, elem := range batch {
		if elem.Error != nil {
			c.logger.Warn("failed to fetch receipt in batch",
				zap.String("tx_hash", hashes[i].Hex()),
				zap.Error(elem.Error))
			result.Errors = append(result.Errors, BatchReceiptError{
				TxHash: hashes[i],
				Error:  elem.Error,
			})
			result.Receipts[i] = nil
			result.FailureCount++
		} else if result.Receipts[i] == nil {
			// Receipt not found (transaction might be pending or non-existent)
			c.logger.Debug("receipt not found in batch",
				zap.String("tx_hash", hashes[i].Hex()))
			result.Errors = append(result.Errors, BatchReceiptError{
				TxHash: hashes[i],
				Error:  fmt.Errorf("receipt not found"),
			})
			result.FailureCount++
		} else {
			result.SuccessCount++
		}
	}

	c.logger.Debug("batch receipt fetch completed",
		zap.Int("total", result.TotalRequests),
		zap.Int("success", result.SuccessCount),
		zap.Int("failed", result.FailureCount),
	)

	return result, nil
}

// BatchGetReceipts fetches multiple transaction receipts in a single batch request
// Returns an error if any receipt fails to fetch (use BatchGetReceiptsWithDetails for partial results)
func (c *Client) BatchGetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	result, err := c.BatchGetReceiptsWithDetails(ctx, hashes)
	if err != nil {
		return nil, err
	}

	// For backward compatibility, return error if any receipt failed
	if result.HasErrors() {
		// Return the first error for backward compatibility
		firstErr := result.Errors[0]
		return nil, fmt.Errorf("failed to fetch receipt for %s: %w", firstErr.TxHash.Hex(), firstErr.Error)
	}

	return result.Receipts, nil
}

// SubscribePendingTransactions subscribes to pending transaction hashes
// Returns a channel that receives transaction hashes and a subscription object
func (c *Client) SubscribePendingTransactions(ctx context.Context) (<-chan common.Hash, ethereum.Subscription, error) {
	ch := make(chan common.Hash, 100) // Buffer to prevent blocking

	// Use RPC client's EthSubscribe for newPendingTransactions
	sub, err := c.rpcClient.EthSubscribe(ctx, ch, "newPendingTransactions")
	if err != nil {
		close(ch)
		return nil, nil, fmt.Errorf("failed to subscribe to pending transactions: %w", err)
	}

	c.logger.Info("subscribed to pending transactions")

	return ch, sub, nil
}
