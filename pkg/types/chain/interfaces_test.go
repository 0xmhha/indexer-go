package chain

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
)

// --- Constants tests ---

func TestChainTypeConstants(t *testing.T) {
	assert.Equal(t, ChainType("evm"), ChainTypeEVM)
	assert.Equal(t, ChainType("cosmos"), ChainTypeCosmos)
}

func TestConsensusTypeConstants(t *testing.T) {
	assert.Equal(t, ConsensusType("wbft"), ConsensusTypeWBFT)
	assert.Equal(t, ConsensusType("poa"), ConsensusTypePoA)
	assert.Equal(t, ConsensusType("pos"), ConsensusTypePoS)
	assert.Equal(t, ConsensusType("tendermint"), ConsensusTypeTendermint)
	assert.Equal(t, ConsensusType("pow"), ConsensusTypePoW)
}

// --- ChainInfo tests ---

func TestChainInfo_Fields(t *testing.T) {
	info := &ChainInfo{
		ChainID:        big.NewInt(1),
		ChainType:      ChainTypeEVM,
		ConsensusType:  ConsensusTypePoS,
		Name:           "Ethereum",
		NativeCurrency: "ETH",
		Decimals:       18,
	}

	assert.Equal(t, big.NewInt(1), info.ChainID)
	assert.Equal(t, ChainTypeEVM, info.ChainType)
	assert.Equal(t, ConsensusTypePoS, info.ConsensusType)
	assert.Equal(t, "Ethereum", info.Name)
	assert.Equal(t, "ETH", info.NativeCurrency)
	assert.Equal(t, 18, info.Decimals)
}

// --- TransactionData tests ---

func TestTransactionData_Fields(t *testing.T) {
	to := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	td := &TransactionData{
		Hash:        common.HexToHash("0xtxhash"),
		From:        common.HexToAddress("0xfrom"),
		To:          &to,
		Value:       big.NewInt(1000),
		GasPrice:    big.NewInt(20000000000),
		GasLimit:    21000,
		GasUsed:     21000,
		Nonce:       5,
		BlockNumber: 100,
		Status:      1,
		Metadata:    map[string]interface{}{"key": "value"},
	}

	assert.Equal(t, uint64(100), td.BlockNumber)
	assert.Equal(t, uint64(1), td.Status)
	assert.NotNil(t, td.To)
	assert.NotNil(t, td.Metadata)
	assert.Equal(t, "value", td.Metadata["key"])
}

func TestTransactionData_NilTo(t *testing.T) {
	td := &TransactionData{
		Hash:            common.HexToHash("0xcreation"),
		To:              nil,
		ContractAddress: &common.Address{0x01},
	}
	assert.Nil(t, td.To)
	assert.NotNil(t, td.ContractAddress)
}

// --- EventData tests ---

func TestEventData_Fields(t *testing.T) {
	ed := &EventData{
		Address:     common.HexToAddress("0xcontract"),
		Topics:      []common.Hash{common.HexToHash("0xevent")},
		Data:        []byte{0x01, 0x02},
		BlockNumber: 100,
		TxHash:      common.HexToHash("0xtx"),
		TxIndex:     0,
		LogIndex:    3,
		Removed:     false,
		EventName:   "Transfer",
		Decoded:     map[string]interface{}{"from": "0x0", "to": "0x1"},
	}

	assert.Equal(t, uint64(100), ed.BlockNumber)
	assert.Equal(t, uint(3), ed.LogIndex)
	assert.False(t, ed.Removed)
	assert.Equal(t, "Transfer", ed.EventName)
	assert.Len(t, ed.Decoded, 2)
}

// --- ConsensusData tests ---

func TestConsensusData_Fields(t *testing.T) {
	epochNum := uint64(5)
	cd := &ConsensusData{
		ConsensusType:     ConsensusTypeWBFT,
		BlockNumber:       100,
		BlockHash:         common.HexToHash("0xblock"),
		ProposerAddress:   common.HexToAddress("0xproposer"),
		ParticipationRate: 66.67,
		ValidatorCount:    3,
		SignedValidators:  []common.Address{common.HexToAddress("0x01"), common.HexToAddress("0x02")},
		IsEpochBoundary:   true,
		EpochNumber:       &epochNum,
		EpochValidators:   []common.Address{common.HexToAddress("0x01")},
	}

	assert.Equal(t, ConsensusTypeWBFT, cd.ConsensusType)
	assert.Equal(t, 3, cd.ValidatorCount)
	assert.Len(t, cd.SignedValidators, 2)
	assert.True(t, cd.IsEpochBoundary)
	assert.Equal(t, uint64(5), *cd.EpochNumber)
}

func TestConsensusData_NilEpoch(t *testing.T) {
	cd := &ConsensusData{
		ConsensusType:   ConsensusTypePoS,
		IsEpochBoundary: false,
		EpochNumber:     nil,
	}
	assert.Nil(t, cd.EpochNumber)
}

// --- SystemContractEvent tests ---

func TestSystemContractEvent_Fields(t *testing.T) {
	ev := &SystemContractEvent{
		ContractAddress: common.HexToAddress("0xgov"),
		ContractName:    "Governance",
		EventName:       "ProposalCreated",
		BlockNumber:     200,
		TxHash:          common.HexToHash("0xtx"),
		LogIndex:        1,
		Data:            map[string]interface{}{"proposalId": "1"},
	}

	assert.Equal(t, "Governance", ev.ContractName)
	assert.Equal(t, "ProposalCreated", ev.EventName)
	assert.Equal(t, uint64(200), ev.BlockNumber)
	assert.Contains(t, ev.Data, "proposalId")
}

