package stableone

import (
	"context"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- Mock Client (implements evm.Client) ---

type mockClient struct {
	closeCalled bool
}

func (m *mockClient) GetLatestBlockNumber(_ context.Context) (uint64, error) {
	return 0, nil
}
func (m *mockClient) GetBlockByNumber(_ context.Context, _ uint64) (*types.Block, error) {
	return nil, nil
}
func (m *mockClient) GetBlockByHash(_ context.Context, _ common.Hash) (*types.Block, error) {
	return nil, nil
}
func (m *mockClient) GetBlockReceipts(_ context.Context, _ uint64) (types.Receipts, error) {
	return nil, nil
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

// addrToHash converts an address to a topic hash (left-padded to 32 bytes)
func addrToHash(addr common.Address) common.Hash {
	return common.BytesToHash(addr.Bytes())
}

// --- DefaultConfig tests ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, big.NewInt(1), cfg.ChainID)
	assert.Equal(t, uint64(constants.DefaultEpochLength), cfg.EpochLength)
}

// --- NewAdapter tests ---

func TestNewAdapter(t *testing.T) {
	mc := &mockClient{}
	adapter := NewAdapter(mc, nil, zap.NewNop())

	assert.NotNil(t, adapter)
	assert.NotNil(t, adapter.Adapter) // embedded EVM adapter
	assert.NotNil(t, adapter.consensusParser)
	assert.NotNil(t, adapter.systemContracts)
}

func TestNewAdapter_NilConfig(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	assert.Equal(t, uint64(constants.DefaultEpochLength), adapter.GetEpochLength())
}

func TestNewAdapter_CustomConfig(t *testing.T) {
	cfg := &Config{
		ChainID:     big.NewInt(999),
		EpochLength: 20,
	}
	adapter := NewAdapter(&mockClient{}, cfg, zap.NewNop())
	assert.Equal(t, uint64(20), adapter.GetEpochLength())
}

// --- Info tests ---

func TestAdapter_Info(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	info := adapter.Info()

	assert.Equal(t, chain.ChainTypeEVM, info.ChainType)
	assert.Equal(t, chain.ConsensusTypeWBFT, info.ConsensusType)
	assert.Equal(t, constants.DefaultNativeTokenName, info.Name)
	assert.Equal(t, constants.DefaultNativeTokenSymbol, info.NativeCurrency)
	assert.Equal(t, constants.DefaultNativeTokenDecimals, info.Decimals)
}

// --- ConsensusParser tests ---

func TestAdapter_ConsensusParser(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	cp := adapter.ConsensusParser()
	assert.NotNil(t, cp)
	assert.Equal(t, chain.ConsensusTypeWBFT, cp.ConsensusType())
}

// --- SystemContracts tests ---

func TestAdapter_SystemContracts(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, nil, zap.NewNop())
	sc := adapter.SystemContracts()
	assert.NotNil(t, sc)
}

// --- GetEpochNumber tests ---

func TestAdapter_GetEpochNumber(t *testing.T) {
	adapter := NewAdapter(&mockClient{}, &Config{ChainID: big.NewInt(1), EpochLength: 10}, zap.NewNop())

	assert.Equal(t, uint64(0), adapter.GetEpochNumber(0))
	assert.Equal(t, uint64(0), adapter.GetEpochNumber(5))
	assert.Equal(t, uint64(1), adapter.GetEpochNumber(10))
	assert.Equal(t, uint64(1), adapter.GetEpochNumber(15))
	assert.Equal(t, uint64(2), adapter.GetEpochNumber(20))
	assert.Equal(t, uint64(10), adapter.GetEpochNumber(100))
}

// --- Close tests ---

func TestAdapter_Close(t *testing.T) {
	mc := &mockClient{}
	adapter := NewAdapter(mc, nil, zap.NewNop())

	err := adapter.Close()
	require.NoError(t, err)
	assert.True(t, mc.closeCalled)
}

// --- Interface compliance ---

func TestAdapter_ImplementsChainAdapter(t *testing.T) {
	var _ chain.Adapter = (*Adapter)(nil)
}

// ========================================================================
// SystemContractsHandler tests
// ========================================================================

func TestNewSystemContractsHandler(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())
	assert.NotNil(t, handler)
	assert.NotEmpty(t, handler.contractNames)
	assert.NotEmpty(t, handler.eventSigToName)
}

// --- IsSystemContract tests ---

func TestIsSystemContract_True(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	assert.True(t, handler.IsSystemContract(constants.NativeCoinAdapterAddress))
	assert.True(t, handler.IsSystemContract(constants.GovValidatorAddress))
	assert.True(t, handler.IsSystemContract(constants.GovMasterMinterAddress))
	assert.True(t, handler.IsSystemContract(constants.GovMinterAddress))
	assert.True(t, handler.IsSystemContract(constants.GovCouncilAddress))
}

func TestIsSystemContract_False(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())
	assert.False(t, handler.IsSystemContract(common.HexToAddress("0xdeadbeef")))
	assert.False(t, handler.IsSystemContract(common.Address{}))
}

// --- GetSystemContractName tests ---

