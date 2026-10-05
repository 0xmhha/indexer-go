package api

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/jsonrpc"
	sc "github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// --- Mock storage with system contract data ---

type mockSystemContractStorage struct {
	sc.SystemContractReader
	totalSupply    *big.Int
	minters        []common.Address
	allowances     map[common.Address]*big.Int
	validators     []common.Address
	blacklisted    []common.Address
	proposals      []*sc.Proposal
	proposalByID   *sc.Proposal
	votes          []*sc.ProposalVote
	mintEvents     []*sc.MintEvent
	burnEvents     []*sc.BurnEvent
	authorizedAcct []common.Address
}

func (m *mockSystemContractStorage) GetTotalSupply(ctx context.Context) (*big.Int, error) {
	if m.totalSupply != nil {
		return m.totalSupply, nil
	}
	return big.NewInt(0), nil
}

func (m *mockSystemContractStorage) GetActiveMinters(ctx context.Context) ([]common.Address, error) {
	return m.minters, nil
}

func (m *mockSystemContractStorage) GetMinterAllowance(ctx context.Context, minter common.Address) (*big.Int, error) {
	if a, ok := m.allowances[minter]; ok {
		return a, nil
	}
	return big.NewInt(0), nil
}

func (m *mockSystemContractStorage) GetActiveValidators(ctx context.Context) ([]common.Address, error) {
	return m.validators, nil
}

func (m *mockSystemContractStorage) GetBlacklistedAddresses(ctx context.Context) ([]common.Address, error) {
	return m.blacklisted, nil
}

func (m *mockSystemContractStorage) GetProposals(ctx context.Context, contract common.Address, status sc.ProposalStatus, limit, offset int) ([]*sc.Proposal, error) {
	if m.proposals != nil {
		return m.proposals, nil
	}
	return []*sc.Proposal{}, nil
}

func (m *mockSystemContractStorage) GetProposalById(ctx context.Context, contract common.Address, proposalId *big.Int) (*sc.Proposal, error) {
	if m.proposalByID != nil {
		return m.proposalByID, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockSystemContractStorage) GetProposalVotes(ctx context.Context, contract common.Address, proposalId *big.Int) ([]*sc.ProposalVote, error) {
	if m.votes != nil {
		return m.votes, nil
	}
	return []*sc.ProposalVote{}, nil
}

func (m *mockSystemContractStorage) GetMintEvents(ctx context.Context, fromBlock, toBlock uint64, minter common.Address, limit, offset int) ([]*sc.MintEvent, error) {
	if m.mintEvents != nil {
		return m.mintEvents, nil
	}
	return []*sc.MintEvent{}, nil
}

func (m *mockSystemContractStorage) GetBurnEvents(ctx context.Context, fromBlock, toBlock uint64, burner common.Address, limit, offset int) ([]*sc.BurnEvent, error) {
	if m.burnEvents != nil {
		return m.burnEvents, nil
	}
	return []*sc.BurnEvent{}, nil
}

func (m *mockSystemContractStorage) GetMinterHistory(ctx context.Context, minter common.Address) ([]*sc.MinterConfigEvent, error) {
	return []*sc.MinterConfigEvent{}, nil
}

func (m *mockSystemContractStorage) GetValidatorHistory(ctx context.Context, validator common.Address) ([]*sc.ValidatorChangeEvent, error) {
	return []*sc.ValidatorChangeEvent{}, nil
}

func (m *mockSystemContractStorage) GetGasTipHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*sc.GasTipUpdateEvent, error) {
	return []*sc.GasTipUpdateEvent{}, nil
}

func (m *mockSystemContractStorage) GetMinterConfigHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*sc.MinterConfigEvent, error) {
	return []*sc.MinterConfigEvent{}, nil
}

func (m *mockSystemContractStorage) GetEmergencyPauseHistory(ctx context.Context, contract common.Address) ([]*sc.EmergencyPauseEvent, error) {
	return []*sc.EmergencyPauseEvent{}, nil
}

func (m *mockSystemContractStorage) GetDepositMintProposals(ctx context.Context, fromBlock, toBlock uint64, status sc.ProposalStatus) ([]*sc.DepositMintProposal, error) {
	return []*sc.DepositMintProposal{}, nil
}

