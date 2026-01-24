package factory

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/pkg/adapters/evm"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// EVMClient wraps an RPC client to implement evm.Client interface
type EVMClient struct {
	rpcClient *rpc.Client
	ethClient *ethclient.Client
}

// Ensure EVMClient implements evm.Client
var _ evm.Client = (*EVMClient)(nil)

// NewEVMClient creates a new EVM client from an RPC client
func NewEVMClient(rpcClient *rpc.Client) *EVMClient {
	return &EVMClient{
		rpcClient: rpcClient,
		ethClient: ethclient.NewClient(rpcClient),
	}
}

// NewEVMClientFromURL creates a new EVM client from an RPC URL
func NewEVMClientFromURL(ctx context.Context, rpcURL string) (*EVMClient, error) {
	rpcClient, err := rpc.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, err
	}
	return NewEVMClient(rpcClient), nil
}

// GetLatestBlockNumber returns the latest block number
func (c *EVMClient) GetLatestBlockNumber(ctx context.Context) (uint64, error) {
	return c.ethClient.BlockNumber(ctx)
}

// GetBlockByNumber retrieves a block by number
func (c *EVMClient) GetBlockByNumber(ctx context.Context, number uint64) (*types.Block, error) {
	// Try standard ethclient first
	block, err := c.ethClient.BlockByNumber(ctx, big.NewInt(int64(number)))
	if err == nil {
		return block, nil
	}

	// Fallback: use raw RPC call with custom parsing for EIP-4844 compatibility
	return c.getBlockByNumberRaw(ctx, number)
}

// GetBlockByHash retrieves a block by hash
func (c *EVMClient) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	// Try standard ethclient first
	block, err := c.ethClient.BlockByHash(ctx, hash)
	if err == nil {
		return block, nil
	}

	// Fallback: use raw RPC call with custom parsing for EIP-4844 compatibility
	return c.getBlockByHashRaw(ctx, hash)
}

// getBlockByNumberRaw fetches a block using raw RPC and custom parsing
func (c *EVMClient) getBlockByNumberRaw(ctx context.Context, number uint64) (*types.Block, error) {
	var raw json.RawMessage
	err := c.rpcClient.CallContext(ctx, &raw, "eth_getBlockByNumber", toBlockNumArg(big.NewInt(int64(number))), true)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ethereum.NotFound
	}

	return parseRawBlock(raw)
}

// getBlockByHashRaw fetches a block by hash using raw RPC and custom parsing
func (c *EVMClient) getBlockByHashRaw(ctx context.Context, hash common.Hash) (*types.Block, error) {
	var raw json.RawMessage
	err := c.rpcClient.CallContext(ctx, &raw, "eth_getBlockByHash", hash, true)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ethereum.NotFound
	}

	return parseRawBlock(raw)
}

// rpcBlock is a helper struct for parsing raw block JSON with EIP-4844 compatibility
type rpcBlock struct {
	Hash         common.Hash         `json:"hash"`
	Transactions []rpcTransaction    `json:"transactions"`
	UncleHashes  []common.Hash       `json:"uncles"`
	Withdrawals  []*types.Withdrawal `json:"withdrawals,omitempty"`
}

// rpcTransaction is a helper for parsing transactions
type rpcTransaction struct {
	tx *types.Transaction
	txExtraInfo
}

type txExtraInfo struct {
	BlockNumber *string         `json:"blockNumber,omitempty"`
	BlockHash   *common.Hash    `json:"blockHash,omitempty"`
	From        *common.Address `json:"from,omitempty"`
}

func (tx *rpcTransaction) UnmarshalJSON(msg []byte) error {
	if err := json.Unmarshal(msg, &tx.txExtraInfo); err != nil {
		return err
	}
	return json.Unmarshal(msg, &tx.tx)
}

// rpcHeader is a helper struct for parsing header with flexible EIP-4844 fields
type rpcHeader struct {
	ParentHash       common.Hash      `json:"parentHash"`
	UncleHash        common.Hash      `json:"sha3Uncles"`
	Coinbase         common.Address   `json:"miner"`
	Root             common.Hash      `json:"stateRoot"`
	TxHash           common.Hash      `json:"transactionsRoot"`
	ReceiptHash      common.Hash      `json:"receiptsRoot"`
	Bloom            types.Bloom      `json:"logsBloom"`
	Difficulty       *hexutil.Big     `json:"difficulty"`
	Number           *hexutil.Big     `json:"number"`
	GasLimit         hexutil.Uint64   `json:"gasLimit"`
	GasUsed          hexutil.Uint64   `json:"gasUsed"`
	Time             hexutil.Uint64   `json:"timestamp"`
	Extra            hexutil.Bytes    `json:"extraData"`
	MixDigest        common.Hash      `json:"mixHash"`
	Nonce            types.BlockNonce `json:"nonce"`
	BaseFee          *hexutil.Big     `json:"baseFeePerGas,omitempty"`
	WithdrawalsHash  *common.Hash     `json:"withdrawalsRoot,omitempty"`
	BlobGasUsed      *flexibleUint64  `json:"blobGasUsed,omitempty"`
	ExcessBlobGas    *flexibleUint64  `json:"excessBlobGas,omitempty"`
	ParentBeaconRoot *common.Hash     `json:"parentBeaconBlockRoot,omitempty"`
}

