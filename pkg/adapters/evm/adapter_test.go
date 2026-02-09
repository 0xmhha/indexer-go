package evm

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// signTx signs a transaction with a test key
func signTx(t *testing.T, tx *types.Transaction) (*types.Transaction, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	signer := types.LatestSignerForChainID(big.NewInt(1))
	signed, err := types.SignTx(tx, signer, key)
	require.NoError(t, err)
	return signed, key
}

// --- Mock Client ---

type mockClient struct {
	latestBlock  uint64
	latestErr    error
	block        *types.Block
	blockErr     error
	receipts     types.Receipts
	receiptsErr  error
	closeCalled  bool
}

func (m *mockClient) GetLatestBlockNumber(_ context.Context) (uint64, error) {
	return m.latestBlock, m.latestErr
}

func (m *mockClient) GetBlockByNumber(_ context.Context, _ uint64) (*types.Block, error) {
	return m.block, m.blockErr
}

func (m *mockClient) GetBlockByHash(_ context.Context, _ common.Hash) (*types.Block, error) {
	return m.block, m.blockErr
}

func (m *mockClient) GetBlockReceipts(_ context.Context, _ uint64) (types.Receipts, error) {
	return m.receipts, m.receiptsErr
}

func (m *mockClient) GetTransactionByHash(_ context.Context, _ common.Hash) (*types.Transaction, bool, error) {
	return nil, false, nil
}

func (m *mockClient) BalanceAt(_ context.Context, _ common.Address, _ *big.Int) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (m *mockClient) Close() {
	m.closeCalled = true
}

// --- DefaultConfig tests ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, big.NewInt(1), cfg.ChainID)
	assert.Equal(t, "Ethereum", cfg.ChainName)
	assert.Equal(t, "ETH", cfg.NativeCurrency)
	assert.Equal(t, 18, cfg.Decimals)
	assert.Equal(t, chain.ConsensusTypePoS, cfg.ConsensusType)
}

// --- NewAdapter tests ---

func TestNewAdapter(t *testing.T) {
	mc := &mockClient{}
	adapter := NewAdapter(mc, nil, zap.NewNop())

	assert.NotNil(t, adapter)
	assert.NotNil(t, adapter.blockFetcher)
	assert.NotNil(t, adapter.transactionParser)
	assert.Equal(t, mc, adapter.client)
}

func TestNewAdapter_NilConfig(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	info := adapter.Info()
	assert.Equal(t, "Ethereum", info.Name) // uses default
}

func TestNewAdapter_ZeroDecimals(t *testing.T) {
	cfg := &Config{ChainID: big.NewInt(5), Decimals: 0}
	adapter := NewAdapter(&mockClient{}, cfg, zap.NewNop())
	info := adapter.Info()
	assert.Equal(t, 18, info.Decimals) // forced to 18
}

// --- Info tests ---

func TestAdapter_Info(t *testing.T) {
	cfg := &Config{
		ChainID:        big.NewInt(137),
		ChainName:      "Polygon",
		NativeCurrency: "MATIC",
		Decimals:       18,
		ConsensusType:  chain.ConsensusTypePoS,
	}
	adapter := NewAdapter(&mockClient{}, cfg, zap.NewNop())
	info := adapter.Info()

	assert.Equal(t, big.NewInt(137), info.ChainID)
	assert.Equal(t, chain.ChainTypeEVM, info.ChainType)
	assert.Equal(t, "Polygon", info.Name)
	assert.Equal(t, "MATIC", info.NativeCurrency)
	assert.Equal(t, 18, info.Decimals)
	assert.Equal(t, chain.ConsensusTypePoS, info.ConsensusType)
}

// --- Interface accessors ---

func TestAdapter_BlockFetcher(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	bf := adapter.BlockFetcher()
	assert.NotNil(t, bf)
}

func TestAdapter_TransactionParser(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	tp := adapter.TransactionParser()
	assert.NotNil(t, tp)
}