func TestGetSystemContractName(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	assert.Equal(t, "NativeCoinAdapter", handler.GetSystemContractName(constants.NativeCoinAdapterAddress))
	assert.Equal(t, "GovValidator", handler.GetSystemContractName(constants.GovValidatorAddress))
	assert.Equal(t, "GovMasterMinter", handler.GetSystemContractName(constants.GovMasterMinterAddress))
	assert.Equal(t, "GovMinter", handler.GetSystemContractName(constants.GovMinterAddress))
	assert.Equal(t, "GovCouncil", handler.GetSystemContractName(constants.GovCouncilAddress))
}

func TestGetSystemContractName_Unknown(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())
	assert.Equal(t, "", handler.GetSystemContractName(common.HexToAddress("0x9999")))
}

// --- GetSystemContractAddresses tests ---

func TestGetSystemContractAddresses(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())
	addrs := handler.GetSystemContractAddresses()

	assert.Len(t, addrs, 5)
	assert.Contains(t, addrs, constants.NativeCoinAdapterAddress)
	assert.Contains(t, addrs, constants.GovValidatorAddress)
	assert.Contains(t, addrs, constants.GovCouncilAddress)
}

// --- GetContractType tests ---

func TestGetContractType(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	assert.Equal(t, "token", handler.GetContractType(constants.NativeCoinAdapterAddress))
	assert.Equal(t, "governance", handler.GetContractType(constants.GovValidatorAddress))
	assert.Equal(t, "governance", handler.GetContractType(constants.GovMasterMinterAddress))
	assert.Equal(t, "minting", handler.GetContractType(constants.GovMinterAddress))
	assert.Equal(t, "governance", handler.GetContractType(constants.GovCouncilAddress))
	assert.Equal(t, "unknown", handler.GetContractType(common.HexToAddress("0xbeef")))
}

// --- GetEventABI tests ---

func TestGetEventABI_NotImplemented(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())
	_, err := handler.GetEventABI("Transfer")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

// --- ParseSystemContractEvent tests ---

func TestParseSystemContractEvent_NilLog(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())
	_, err := handler.ParseSystemContractEvent(nil)
	require.Error(t, err)
}

func TestParseSystemContractEvent_NotSystemContract(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())
	log := &types.Log{
		Address: common.HexToAddress("0xdeadbeef"),
		Topics:  []common.Hash{common.HexToHash("0x01")},
	}
	_, err := handler.ParseSystemContractEvent(log)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a system contract")
}

func TestParseSystemContractEvent_NoTopics(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())
	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{},
	}
	_, err := handler.ParseSystemContractEvent(log)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no topics")
}

func TestParseSystemContractEvent_Transfer(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	from := common.HexToAddress("0xaaaa")
	to := common.HexToAddress("0xbbbb")
	value := big.NewInt(1000000)

	// Build value as 32-byte padded
	valueBytes := common.LeftPadBytes(value.Bytes(), 32)

	log := &types.Log{
		Address:     constants.NativeCoinAdapterAddress,
		Topics:      []common.Hash{constants.EventSigTransfer, addrToHash(from), addrToHash(to)},
		Data:        valueBytes,
		BlockNumber: 100,
		TxHash:      common.HexToHash("0xtxhash"),
		Index:       0,
	}

	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err)
	assert.Equal(t, "Transfer", event.EventName)
	assert.Equal(t, "NativeCoinAdapter", event.ContractName)
	assert.Equal(t, uint64(100), event.BlockNumber)
	assert.Equal(t, value.String(), event.Data["value"])
}

func TestParseSystemContractEvent_Transfer_InsufficientTopics(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{constants.EventSigTransfer}, // only 1 topic, need 3
	}

	// Should still return event with basic info, just decode error logged
	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err) // decoding failure is non-fatal
	assert.Equal(t, "Transfer", event.EventName)
}

func TestParseSystemContractEvent_Mint(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	minter := common.HexToAddress("0x1111")
	to := common.HexToAddress("0x2222")
	amount := big.NewInt(500000)
	amountBytes := common.LeftPadBytes(amount.Bytes(), 32)

	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{constants.EventSigMint, addrToHash(minter), addrToHash(to)},
		Data:    amountBytes,
	}

	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err)
	assert.Equal(t, "Mint", event.EventName)
	assert.Equal(t, amount.String(), event.Data["amount"])
}

func TestParseSystemContractEvent_Burn(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	burner := common.HexToAddress("0x3333")
	amount := big.NewInt(100000)
	amountBytes := common.LeftPadBytes(amount.Bytes(), 32)

	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{constants.EventSigBurn, addrToHash(burner)},
		Data:    amountBytes,
	}

	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err)
	assert.Equal(t, "Burn", event.EventName)
	assert.Equal(t, amount.String(), event.Data["amount"])
}