// flexibleUint64 can unmarshal from both hex string and number
type flexibleUint64 uint64

func (f *flexibleUint64) UnmarshalJSON(data []byte) error {
	// Try as hex string first (e.g., "0x0")
	var hexStr string
	if err := json.Unmarshal(data, &hexStr); err == nil {
		val, err := hexutil.DecodeUint64(hexStr)
		if err != nil {
			return err
		}
		*f = flexibleUint64(val)
		return nil
	}

	// Try as number
	var num uint64
	if err := json.Unmarshal(data, &num); err != nil {
		return err
	}
	*f = flexibleUint64(num)
	return nil
}

// parseRawBlock parses a raw JSON block into types.Block
func parseRawBlock(raw json.RawMessage) (*types.Block, error) {
	// Parse header
	var head rpcHeader
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("failed to parse header: %w", err)
	}

	// Parse block body
	var body rpcBlock
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("failed to parse block body: %w", err)
	}

	// Convert to types.Header
	header := &types.Header{
		ParentHash:  head.ParentHash,
		UncleHash:   head.UncleHash,
		Coinbase:    head.Coinbase,
		Root:        head.Root,
		TxHash:      head.TxHash,
		ReceiptHash: head.ReceiptHash,
		Bloom:       head.Bloom,
		Difficulty:  (*big.Int)(head.Difficulty),
		Number:      (*big.Int)(head.Number),
		GasLimit:    uint64(head.GasLimit),
		GasUsed:     uint64(head.GasUsed),
		Time:        uint64(head.Time),
		Extra:       head.Extra,
		MixDigest:   head.MixDigest,
		Nonce:       head.Nonce,
	}

	if head.BaseFee != nil {
		header.BaseFee = (*big.Int)(head.BaseFee)
	}
	if head.WithdrawalsHash != nil {
		header.WithdrawalsHash = head.WithdrawalsHash
	}
	if head.BlobGasUsed != nil {
		val := uint64(*head.BlobGasUsed)
		header.BlobGasUsed = &val
	}
	if head.ExcessBlobGas != nil {
		val := uint64(*head.ExcessBlobGas)
		header.ExcessBlobGas = &val
	}
	if head.ParentBeaconRoot != nil {
		header.ParentBeaconRoot = head.ParentBeaconRoot
	}

	// Extract transactions
	txs := make([]*types.Transaction, len(body.Transactions))
	for i, tx := range body.Transactions {
		txs[i] = tx.tx
	}

	// Create block with transactions and withdrawals
	block := types.NewBlockWithHeader(header).WithBody(types.Body{
		Transactions: txs,
		Withdrawals:  body.Withdrawals,
	})

	return block, nil
}

// GetBlockReceipts retrieves all receipts for a block
func (c *EVMClient) GetBlockReceipts(ctx context.Context, blockNumber uint64) (types.Receipts, error) {
	// Use eth_getBlockReceipts if available (EIP-1898)
	var receipts types.Receipts
	err := c.rpcClient.CallContext(ctx, &receipts, "eth_getBlockReceipts", toBlockNumArg(big.NewInt(int64(blockNumber))))
	if err == nil {
		return receipts, nil
	}

	// Fallback: fetch block and then each receipt individually
	block, err := c.GetBlockByNumber(ctx, blockNumber)
	if err != nil {
		return nil, err
	}

	receipts = make(types.Receipts, 0, len(block.Transactions()))
	for _, tx := range block.Transactions() {
		receipt, err := c.ethClient.TransactionReceipt(ctx, tx.Hash())
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, receipt)
	}

	return receipts, nil
}

// GetTransactionByHash retrieves a transaction by hash
func (c *EVMClient) GetTransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error) {
	return c.ethClient.TransactionByHash(ctx, hash)
}

// BalanceAt returns the balance of an account
func (c *EVMClient) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	return c.ethClient.BalanceAt(ctx, account, blockNumber)
}

// Close releases the client connection
func (c *EVMClient) Close() {
	if c.ethClient != nil {
		c.ethClient.Close()
	}
}

// RawRPCClient returns the underlying RPC client for advanced operations
func (c *EVMClient) RawRPCClient() *rpc.Client {
	return c.rpcClient
}

// EthClient returns the underlying ethclient for advanced operations
func (c *EVMClient) EthClient() *ethclient.Client {
	return c.ethClient
}

// SubscribePendingTransactions subscribes to pending transactions
func (c *EVMClient) SubscribePendingTransactions(ctx context.Context) (<-chan common.Hash, ethereum.Subscription, error) {
	txHashCh := make(chan common.Hash, 100)
	sub, err := c.ethClient.Client().EthSubscribe(ctx, txHashCh, "newPendingTransactions")
	if err != nil {
		close(txHashCh)
		return nil, nil, err
	}
	return txHashCh, sub, nil
}

// toBlockNumArg converts a big.Int block number to the RPC argument format
func toBlockNumArg(number *big.Int) string {
	if number == nil {
		return "latest"
	}
	pending := big.NewInt(-1)
	if number.Cmp(pending) == 0 {
		return "pending"
	}
	return "0x" + number.Text(16)
}