// --- AdapterConfig tests ---

func TestAdapterConfig_Fields(t *testing.T) {
	cfg := &AdapterConfig{
		ChainType:     ChainTypeEVM,
		ConsensusType: ConsensusTypeWBFT,
		ChainID:       big.NewInt(137),
		RPCEndpoint:   "http://localhost:8545",
		WSEndpoint:    "ws://localhost:8546",
		Settings:      map[string]interface{}{"epochLength": 10},
	}

	assert.Equal(t, ChainTypeEVM, cfg.ChainType)
	assert.Equal(t, big.NewInt(137), cfg.ChainID)
	assert.Equal(t, "http://localhost:8545", cfg.RPCEndpoint)
	assert.Equal(t, "ws://localhost:8546", cfg.WSEndpoint)
}

// --- Interface compliance tests ---
// These verify that mock implementations can satisfy the interfaces.

type mockAdapter struct{}

func (m *mockAdapter) Info() *ChainInfo                        { return &ChainInfo{} }
func (m *mockAdapter) BlockFetcher() BlockFetcher              { return nil }
func (m *mockAdapter) TransactionParser() TransactionParser    { return nil }
func (m *mockAdapter) ConsensusParser() ConsensusParser        { return nil }
func (m *mockAdapter) SystemContracts() SystemContractsHandler { return nil }
func (m *mockAdapter) Close() error                            { return nil }

func TestAdapter_InterfaceCompliance(t *testing.T) {
	var _ Adapter = (*mockAdapter)(nil)
	a := &mockAdapter{}
	assert.NotNil(t, a.Info())
	assert.Nil(t, a.BlockFetcher())
	assert.NoError(t, a.Close())
}

type mockBlockFetcher struct{}

func (m *mockBlockFetcher) GetLatestBlockNumber(_ context.Context) (uint64, error) { return 0, nil }
func (m *mockBlockFetcher) GetBlockByNumber(_ context.Context, _ uint64) (*types.Block, error) {
	return nil, nil
}
func (m *mockBlockFetcher) GetBlockByHash(_ context.Context, _ common.Hash) (*types.Block, error) {
	return nil, nil
}
func (m *mockBlockFetcher) GetBlockReceipts(_ context.Context, _ uint64) (types.Receipts, error) {
	return nil, nil
}
func (m *mockBlockFetcher) GetTransactionByHash(_ context.Context, _ common.Hash) (*types.Transaction, bool, error) {
	return nil, false, nil
}
func (m *mockBlockFetcher) BalanceAt(_ context.Context, _ common.Address, _ *big.Int) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (m *mockBlockFetcher) Close() {}

func TestBlockFetcher_InterfaceCompliance(t *testing.T) {
	var _ BlockFetcher = (*mockBlockFetcher)(nil)
}

type mockTransactionParser struct{}

func (m *mockTransactionParser) ParseTransaction(_ *types.Transaction, _ *types.Receipt) (*TransactionData, error) {
	return nil, nil
}
func (m *mockTransactionParser) ParseLogs(_ []*types.Log) ([]*EventData, error) { return nil, nil }
func (m *mockTransactionParser) IsContractCreation(_ *types.Transaction) bool    { return false }
func (m *mockTransactionParser) GetContractAddress(_ *types.Transaction, _ *types.Receipt) *common.Address {
	return nil
}

func TestTransactionParser_InterfaceCompliance(t *testing.T) {
	var _ TransactionParser = (*mockTransactionParser)(nil)
}

type mockConsensusParser struct{}

func (m *mockConsensusParser) ConsensusType() ConsensusType { return ConsensusTypePoS }
func (m *mockConsensusParser) ParseConsensusData(_ *types.Block) (*ConsensusData, error) {
	return nil, nil
}
func (m *mockConsensusParser) GetValidators(_ context.Context, _ uint64) ([]common.Address, error) {
	return nil, nil
}
func (m *mockConsensusParser) IsEpochBoundary(_ *types.Block) bool { return false }

func TestConsensusParser_InterfaceCompliance(t *testing.T) {
	var _ ConsensusParser = (*mockConsensusParser)(nil)
}

type mockSystemContractsHandler struct{}

func (m *mockSystemContractsHandler) IsSystemContract(_ common.Address) bool     { return false }
func (m *mockSystemContractsHandler) GetSystemContractName(_ common.Address) string { return "" }
func (m *mockSystemContractsHandler) GetSystemContractAddresses() []common.Address  { return nil }
func (m *mockSystemContractsHandler) ParseSystemContractEvent(_ *types.Log) (*SystemContractEvent, error) {
	return nil, nil
}

func TestSystemContractsHandler_InterfaceCompliance(t *testing.T) {
	var _ SystemContractsHandler = (*mockSystemContractsHandler)(nil)
}

type mockAdapterFactory struct{}

func (m *mockAdapterFactory) CreateAdapter(_ *AdapterConfig) (Adapter, error) { return nil, nil }
func (m *mockAdapterFactory) SupportedChains() []ChainType                    { return nil }
func (m *mockAdapterFactory) SupportedConsensus() []ConsensusType             { return nil }

func TestAdapterFactory_InterfaceCompliance(t *testing.T) {
	var _ AdapterFactory = (*mockAdapterFactory)(nil)
}