func TestParseSystemContractEvent_MemberAdded(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	member := common.HexToAddress("0x4444")
	proposalId := big.NewInt(42)
	memberCount := big.NewInt(5)

	data := make([]byte, 64)
	copy(data[0:32], common.LeftPadBytes(proposalId.Bytes(), 32))
	copy(data[32:64], common.LeftPadBytes(memberCount.Bytes(), 32))

	log := &types.Log{
		Address: constants.GovCouncilAddress,
		Topics:  []common.Hash{constants.EventSigMemberAdded, addrToHash(member)},
		Data:    data,
	}

	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err)
	assert.Equal(t, "MemberAdded", event.EventName)
	assert.Equal(t, "GovCouncil", event.ContractName)
	assert.Equal(t, proposalId.String(), event.Data["proposalId"])
	assert.Equal(t, uint64(5), event.Data["memberCount"])
}

func TestParseSystemContractEvent_MemberRemoved(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	member := common.HexToAddress("0x5555")
	proposalId := big.NewInt(43)
	memberCount := big.NewInt(4)

	data := make([]byte, 64)
	copy(data[0:32], common.LeftPadBytes(proposalId.Bytes(), 32))
	copy(data[32:64], common.LeftPadBytes(memberCount.Bytes(), 32))

	log := &types.Log{
		Address: constants.GovCouncilAddress,
		Topics:  []common.Hash{constants.EventSigMemberRemoved, addrToHash(member)},
		Data:    data,
	}

	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err)
	assert.Equal(t, "MemberRemoved", event.EventName)
	assert.Equal(t, proposalId.String(), event.Data["proposalId"])
}

func TestParseSystemContractEvent_ProposalCreated(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	proposalId := big.NewInt(100)
	proposer := common.HexToAddress("0x6666")

	log := &types.Log{
		Address: constants.GovCouncilAddress,
		Topics:  []common.Hash{constants.EventSigProposalCreated, common.BigToHash(proposalId), addrToHash(proposer)},
	}

	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err)
	assert.Equal(t, "ProposalCreated", event.EventName)
	assert.Equal(t, proposalId.String(), event.Data["proposalId"])
}

func TestParseSystemContractEvent_ProposalVoted(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	proposalId := big.NewInt(100)
	voter := common.HexToAddress("0x7777")

	// approved = true → last byte of 32 bytes = 1
	data := make([]byte, 32)
	data[31] = 1

	log := &types.Log{
		Address: constants.GovCouncilAddress,
		Topics:  []common.Hash{constants.EventSigProposalVoted, common.BigToHash(proposalId), addrToHash(voter)},
		Data:    data,
	}

	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err)
	assert.Equal(t, "ProposalVoted", event.EventName)
	assert.Equal(t, true, event.Data["approved"])
}

func TestParseSystemContractEvent_ProposalExecuted(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	proposalId := big.NewInt(100)
	executor := common.HexToAddress("0x8888")

	data := make([]byte, 32)
	data[31] = 1 // success = true

	log := &types.Log{
		Address: constants.GovCouncilAddress,
		Topics:  []common.Hash{constants.EventSigProposalExecuted, common.BigToHash(proposalId), addrToHash(executor)},
		Data:    data,
	}

	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err)
	assert.Equal(t, "ProposalExecuted", event.EventName)
	assert.Equal(t, true, event.Data["success"])
}

func TestParseSystemContractEvent_AddressBlacklisted(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	account := common.HexToAddress("0x9999")
	proposalId := big.NewInt(50)
	data := common.LeftPadBytes(proposalId.Bytes(), 32)

	log := &types.Log{
		Address: constants.GovCouncilAddress,
		Topics:  []common.Hash{constants.EventSigAddressBlacklisted, addrToHash(account)},
		Data:    data,
	}

	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err)
	assert.Equal(t, "AddressBlacklisted", event.EventName)
	assert.Equal(t, proposalId.String(), event.Data["proposalId"])
}

func TestParseSystemContractEvent_AddressUnblacklisted(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	account := common.HexToAddress("0xaaaa")
	proposalId := big.NewInt(51)
	data := common.LeftPadBytes(proposalId.Bytes(), 32)

	log := &types.Log{
		Address: constants.GovCouncilAddress,
		Topics:  []common.Hash{constants.EventSigAddressUnblacklisted, addrToHash(account)},
		Data:    data,
	}

	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err)
	assert.Equal(t, "AddressUnblacklisted", event.EventName)
}

func TestParseSystemContractEvent_UnknownEvent(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	log := &types.Log{
		Address: constants.NativeCoinAdapterAddress,
		Topics:  []common.Hash{common.HexToHash("0xunknownsig")},
		Data:    []byte{0x01, 0x02, 0x03},
	}

	event, err := handler.ParseSystemContractEvent(log)
	require.NoError(t, err)
	// Unknown events store raw data
	assert.NotNil(t, event.Data["rawData"])
}

// --- GetTokenMetadata tests ---

func TestGetTokenMetadata(t *testing.T) {
	handler := NewSystemContractsHandler(zap.NewNop())

	meta := handler.GetTokenMetadata(constants.NativeCoinAdapterAddress)
	// May be nil if not configured, just ensure no panic
	_ = meta
}

// --- Interface compliance ---

func TestSystemContractsHandler_ImplementsInterface(t *testing.T) {
	var _ chain.SystemContractsHandler = (*SystemContractsHandler)(nil)
}