func TestAdapter_ConsensusParser_Default(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	assert.Nil(t, adapter.ConsensusParser())
}

func TestAdapter_SystemContracts_Default(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	assert.Nil(t, adapter.SystemContracts())
}

func TestAdapter_SetConsensusParser(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	// Just test that it doesn't panic and can be retrieved
	adapter.SetConsensusParser(nil)
	assert.Nil(t, adapter.ConsensusParser())
}

func TestAdapter_GetClient(t *testing.T) {
	mc := &mockClient{}
	adapter := NewAdapter(mc, nil, zap.NewNop())
	assert.Equal(t, mc, adapter.GetClient())
}

// --- Close tests ---

func TestAdapter_Close(t *testing.T) {
	mc := &mockClient{}
	adapter := NewAdapter(mc, nil, zap.NewNop())

	err := adapter.Close()
	require.NoError(t, err)
	assert.True(t, mc.closeCalled)
}

func TestAdapter_Close_NilClient(t *testing.T) {
	adapter := &Adapter{}
	err := adapter.Close()
	require.NoError(t, err)
}

// --- BlockFetcher delegation tests ---

func TestBlockFetcher_GetLatestBlockNumber(t *testing.T) {
	mc := &mockClient{latestBlock: 12345}
	adapter := NewAdapter(mc, nil, zap.NewNop())
	bf := adapter.BlockFetcher()

	num, err := bf.GetLatestBlockNumber(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(12345), num)
}

func TestBlockFetcher_GetBlockByNumber(t *testing.T) {
	header := &types.Header{Number: big.NewInt(100)}
	block := types.NewBlockWithHeader(header)
	mc := &mockClient{block: block}
	adapter := NewAdapter(mc, nil, zap.NewNop())
	bf := adapter.BlockFetcher()

	result, err := bf.GetBlockByNumber(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, block, result)
}

// --- TransactionParser tests ---

func TestTransactionParser_ParseTransaction_NilTx(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	tp := adapter.TransactionParser()

	result, err := tp.ParseTransaction(nil, nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestTransactionParser_ParseTransaction(t *testing.T) {
	cfg := &Config{ChainID: big.NewInt(1), Decimals: 18}
	adapter := NewAdapter(&mockClient{}, cfg, zap.NewNop())
	tp := adapter.TransactionParser()

	to := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     10,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(20_000_000_000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(1_000_000_000_000_000_000),
	})

	signed, _ := signTx(t, tx)

	result, err := tp.ParseTransaction(signed, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, signed.Hash(), result.Hash)
	assert.Equal(t, &to, result.To)
	assert.Equal(t, big.NewInt(1_000_000_000_000_000_000), result.Value)
	assert.Equal(t, uint64(21000), result.GasLimit)
	assert.Equal(t, uint64(10), result.Nonce)
	assert.NotEqual(t, common.Address{}, result.From) // sender recovered
}

func TestTransactionParser_ParseTransaction_WithReceipt(t *testing.T) {
	cfg := &Config{ChainID: big.NewInt(1), Decimals: 18}
	adapter := NewAdapter(&mockClient{}, cfg, zap.NewNop())
	tp := adapter.TransactionParser()

	to := common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12")
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		To:        &to,
		Gas:       21000,
		GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(0),
	})

	signed, _ := signTx(t, tx)

	receipt := &types.Receipt{
		Status:           1,
		GasUsed:          21000,
		BlockNumber:      big.NewInt(100),
		BlockHash:        common.HexToHash("0xblockhash"),
		TransactionIndex: 3,
	}

	result, err := tp.ParseTransaction(signed, receipt)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, uint64(1), result.Status)
	assert.Equal(t, uint64(21000), result.GasUsed)
	assert.Equal(t, uint64(100), result.BlockNumber)
	assert.Equal(t, uint(3), result.TxIndex)
}

// --- ParseLogs tests ---