func (m *mockSystemContractStorage) GetBurnHistory(ctx context.Context, fromBlock, toBlock uint64, user common.Address) ([]*sc.BurnEvent, error) {
	return []*sc.BurnEvent{}, nil
}

func (m *mockSystemContractStorage) GetBlacklistHistory(ctx context.Context, address common.Address) ([]*sc.BlacklistEvent, error) {
	return []*sc.BlacklistEvent{}, nil
}

func (m *mockSystemContractStorage) GetAuthorizedAccounts(ctx context.Context) ([]common.Address, error) {
	return m.authorizedAcct, nil
}

func (m *mockSystemContractStorage) GetMemberHistory(ctx context.Context, contract common.Address) ([]*sc.MemberChangeEvent, error) {
	return []*sc.MemberChangeEvent{}, nil
}

func (m *mockSystemContractStorage) GetMaxProposalsUpdateHistory(ctx context.Context, contract common.Address) ([]*sc.MaxProposalsUpdateEvent, error) {
	return nil, nil
}

func (m *mockSystemContractStorage) GetProposalExecutionSkippedEvents(ctx context.Context, contract common.Address, proposalID *big.Int) ([]*sc.ProposalExecutionSkippedEvent, error) {
	return nil, nil
}

func TestSystemContractMethods(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	minter1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	minter2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	validator1 := common.HexToAddress("0x3333333333333333333333333333333333333333")
	blacklisted1 := common.HexToAddress("0x4444444444444444444444444444444444444444")

	store := &mockSystemContractStorage{
		totalSupply: big.NewInt(1000000000),
		minters:     []common.Address{minter1, minter2},
		allowances: map[common.Address]*big.Int{
			minter1: big.NewInt(500000),
			minter2: big.NewInt(300000),
		},
		validators:  []common.Address{validator1},
		blacklisted: []common.Address{blacklisted1},
	}

	h := &rpcHandler{storage: store, logger: logger}

	t.Run("GetTotalSupply", func(t *testing.T) {
		result, err := h.getTotalSupply(ctx, json.RawMessage(`{}`))
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		assert.Equal(t, "1000000000", m["totalSupply"])
	})

	t.Run("GetActiveMinters", func(t *testing.T) {
		result, err := h.getActiveMinters(ctx, json.RawMessage(`{}`))
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		minters := m["minters"].([]map[string]interface{})
		assert.Len(t, minters, 2)
		assert.Equal(t, "500000", minters[0]["allowance"])
		assert.Equal(t, true, minters[0]["isActive"])
	})

	t.Run("GetMinterAllowance", func(t *testing.T) {
		params := json.RawMessage(`{"minter": "0x1111111111111111111111111111111111111111"}`)
		result, err := h.getMinterAllowance(ctx, params)
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		assert.Equal(t, "500000", m["allowance"])
	})

	t.Run("GetMinterAllowance_MissingParam", func(t *testing.T) {
		_, err := h.getMinterAllowance(ctx, json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, jsonrpc.InvalidParams, err.Code)
	})

	t.Run("GetActiveValidators", func(t *testing.T) {
		result, err := h.getActiveValidators(ctx, json.RawMessage(`{}`))
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		validators := m["validators"].([]map[string]interface{})
		assert.Len(t, validators, 1)
		assert.Equal(t, true, validators[0]["isActive"])
	})

	t.Run("GetBlacklistedAddresses", func(t *testing.T) {
		result, err := h.getBlacklistedAddresses(ctx, json.RawMessage(`{}`))
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		addrs := m["addresses"].([]string)
		assert.Len(t, addrs, 1)
	})

	t.Run("GetProposals", func(t *testing.T) {
		execTime := uint64(1234567890)
		store.proposals = []*sc.Proposal{
			{
				Contract:          common.HexToAddress("0xcontract"),
				ProposalID:        big.NewInt(1),
				Proposer:          common.HexToAddress("0xproposer"),
				ActionType:        [32]byte{0, 0, 0, 1},
				CallData:          []byte{0x01, 0x02},
				MemberVersion:     big.NewInt(1),
				RequiredApprovals: 3,
				Approved:          2,
				Rejected:          0,
				Status:            sc.ProposalStatusVoting,
				CreatedAt:         1234567800,
				BlockNumber:       50,
				TxHash:            common.HexToHash("0xtx"),
				ExecutedAt:        &execTime,
			},
		}

		params := json.RawMessage(`{"contract": "0xcontract"}`)
		result, err := h.getProposals(ctx, params)
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		proposals := m["proposals"].([]map[string]interface{})
		assert.Len(t, proposals, 1)
		assert.Equal(t, "voting", proposals[0]["status"])
		assert.EqualValues(t, 3, proposals[0]["requiredApprovals"])
		assert.NotNil(t, proposals[0]["executedAt"])
	})

	t.Run("GetProposals_MissingContract", func(t *testing.T) {
		_, err := h.getProposals(ctx, json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, jsonrpc.InvalidParams, err.Code)
	})

	t.Run("GetProposal_ById", func(t *testing.T) {
		store.proposalByID = &sc.Proposal{
			Contract:          common.HexToAddress("0xcontract"),
			ProposalID:        big.NewInt(42),
			Proposer:          common.HexToAddress("0xproposer"),
			ActionType:        [32]byte{0, 0, 0, 1},
			CallData:          []byte{},
			MemberVersion:     big.NewInt(1),
			RequiredApprovals: 2,
			Approved:          2,
			Rejected:          0,
			Status:            sc.ProposalStatusApproved,
			CreatedAt:         1234567800,
			BlockNumber:       50,
			TxHash:            common.HexToHash("0xtx"),
		}

		params := json.RawMessage(`{"contract": "0xcontract", "proposalId": "42"}`)
		result, err := h.getProposal(ctx, params)
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		assert.Equal(t, "approved", m["status"])
		assert.Equal(t, "42", m["proposalId"])
	})

	t.Run("GetProposal_MissingParams", func(t *testing.T) {
		_, err := h.getProposal(ctx, json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, jsonrpc.InvalidParams, err.Code)

		_, err = h.getProposal(ctx, json.RawMessage(`{"contract": "0x1"}`))
		require.NotNil(t, err)
		assert.Equal(t, jsonrpc.InvalidParams, err.Code)
	})

	t.Run("GetProposal_InvalidID", func(t *testing.T) {
		params := json.RawMessage(`{"contract": "0xcontract", "proposalId": "not-a-number"}`)
		_, err := h.getProposal(ctx, params)
		require.NotNil(t, err)
		assert.Equal(t, jsonrpc.InvalidParams, err.Code)
	})

	t.Run("GetProposalVotes", func(t *testing.T) {
		store.votes = []*sc.ProposalVote{
			{
				Contract:    common.HexToAddress("0xcontract"),
				ProposalID:  big.NewInt(1),
				Voter:       common.HexToAddress("0xvoter1"),
				Approval:    true,
				BlockNumber: 55,
				TxHash:      common.HexToHash("0xvotetx1"),
				Timestamp:   1234567850,
			},
		}

		params := json.RawMessage(`{"contract": "0xcontract", "proposalId": "1"}`)
		result, err := h.getProposalVotes(ctx, params)
		require.Nil(t, err)

		m := result.(map[string]interface{})
		votes := m["votes"].([]map[string]interface{})
		assert.Len(t, votes, 1)
		assert.Equal(t, true, votes[0]["approval"])
	})

	t.Run("GetProposalVotes_MissingParams", func(t *testing.T) {
		_, err := h.getProposalVotes(ctx, json.RawMessage(`{"contract": "0x1"}`))
		require.NotNil(t, err)
		assert.Equal(t, jsonrpc.InvalidParams, err.Code)
	})

	t.Run("GetMintEvents", func(t *testing.T) {
		store.mintEvents = []*sc.MintEvent{
			{
				BlockNumber: 10,
				TxHash:      common.HexToHash("0xminttx"),
				Minter:      minter1,
				To:          common.HexToAddress("0xreceiver"),
				Amount:      big.NewInt(1000),
				Timestamp:   1234567890,
			},
		}

		result, err := h.getMintEvents(ctx, json.RawMessage(`{}`))
		require.Nil(t, err)

		m := result.(map[string]interface{})
		events := m["events"].([]map[string]interface{})
		assert.Len(t, events, 1)
		assert.Equal(t, "1000", events[0]["amount"])
	})

	t.Run("GetMintEvents_WithFilter", func(t *testing.T) {
		params := json.RawMessage(`{"fromBlock": 1, "toBlock": 100, "minter": "0x1111111111111111111111111111111111111111", "limit": 50}`)
		result, err := h.getMintEvents(ctx, params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetBurnEvents", func(t *testing.T) {
		store.burnEvents = []*sc.BurnEvent{
			{
				BlockNumber:  20,
				TxHash:       common.HexToHash("0xburntx"),
				Burner:       common.HexToAddress("0xburner"),
				Amount:       big.NewInt(500),
				Timestamp:    1234567891,
				WithdrawalID: "withdrawal-123",
			},
		}

		result, err := h.getBurnEvents(ctx, json.RawMessage(`{}`))
		require.Nil(t, err)

		m := result.(map[string]interface{})
		events := m["events"].([]map[string]interface{})
		assert.Len(t, events, 1)
		assert.Equal(t, "500", events[0]["amount"])
		assert.Equal(t, "withdrawal-123", events[0]["withdrawalId"])
	})

	t.Run("GetBurnEvents_WithoutWithdrawalID", func(t *testing.T) {
		store.burnEvents = []*sc.BurnEvent{
			{
				BlockNumber: 20,
				TxHash:      common.HexToHash("0xburntx2"),
				Burner:      common.HexToAddress("0xburner"),
				Amount:      big.NewInt(200),
				Timestamp:   1234567892,
			},
		}

		result, err := h.getBurnEvents(ctx, json.RawMessage(`{}`))
		require.Nil(t, err)

		m := result.(map[string]interface{})
		events := m["events"].([]map[string]interface{})
		assert.Len(t, events, 1)
		_, hasWithdrawal := events[0]["withdrawalId"]
		assert.False(t, hasWithdrawal)
	})

	// Total supply of an index without supply data
	t.Run("GetTotalSupply_ZeroWhenNotIndexed", func(t *testing.T) {
		db, dbErr := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
		require.NoError(t, dbErr)
		defer func() { _ = db.Close() }()
		empty := &rpcHandler{storage: sc.NewStore(db, logger), logger: logger}
		result, err := empty.getTotalSupply(ctx, json.RawMessage(`{}`))
		require.Nil(t, err)
		require.NotNil(t, result)
		m := result.(map[string]interface{})
		assert.Equal(t, "0", m["totalSupply"])
	})
}

func TestParseProposalStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected sc.ProposalStatus
	}{
		{"none", sc.ProposalStatusNone},
		{"NONE", sc.ProposalStatusNone},
		{"voting", sc.ProposalStatusVoting},
		{"VOTING", sc.ProposalStatusVoting},
		{"approved", sc.ProposalStatusApproved},
		{"APPROVED", sc.ProposalStatusApproved},
		{"executed", sc.ProposalStatusExecuted},
		{"EXECUTED", sc.ProposalStatusExecuted},
		{"cancelled", sc.ProposalStatusCancelled},
		{"CANCELLED", sc.ProposalStatusCancelled},
		{"expired", sc.ProposalStatusExpired},
		{"EXPIRED", sc.ProposalStatusExpired},
		{"failed", sc.ProposalStatusFailed},
		{"FAILED", sc.ProposalStatusFailed},
		{"rejected", sc.ProposalStatusRejected},
		{"REJECTED", sc.ProposalStatusRejected},
		{"unknown", sc.ProposalStatusNone},
		{"", sc.ProposalStatusNone},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseProposalStatus(tt.input))
		})
	}
}

func TestProposalStatusToString(t *testing.T) {
	tests := []struct {
		input    sc.ProposalStatus
		expected string
	}{
		{sc.ProposalStatusNone, "none"},
		{sc.ProposalStatusVoting, "voting"},
		{sc.ProposalStatusApproved, "approved"},
		{sc.ProposalStatusExecuted, "executed"},
		{sc.ProposalStatusCancelled, "cancelled"},
		{sc.ProposalStatusExpired, "expired"},
		{sc.ProposalStatusFailed, "failed"},
		{sc.ProposalStatusRejected, "rejected"},
		{sc.ProposalStatus(99), "none"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, proposalStatusToString(tt.input))
		})
	}
}
