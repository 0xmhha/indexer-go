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
	block, _, err := c.getBlockByNumberRawWithMetas(ctx, number)
	return block, err
}

// getBlockByNumberRawWithMetas fetches a block and extracts fee delegation metadata
func (c *EVMClient) getBlockByNumberRawWithMetas(ctx context.Context, number uint64) (*types.Block, []*FeeDelegationMeta, error) {
	var raw json.RawMessage
	err := c.rpcClient.CallContext(ctx, &raw, "eth_getBlockByNumber", toBlockNumArg(big.NewInt(int64(number))), true)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, ethereum.NotFound
	}

	return parseRawBlockWithMetas(raw)
}

// GetBlockWithFeeDelegationMeta retrieves a block by number along with fee delegation metadata
func (c *EVMClient) GetBlockWithFeeDelegationMeta(ctx context.Context, number uint64) (*types.Block, []*FeeDelegationMeta, error) {
	return c.getBlockByNumberRawWithMetas(ctx, number)
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

// FeeDelegateDynamicFeeTxType is the StableNet-specific fee delegation transaction type
const FeeDelegateDynamicFeeTxType = 0x16

// FeeDelegationMeta contains fee delegation metadata for a transaction
type FeeDelegationMeta struct {
	TxHash       common.Hash
	BlockNumber  uint64
	OriginalType uint8
	FeePayer     common.Address
	FeePayerV    *big.Int
	FeePayerR    *big.Int
	FeePayerS    *big.Int
}

// rpcTransaction is a helper for parsing transactions
type rpcTransaction struct {
	tx *types.Transaction
	txExtraInfo
	// originalType stores the original transaction type for custom types like 0x16
	// This is needed because go-ethereum doesn't support StableNet's Fee Delegation type
	originalType *uint8
	// feePayer stores the fee payer address for Fee Delegation transactions
	feePayer *common.Address
	// feePayerV, feePayerR, feePayerS store the fee payer signature
	feePayerV, feePayerR, feePayerS *big.Int
}

type txExtraInfo struct {
	BlockNumber *string         `json:"blockNumber,omitempty"`
	BlockHash   *common.Hash    `json:"blockHash,omitempty"`
	From        *common.Address `json:"from,omitempty"`
}

// IsFeeDelegation returns true if this transaction is a Fee Delegation transaction (type 0x16)
func (tx *rpcTransaction) IsFeeDelegation() bool {
	return tx.originalType != nil && *tx.originalType == FeeDelegateDynamicFeeTxType
}

// FeePayer returns the fee payer address for Fee Delegation transactions
func (tx *rpcTransaction) FeePayer() *common.Address {
	return tx.feePayer
}

// FeePayerSignature returns the fee payer signature (V, R, S) for Fee Delegation transactions
func (tx *rpcTransaction) FeePayerSignature() (*big.Int, *big.Int, *big.Int) {
	return tx.feePayerV, tx.feePayerR, tx.feePayerS
}

// OriginalType returns the original transaction type (useful for custom types like 0x16)
func (tx *rpcTransaction) OriginalType() uint8 {
	if tx.originalType != nil {
		return *tx.originalType
	}
	return tx.tx.Type()
}

// feeDelegationTxJSON is used to parse StableNet Fee Delegation transactions (type 0x16)
// Since go-ethereum doesn't support this type, we parse it manually and convert to DynamicFeeTx
type feeDelegationTxJSON struct {
	Type                 hexutil.Uint64  `json:"type"`
	ChainID              *hexutil.Big    `json:"chainId"`
	Nonce                *hexutil.Uint64 `json:"nonce"`
	GasTipCap            *hexutil.Big    `json:"maxPriorityFeePerGas"`
	GasFeeCap            *hexutil.Big    `json:"maxFeePerGas"`
	Gas                  *hexutil.Uint64 `json:"gas"`
	To                   *common.Address `json:"to"`
	Value                *hexutil.Big    `json:"value"`
	Data                 *hexutil.Bytes  `json:"input"`
	AccessList           types.AccessList `json:"accessList"`
	V                    *hexutil.Big    `json:"v"`
	R                    *hexutil.Big    `json:"r"`
	S                    *hexutil.Big    `json:"s"`
	// Fee Delegation specific fields
	FeePayer             *common.Address `json:"feePayer"`
	FeePayerSignatures   []feePayerSig   `json:"feePayerSignatures"`
}

type feePayerSig struct {
	V *hexutil.Big `json:"v"`
	R *hexutil.Big `json:"r"`
	S *hexutil.Big `json:"s"`
}

func (tx *rpcTransaction) UnmarshalJSON(msg []byte) error {
	if err := json.Unmarshal(msg, &tx.txExtraInfo); err != nil {
		return err
	}

	// First, try standard go-ethereum unmarshalling
	if err := json.Unmarshal(msg, &tx.tx); err == nil {
		return nil
	}

	// If standard unmarshalling fails, check if it's a Fee Delegation transaction (type 0x16)
	var txType struct {
		Type hexutil.Uint64 `json:"type"`
	}
	if err := json.Unmarshal(msg, &txType); err != nil {
		return fmt.Errorf("failed to parse transaction type: %w", err)
	}

	if uint64(txType.Type) == FeeDelegateDynamicFeeTxType {
		// Parse Fee Delegation transaction and convert to DynamicFeeTx
		return tx.unmarshalFeeDelegationTx(msg)
	}

	// Unknown transaction type
	return fmt.Errorf("unsupported transaction type: 0x%x", txType.Type)
}

// unmarshalFeeDelegationTx parses a StableNet Fee Delegation transaction (type 0x16)
// and converts it to a DynamicFeeTx (type 0x02) for compatibility with go-ethereum
// The original type (0x16) and fee payer information are preserved in the rpcTransaction struct
func (tx *rpcTransaction) unmarshalFeeDelegationTx(msg []byte) error {
	var fdTx feeDelegationTxJSON
	if err := json.Unmarshal(msg, &fdTx); err != nil {
		return fmt.Errorf("failed to parse fee delegation transaction: %w", err)
	}

	// Store the original transaction type
	originalType := uint8(FeeDelegateDynamicFeeTxType)
	tx.originalType = &originalType

	// Store fee payer information
	tx.feePayer = fdTx.FeePayer
	if len(fdTx.FeePayerSignatures) > 0 {
		sig := fdTx.FeePayerSignatures[0]
		if sig.V != nil {
			tx.feePayerV = (*big.Int)(sig.V)
		}
		if sig.R != nil {
			tx.feePayerR = (*big.Int)(sig.R)
		}
		if sig.S != nil {
			tx.feePayerS = (*big.Int)(sig.S)
		}
	}

	// Convert to DynamicFeeTx (type 0x02) - the closest standard type
	var to *common.Address
	if fdTx.To != nil {
		to = fdTx.To
	}

	var data []byte
	if fdTx.Data != nil {
		data = *fdTx.Data
	}

	var nonce uint64
	if fdTx.Nonce != nil {
		nonce = uint64(*fdTx.Nonce)
	}

	var gas uint64
	if fdTx.Gas != nil {
		gas = uint64(*fdTx.Gas)
	}

	var chainID *big.Int
	if fdTx.ChainID != nil {
		chainID = (*big.Int)(fdTx.ChainID)
	}

	var gasTipCap *big.Int
	if fdTx.GasTipCap != nil {
		gasTipCap = (*big.Int)(fdTx.GasTipCap)
	}

	var gasFeeCap *big.Int
	if fdTx.GasFeeCap != nil {
		gasFeeCap = (*big.Int)(fdTx.GasFeeCap)
	}

	var value *big.Int
	if fdTx.Value != nil {
		value = (*big.Int)(fdTx.Value)
	} else {
		value = big.NewInt(0)
	}

	var v, r, s *big.Int
	if fdTx.V != nil {
		v = (*big.Int)(fdTx.V)
	}
	if fdTx.R != nil {
		r = (*big.Int)(fdTx.R)
	}
	if fdTx.S != nil {
		s = (*big.Int)(fdTx.S)
	}

	// Create a DynamicFeeTx with the parsed data
	// We use types.NewTx to properly wrap it as a Transaction
	dynamicTx := &types.DynamicFeeTx{
		ChainID:    chainID,
		Nonce:      nonce,
		GasTipCap:  gasTipCap,
		GasFeeCap:  gasFeeCap,
		Gas:        gas,
		To:         to,
		Value:      value,
		Data:       data,
		AccessList: fdTx.AccessList,
		V:          v,
		R:          r,
		S:          s,
	}

	tx.tx = types.NewTx(dynamicTx)
	return nil
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
	block, _, err := parseRawBlockWithMetas(raw)
	return block, err
}

// parseRawBlockWithMetas parses a raw JSON block into types.Block and extracts fee delegation metadata
func parseRawBlockWithMetas(raw json.RawMessage) (*types.Block, []*FeeDelegationMeta, error) {
	// Parse header
	var head rpcHeader
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, nil, fmt.Errorf("failed to parse header: %w", err)
	}

	// Parse block body
	var body rpcBlock
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, nil, fmt.Errorf("failed to parse block body: %w", err)
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

	// Extract transactions and fee delegation metadata
	txs := make([]*types.Transaction, len(body.Transactions))
	var feeDelegationMetas []*FeeDelegationMeta
	blockNumber := header.Number.Uint64()

	for i, tx := range body.Transactions {
		txs[i] = tx.tx

		// Extract fee delegation metadata if this is a fee delegation transaction
		if tx.IsFeeDelegation() && tx.feePayer != nil {
			meta := &FeeDelegationMeta{
				TxHash:       tx.tx.Hash(),
				BlockNumber:  blockNumber,
				OriginalType: *tx.originalType,
				FeePayer:     *tx.feePayer,
				FeePayerV:    tx.feePayerV,
				FeePayerR:    tx.feePayerR,
				FeePayerS:    tx.feePayerS,
			}
			feeDelegationMetas = append(feeDelegationMetas, meta)
		}
	}

	// Create block with transactions and withdrawals
	block := types.NewBlockWithHeader(header).WithBody(types.Body{
		Transactions: txs,
		Withdrawals:  body.Withdrawals,
	})

	return block, feeDelegationMetas, nil
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