func TestTransactionParser_ParseLogs(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	tp := adapter.TransactionParser()

	logs := []*types.Log{
		{
			Address:     common.HexToAddress("0xtoken"),
			Topics:      []common.Hash{common.HexToHash("0xddf252"), common.HexToHash("0xfrom"), common.HexToHash("0xto")},
			Data:        []byte{0x01},
			BlockNumber: 100,
			TxHash:      common.HexToHash("0xtx"),
			TxIndex:     0,
			Index:       0,
		},
		{
			Address:     common.HexToAddress("0xtoken"),
			Topics:      []common.Hash{common.HexToHash("0xapproval")},
			Data:        []byte{0x02},
			BlockNumber: 100,
			TxHash:      common.HexToHash("0xtx"),
			TxIndex:     0,
			Index:       1,
			Removed:     true,
		},
	}

	events, err := tp.ParseLogs(logs)
	require.NoError(t, err)
	require.Len(t, events, 2)

	assert.Equal(t, logs[0].Address, events[0].Address)
	assert.Equal(t, logs[0].Topics, events[0].Topics)
	assert.Equal(t, uint64(100), events[0].BlockNumber)
	assert.Equal(t, uint(0), events[0].LogIndex)
	assert.False(t, events[0].Removed)

	assert.Equal(t, uint(1), events[1].LogIndex)
	assert.True(t, events[1].Removed)
}

func TestTransactionParser_ParseLogs_Empty(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	tp := adapter.TransactionParser()

	events, err := tp.ParseLogs(nil)
	require.NoError(t, err)
	assert.Empty(t, events)
}

// --- IsContractCreation tests ---

func TestTransactionParser_IsContractCreation_True(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	tp := adapter.TransactionParser()

	// Contract creation: To == nil
	tx := types.NewTx(&types.LegacyTx{
		To: nil,
	})

	assert.True(t, tp.IsContractCreation(tx))
}

func TestTransactionParser_IsContractCreation_False(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	tp := adapter.TransactionParser()

	to := common.HexToAddress("0xabcd")
	tx := types.NewTx(&types.LegacyTx{
		To: &to,
	})

	assert.False(t, tp.IsContractCreation(tx))
}

// --- GetContractAddress tests ---

func TestTransactionParser_GetContractAddress_ContractCreation(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	tp := adapter.TransactionParser()

	contractAddr := common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12")
	tx := types.NewTx(&types.LegacyTx{To: nil})
	receipt := &types.Receipt{
		ContractAddress: contractAddr,
	}

	addr := tp.GetContractAddress(tx, receipt)
	require.NotNil(t, addr)
	assert.Equal(t, contractAddr, *addr)
}

func TestTransactionParser_GetContractAddress_NotCreation(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	tp := adapter.TransactionParser()

	to := common.HexToAddress("0xexisting")
	tx := types.NewTx(&types.LegacyTx{To: &to})
	receipt := &types.Receipt{
		ContractAddress: common.HexToAddress("0xsomeaddr"),
	}

	assert.Nil(t, tp.GetContractAddress(tx, receipt))
}

func TestTransactionParser_GetContractAddress_NilReceipt(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	tp := adapter.TransactionParser()

	tx := types.NewTx(&types.LegacyTx{To: nil})
	assert.Nil(t, tp.GetContractAddress(tx, nil))
}

func TestTransactionParser_GetContractAddress_ZeroAddress(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	tp := adapter.TransactionParser()

	tx := types.NewTx(&types.LegacyTx{To: nil})
	receipt := &types.Receipt{
		ContractAddress: common.Address{}, // zero address
	}

	assert.Nil(t, tp.GetContractAddress(tx, receipt))
}

// --- Interface compliance ---

func TestAdapter_ImplementsChainAdapter(t *testing.T) {
	var _ chain.Adapter = (*Adapter)(nil)
}

func TestBlockFetcher_ImplementsChainBlockFetcher(t *testing.T) {
	var _ chain.BlockFetcher = (*BlockFetcher)(nil)
}

func TestTransactionParser_ImplementsChainTransactionParser(t *testing.T) {
	var _ chain.TransactionParser = (*TransactionParser)(nil)
}
