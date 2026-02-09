package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- Mock storage with system contract data ---

type mockSystemContractStorage struct {
	*mockStorage
	totalSupply    *big.Int
	minters        []common.Address
	allowances     map[common.Address]*big.Int
	validators     []common.Address
	blacklisted    []common.Address
	proposals      []*storage.Proposal
	proposalByID   *storage.Proposal
	votes          []*storage.ProposalVote
	mintEvents     []*storage.MintEvent
	burnEvents     []*storage.BurnEvent
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

func (m *mockSystemContractStorage) GetProposals(ctx context.Context, contract common.Address, status storage.ProposalStatus, limit, offset int) ([]*storage.Proposal, error) {
	if m.proposals != nil {
		return m.proposals, nil
	}
	return []*storage.Proposal{}, nil
}

func (m *mockSystemContractStorage) GetProposalById(ctx context.Context, contract common.Address, proposalId *big.Int) (*storage.Proposal, error) {
	if m.proposalByID != nil {
		return m.proposalByID, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockSystemContractStorage) GetProposalVotes(ctx context.Context, contract common.Address, proposalId *big.Int) ([]*storage.ProposalVote, error) {
	if m.votes != nil {
		return m.votes, nil
	}
	return []*storage.ProposalVote{}, nil
}

func (m *mockSystemContractStorage) GetMintEvents(ctx context.Context, fromBlock, toBlock uint64, minter common.Address, limit, offset int) ([]*storage.MintEvent, error) {
	if m.mintEvents != nil {
		return m.mintEvents, nil
	}
	return []*storage.MintEvent{}, nil
}

func (m *mockSystemContractStorage) GetBurnEvents(ctx context.Context, fromBlock, toBlock uint64, burner common.Address, limit, offset int) ([]*storage.BurnEvent, error) {
	if m.burnEvents != nil {
		return m.burnEvents, nil
	}
	return []*storage.BurnEvent{}, nil
}

func (m *mockSystemContractStorage) GetMinterHistory(ctx context.Context, minter common.Address) ([]*storage.MinterConfigEvent, error) {
	return []*storage.MinterConfigEvent{}, nil
}

func (m *mockSystemContractStorage) GetValidatorHistory(ctx context.Context, validator common.Address) ([]*storage.ValidatorChangeEvent, error) {
	return []*storage.ValidatorChangeEvent{}, nil
}

func (m *mockSystemContractStorage) GetGasTipHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*storage.GasTipUpdateEvent, error) {
	return []*storage.GasTipUpdateEvent{}, nil
}

func (m *mockSystemContractStorage) GetMinterConfigHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*storage.MinterConfigEvent, error) {
	return []*storage.MinterConfigEvent{}, nil
}

func (m *mockSystemContractStorage) GetEmergencyPauseHistory(ctx context.Context, contract common.Address) ([]*storage.EmergencyPauseEvent, error) {
	return []*storage.EmergencyPauseEvent{}, nil
}

func (m *mockSystemContractStorage) GetDepositMintProposals(ctx context.Context, fromBlock, toBlock uint64, status storage.ProposalStatus) ([]*storage.DepositMintProposal, error) {
	return []*storage.DepositMintProposal{}, nil
}

func (m *mockSystemContractStorage) GetBurnHistory(ctx context.Context, fromBlock, toBlock uint64, user common.Address) ([]*storage.BurnEvent, error) {
	return []*storage.BurnEvent{}, nil
}

func (m *mockSystemContractStorage) GetBlacklistHistory(ctx context.Context, address common.Address) ([]*storage.BlacklistEvent, error) {
	return []*storage.BlacklistEvent{}, nil
}

func (m *mockSystemContractStorage) GetAuthorizedAccounts(ctx context.Context) ([]common.Address, error) {
	return m.authorizedAcct, nil
}

func (m *mockSystemContractStorage) GetMemberHistory(ctx context.Context, contract common.Address) ([]*storage.MemberChangeEvent, error) {
	return []*storage.MemberChangeEvent{}, nil
}

func (m *mockSystemContractStorage) GetMaxProposalsUpdateHistory(ctx context.Context, contract common.Address) ([]*storage.MaxProposalsUpdateEvent, error) {
	return nil, nil
}

func (m *mockSystemContractStorage) GetProposalExecutionSkippedEvents(ctx context.Context, contract common.Address, proposalID *big.Int) ([]*storage.ProposalExecutionSkippedEvent, error) {
	return nil, nil
}

// --- Mock storage with SetCode data ---

type mockSetCodeStorage struct {
	*mockStorage
	authsByTx        []*storage.SetCodeAuthorizationRecord
	authsByTarget    []*storage.SetCodeAuthorizationRecord
	authsByAuthority []*storage.SetCodeAuthorizationRecord
	authsByBlock     []*storage.SetCodeAuthorizationRecord
	recentAuths      []*storage.SetCodeAuthorizationRecord
	delegationState  *storage.AddressDelegationState
	setCodeStats     *storage.AddressSetCodeStats
	txCount          int
}

func (m *mockSetCodeStorage) GetSetCodeAuthorization(ctx context.Context, txHash common.Hash, authIndex int) (*storage.SetCodeAuthorizationRecord, error) {
	for _, r := range m.authsByTx {
		if r.TxHash == txHash && r.AuthIndex == authIndex {
			return r, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (m *mockSetCodeStorage) GetSetCodeAuthorizationsByTx(ctx context.Context, txHash common.Hash) ([]*storage.SetCodeAuthorizationRecord, error) {
	if m.authsByTx != nil {
		return m.authsByTx, nil
	}
	return []*storage.SetCodeAuthorizationRecord{}, nil
}

func (m *mockSetCodeStorage) GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, limit, offset int) ([]*storage.SetCodeAuthorizationRecord, error) {
	if m.authsByTarget != nil {
		return m.authsByTarget, nil
	}
	return []*storage.SetCodeAuthorizationRecord{}, nil
}

func (m *mockSetCodeStorage) GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, limit, offset int) ([]*storage.SetCodeAuthorizationRecord, error) {
	if m.authsByAuthority != nil {
		return m.authsByAuthority, nil
	}
	return []*storage.SetCodeAuthorizationRecord{}, nil
}

func (m *mockSetCodeStorage) GetSetCodeAuthorizationsByBlock(ctx context.Context, blockNumber uint64) ([]*storage.SetCodeAuthorizationRecord, error) {
	if m.authsByBlock != nil {
		return m.authsByBlock, nil
	}
	return []*storage.SetCodeAuthorizationRecord{}, nil
}

func (m *mockSetCodeStorage) GetAddressSetCodeStats(ctx context.Context, address common.Address) (*storage.AddressSetCodeStats, error) {
	if m.setCodeStats != nil {
		return m.setCodeStats, nil
	}
	return &storage.AddressSetCodeStats{}, nil
}

func (m *mockSetCodeStorage) GetAddressDelegationState(ctx context.Context, address common.Address) (*storage.AddressDelegationState, error) {
	if m.delegationState != nil {
		return m.delegationState, nil
	}
	return &storage.AddressDelegationState{}, nil
}

func (m *mockSetCodeStorage) GetSetCodeAuthorizationsCountByTarget(ctx context.Context, target common.Address) (int, error) {
	return len(m.authsByTarget), nil
}

func (m *mockSetCodeStorage) GetSetCodeAuthorizationsCountByAuthority(ctx context.Context, authority common.Address) (int, error) {
	return len(m.authsByAuthority), nil
}

func (m *mockSetCodeStorage) GetSetCodeTransactionCount(ctx context.Context) (int, error) {
	return m.txCount, nil
}

func (m *mockSetCodeStorage) GetRecentSetCodeAuthorizations(ctx context.Context, limit int) ([]*storage.SetCodeAuthorizationRecord, error) {
	if m.recentAuths != nil {
		return m.recentAuths, nil
	}
	return []*storage.SetCodeAuthorizationRecord{}, nil
}

// --- Tests ---

func TestSystemContractMethods(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	minter1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	minter2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	validator1 := common.HexToAddress("0x3333333333333333333333333333333333333333")
	blacklisted1 := common.HexToAddress("0x4444444444444444444444444444444444444444")

	store := &mockSystemContractStorage{
		mockStorage: &mockStorage{
			latestHeight: 100,
			blocks:       make(map[uint64]*types.Block),
			blocksByHash: make(map[common.Hash]*types.Block),
		},
		totalSupply: big.NewInt(1000000000),
		minters:     []common.Address{minter1, minter2},
		allowances: map[common.Address]*big.Int{
			minter1: big.NewInt(500000),
			minter2: big.NewInt(300000),
		},
		validators:  []common.Address{validator1},
		blacklisted: []common.Address{blacklisted1},
	}

	server := NewServer(store, logger)

	t.Run("GetTotalSupply", func(t *testing.T) {
		result, err := server.HandleMethodDirect(ctx, "getTotalSupply", json.RawMessage(`{}`))
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		assert.Equal(t, "1000000000", m["totalSupply"])
	})

	t.Run("GetActiveMinters", func(t *testing.T) {
		result, err := server.HandleMethodDirect(ctx, "getActiveMinters", json.RawMessage(`{}`))
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
		result, err := server.HandleMethodDirect(ctx, "getMinterAllowance", params)
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		assert.Equal(t, "500000", m["allowance"])
	})

	t.Run("GetMinterAllowance_MissingParam", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getMinterAllowance", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetActiveValidators", func(t *testing.T) {
		result, err := server.HandleMethodDirect(ctx, "getActiveValidators", json.RawMessage(`{}`))
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		validators := m["validators"].([]map[string]interface{})
		assert.Len(t, validators, 1)
		assert.Equal(t, true, validators[0]["isActive"])
	})

	t.Run("GetBlacklistedAddresses", func(t *testing.T) {
		result, err := server.HandleMethodDirect(ctx, "getBlacklistedAddresses", json.RawMessage(`{}`))
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		addrs := m["addresses"].([]string)
		assert.Len(t, addrs, 1)
	})

	t.Run("GetProposals", func(t *testing.T) {
		execTime := uint64(1234567890)
		store.proposals = []*storage.Proposal{
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
				Status:            storage.ProposalStatusVoting,
				CreatedAt:         1234567800,
				BlockNumber:       50,
				TxHash:            common.HexToHash("0xtx"),
				ExecutedAt:        &execTime,
			},
		}

		params := json.RawMessage(`{"contract": "0xcontract"}`)
		result, err := server.HandleMethodDirect(ctx, "getProposals", params)
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
		_, err := server.HandleMethodDirect(ctx, "getProposals", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetProposal_ById", func(t *testing.T) {
		store.proposalByID = &storage.Proposal{
			Contract:          common.HexToAddress("0xcontract"),
			ProposalID:        big.NewInt(42),
			Proposer:          common.HexToAddress("0xproposer"),
			ActionType:        [32]byte{0, 0, 0, 1},
			CallData:          []byte{},
			MemberVersion:     big.NewInt(1),
			RequiredApprovals: 2,
			Approved:          2,
			Rejected:          0,
			Status:            storage.ProposalStatusApproved,
			CreatedAt:         1234567800,
			BlockNumber:       50,
			TxHash:            common.HexToHash("0xtx"),
		}

		params := json.RawMessage(`{"contract": "0xcontract", "proposalId": "42"}`)
		result, err := server.HandleMethodDirect(ctx, "getProposal", params)
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		assert.Equal(t, "approved", m["status"])
		assert.Equal(t, "42", m["proposalId"])
	})

	t.Run("GetProposal_MissingParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getProposal", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)

		_, err = server.HandleMethodDirect(ctx, "getProposal", json.RawMessage(`{"contract": "0x1"}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetProposal_InvalidID", func(t *testing.T) {
		params := json.RawMessage(`{"contract": "0xcontract", "proposalId": "not-a-number"}`)
		_, err := server.HandleMethodDirect(ctx, "getProposal", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetProposalVotes", func(t *testing.T) {
		store.votes = []*storage.ProposalVote{
			{
				Contract:   common.HexToAddress("0xcontract"),
				ProposalID: big.NewInt(1),
				Voter:      common.HexToAddress("0xvoter1"),
				Approval:   true,
				BlockNumber: 55,
				TxHash:     common.HexToHash("0xvotetx1"),
				Timestamp:  1234567850,
			},
		}

		params := json.RawMessage(`{"contract": "0xcontract", "proposalId": "1"}`)
		result, err := server.HandleMethodDirect(ctx, "getProposalVotes", params)
		require.Nil(t, err)

		m := result.(map[string]interface{})
		votes := m["votes"].([]map[string]interface{})
		assert.Len(t, votes, 1)
		assert.Equal(t, true, votes[0]["approval"])
	})

	t.Run("GetProposalVotes_MissingParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getProposalVotes", json.RawMessage(`{"contract": "0x1"}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetMintEvents", func(t *testing.T) {
		store.mintEvents = []*storage.MintEvent{
			{
				BlockNumber: 10,
				TxHash:      common.HexToHash("0xminttx"),
				Minter:      minter1,
				To:          common.HexToAddress("0xreceiver"),
				Amount:      big.NewInt(1000),
				Timestamp:   1234567890,
			},
		}

		result, err := server.HandleMethodDirect(ctx, "getMintEvents", json.RawMessage(`{}`))
		require.Nil(t, err)

		m := result.(map[string]interface{})
		events := m["events"].([]map[string]interface{})
		assert.Len(t, events, 1)
		assert.Equal(t, "1000", events[0]["amount"])
	})

	t.Run("GetMintEvents_WithFilter", func(t *testing.T) {
		params := json.RawMessage(`{"fromBlock": 1, "toBlock": 100, "minter": "0x1111111111111111111111111111111111111111", "limit": 50}`)
		result, err := server.HandleMethodDirect(ctx, "getMintEvents", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetBurnEvents", func(t *testing.T) {
		store.burnEvents = []*storage.BurnEvent{
			{
				BlockNumber:  20,
				TxHash:       common.HexToHash("0xburntx"),
				Burner:       common.HexToAddress("0xburner"),
				Amount:       big.NewInt(500),
				Timestamp:    1234567891,
				WithdrawalID: "withdrawal-123",
			},
		}

		result, err := server.HandleMethodDirect(ctx, "getBurnEvents", json.RawMessage(`{}`))
		require.Nil(t, err)

		m := result.(map[string]interface{})
		events := m["events"].([]map[string]interface{})
		assert.Len(t, events, 1)
		assert.Equal(t, "500", events[0]["amount"])
		assert.Equal(t, "withdrawal-123", events[0]["withdrawalId"])
	})

	t.Run("GetBurnEvents_WithoutWithdrawalID", func(t *testing.T) {
		store.burnEvents = []*storage.BurnEvent{
			{
				BlockNumber: 20,
				TxHash:      common.HexToHash("0xburntx2"),
				Burner:      common.HexToAddress("0xburner"),
				Amount:      big.NewInt(200),
				Timestamp:   1234567892,
			},
		}

		result, err := server.HandleMethodDirect(ctx, "getBurnEvents", json.RawMessage(`{}`))
		require.Nil(t, err)

		m := result.(map[string]interface{})
		events := m["events"].([]map[string]interface{})
		assert.Len(t, events, 1)
		_, hasWithdrawal := events[0]["withdrawalId"]
		assert.False(t, hasWithdrawal)
	})

	// Test GetTotalSupply with zero value from base mockStorage
	t.Run("GetTotalSupply_ZeroFromBaseMock", func(t *testing.T) {
		basicStore := &mockStorage{
			latestHeight: 100,
			blocks:       make(map[uint64]*types.Block),
			blocksByHash: make(map[common.Hash]*types.Block),
		}
		basicServer := NewServer(basicStore, logger)
		result, err := basicServer.HandleMethodDirect(ctx, "getTotalSupply", json.RawMessage(`{}`))
		require.Nil(t, err)
		require.NotNil(t, result)
		m := result.(map[string]interface{})
		assert.Equal(t, "0", m["totalSupply"])
	})
}

func TestParseProposalStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected storage.ProposalStatus
	}{
		{"none", storage.ProposalStatusNone},
		{"NONE", storage.ProposalStatusNone},
		{"voting", storage.ProposalStatusVoting},
		{"VOTING", storage.ProposalStatusVoting},
		{"approved", storage.ProposalStatusApproved},
		{"APPROVED", storage.ProposalStatusApproved},
		{"executed", storage.ProposalStatusExecuted},
		{"EXECUTED", storage.ProposalStatusExecuted},
		{"cancelled", storage.ProposalStatusCancelled},
		{"CANCELLED", storage.ProposalStatusCancelled},
		{"expired", storage.ProposalStatusExpired},
		{"EXPIRED", storage.ProposalStatusExpired},
		{"failed", storage.ProposalStatusFailed},
		{"FAILED", storage.ProposalStatusFailed},
		{"rejected", storage.ProposalStatusRejected},
		{"REJECTED", storage.ProposalStatusRejected},
		{"unknown", storage.ProposalStatusNone},
		{"", storage.ProposalStatusNone},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseProposalStatus(tt.input))
		})
	}
}

func TestProposalStatusToString(t *testing.T) {
	tests := []struct {
		input    storage.ProposalStatus
		expected string
	}{
		{storage.ProposalStatusNone, "none"},
		{storage.ProposalStatusVoting, "voting"},
		{storage.ProposalStatusApproved, "approved"},
		{storage.ProposalStatusExecuted, "executed"},
		{storage.ProposalStatusCancelled, "cancelled"},
		{storage.ProposalStatusExpired, "expired"},
		{storage.ProposalStatusFailed, "failed"},
		{storage.ProposalStatusRejected, "rejected"},
		{storage.ProposalStatus(99), "none"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, proposalStatusToString(tt.input))
		})
	}
}

func TestSetCodeMethods(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	txHash := common.HexToHash("0xaabbccdd")
	target := common.HexToAddress("0x5555555555555555555555555555555555555555")
	authority := common.HexToAddress("0x6666666666666666666666666666666666666666")

	record := &storage.SetCodeAuthorizationRecord{
		TxHash:           txHash,
		BlockNumber:      100,
		BlockHash:        common.HexToHash("0xblockhash"),
		TxIndex:          0,
		AuthIndex:        0,
		ChainID:          big.NewInt(1),
		TargetAddress:    target,
		Nonce:            5,
		YParity:          0,
		R:                big.NewInt(12345),
		S:                big.NewInt(67890),
		AuthorityAddress: authority,
		Applied:          true,
		Error:            "",
		Timestamp:        time.Now(),
	}

	store := &mockSetCodeStorage{
		mockStorage: &mockStorage{
			latestHeight: 100,
			blocks:       make(map[uint64]*types.Block),
			blocksByHash: make(map[common.Hash]*types.Block),
		},
		authsByTx:        []*storage.SetCodeAuthorizationRecord{record},
		authsByTarget:    []*storage.SetCodeAuthorizationRecord{record},
		authsByAuthority: []*storage.SetCodeAuthorizationRecord{record},
		authsByBlock:     []*storage.SetCodeAuthorizationRecord{record},
		recentAuths:      []*storage.SetCodeAuthorizationRecord{record},
		delegationState: &storage.AddressDelegationState{
			HasDelegation:    true,
			DelegationTarget: &target,
		},
		setCodeStats: &storage.AddressSetCodeStats{
			AsTargetCount:    3,
			AsAuthorityCount: 2,
			LastActivityBlock: 100,
		},
		txCount: 42,
	}

	server := NewServer(store, logger)

	t.Run("GetSetCodeAuthorization_Found", func(t *testing.T) {
		params := json.RawMessage(`{"txHash": "` + txHash.Hex() + `", "authIndex": 0}`)
		result, err := server.HandleMethodDirect(ctx, "getSetCodeAuthorization", params)
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		assert.Equal(t, txHash.Hex(), m["txHash"])
		assert.Equal(t, true, m["applied"])
	})

	t.Run("GetSetCodeAuthorization_NotFound", func(t *testing.T) {
		params := json.RawMessage(`{"txHash": "` + txHash.Hex() + `", "authIndex": 999}`)
		result, err := server.HandleMethodDirect(ctx, "getSetCodeAuthorization", params)
		require.Nil(t, err)
		assert.Nil(t, result)
	})

	t.Run("GetSetCodeAuthorization_MissingTxHash", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getSetCodeAuthorization", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetSetCodeAuthorizationsByTx", func(t *testing.T) {
		params := json.RawMessage(`{"txHash": "` + txHash.Hex() + `"}`)
		result, err := server.HandleMethodDirect(ctx, "getSetCodeAuthorizationsByTx", params)
		require.Nil(t, err)

		arr := result.([]interface{})
		assert.Len(t, arr, 1)
	})

	t.Run("GetSetCodeAuthorizationsByTx_MissingTxHash", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getSetCodeAuthorizationsByTx", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetSetCodeAuthorizationsByTarget", func(t *testing.T) {
		params := json.RawMessage(`{"target": "` + target.Hex() + `"}`)
		result, err := server.HandleMethodDirect(ctx, "getSetCodeAuthorizationsByTarget", params)
		require.Nil(t, err)

		m := result.(map[string]interface{})
		auths := m["authorizations"].([]interface{})
		assert.Len(t, auths, 1)
		assert.Equal(t, 100, m["limit"])
		assert.Equal(t, 0, m["offset"])
	})

	t.Run("GetSetCodeAuthorizationsByTarget_MissingTarget", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getSetCodeAuthorizationsByTarget", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetSetCodeAuthorizationsByTarget_WithPagination", func(t *testing.T) {
		params := json.RawMessage(`{"target": "` + target.Hex() + `", "limit": 50, "offset": 10}`)
		result, err := server.HandleMethodDirect(ctx, "getSetCodeAuthorizationsByTarget", params)
		require.Nil(t, err)

		m := result.(map[string]interface{})
		assert.Equal(t, 50, m["limit"])
		assert.Equal(t, 10, m["offset"])
	})

	t.Run("GetSetCodeAuthorizationsByAuthority", func(t *testing.T) {
		params := json.RawMessage(`{"authority": "` + authority.Hex() + `"}`)
		result, err := server.HandleMethodDirect(ctx, "getSetCodeAuthorizationsByAuthority", params)
		require.Nil(t, err)

		m := result.(map[string]interface{})
		auths := m["authorizations"].([]interface{})
		assert.Len(t, auths, 1)
	})

	t.Run("GetSetCodeAuthorizationsByAuthority_MissingAuthority", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getSetCodeAuthorizationsByAuthority", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetAddressSetCodeInfo", func(t *testing.T) {
		params := json.RawMessage(`{"address": "` + target.Hex() + `"}`)
		result, err := server.HandleMethodDirect(ctx, "getAddressSetCodeInfo", params)
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		assert.Equal(t, true, m["hasDelegation"])
		assert.Equal(t, target.Hex(), m["delegationTarget"])
		assert.Equal(t, 3, m["asTargetCount"])
		assert.Equal(t, 2, m["asAuthorityCount"])
	})

	t.Run("GetAddressSetCodeInfo_MissingAddress", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getAddressSetCodeInfo", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetSetCodeTransactionsInBlock", func(t *testing.T) {
		params := json.RawMessage(`{"blockNumber": 100}`)
		result, err := server.HandleMethodDirect(ctx, "getSetCodeTransactionsInBlock", params)
		require.Nil(t, err)
		// Result is a list of transactions (may be empty since mock GetTransaction returns ErrNotFound)
		require.NotNil(t, result)
	})

	t.Run("GetSetCodeTransactionsInBlock_StringNumber", func(t *testing.T) {
		params := json.RawMessage(`{"blockNumber": "100"}`)
		result, err := server.HandleMethodDirect(ctx, "getSetCodeTransactionsInBlock", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetSetCodeTransactionsInBlock_MissingParam", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getSetCodeTransactionsInBlock", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetSetCodeTransactionsInBlock_InvalidType", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getSetCodeTransactionsInBlock", json.RawMessage(`{"blockNumber": true}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetRecentSetCodeTransactions", func(t *testing.T) {
		result, err := server.HandleMethodDirect(ctx, "getRecentSetCodeTransactions", json.RawMessage(`{}`))
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetRecentSetCodeTransactions_WithLimit", func(t *testing.T) {
		result, err := server.HandleMethodDirect(ctx, "getRecentSetCodeTransactions", json.RawMessage(`{"limit": 5}`))
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetSetCodeTransactionCount", func(t *testing.T) {
		result, err := server.HandleMethodDirect(ctx, "getSetCodeTransactionCount", json.RawMessage(`{}`))
		require.Nil(t, err)

		m := result.(map[string]interface{})
		assert.Equal(t, 42, m["count"])
	})

	// Test storage without SetCode support
	t.Run("SetCode_NotSupported", func(t *testing.T) {
		basicStore := &mockStorage{
			latestHeight: 100,
			blocks:       make(map[uint64]*types.Block),
			blocksByHash: make(map[common.Hash]*types.Block),
		}
		basicServer := NewServer(basicStore, logger)

		methods := []string{
			"getSetCodeAuthorization",
			"getSetCodeAuthorizationsByTx",
			"getSetCodeAuthorizationsByTarget",
			"getSetCodeAuthorizationsByAuthority",
			"getAddressSetCodeInfo",
			"getSetCodeTransactionsInBlock",
			"getRecentSetCodeTransactions",
			"getSetCodeTransactionCount",
		}
		for _, method := range methods {
			params := json.RawMessage(`{"txHash":"0x1","target":"0x1","authority":"0x1","address":"0x1","blockNumber":1}`)
			_, err := basicServer.HandleMethodDirect(ctx, method, params)
			require.NotNil(t, err, "expected error for %s", method)
			assert.Equal(t, InternalError, err.Code, "expected InternalError for %s", method)
		}
	})
}

func TestFilterMethods(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	server := NewServer(store, logger)

	t.Run("EthNewFilter_Basic", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64"}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_newFilter", params)
		require.Nil(t, err)
		require.NotNil(t, result)

		filterID := result.(string)
		assert.NotEmpty(t, filterID)
	})

	t.Run("EthNewFilter_WithAddress", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64", "address": "0x1234567890123456789012345678901234567890"}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_newFilter", params)
		require.Nil(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("EthNewFilter_WithMultipleAddresses", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64", "address": ["0x1111111111111111111111111111111111111111", "0x2222222222222222222222222222222222222222"]}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_newFilter", params)
		require.Nil(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("EthNewFilter_WithTopics", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64", "topics": ["0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", null]}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_newFilter", params)
		require.Nil(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("EthNewFilter_WithTopicArrays", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64", "topics": [["0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925"]]}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_newFilter", params)
		require.Nil(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("EthNewFilter_DefaultBlocks", func(t *testing.T) {
		params := json.RawMessage(`[{}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_newFilter", params)
		require.Nil(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("EthNewFilter_WithDecode", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64", "decode": true}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_newFilter", params)
		require.Nil(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("EthNewFilter_EmptyParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "eth_newFilter", json.RawMessage(`[]`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("EthNewFilter_InvalidAddressType", func(t *testing.T) {
		params := json.RawMessage(`[{"address": 123}]`)
		_, err := server.HandleMethodDirect(ctx, "eth_newFilter", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("EthNewFilter_InvalidTopicType", func(t *testing.T) {
		params := json.RawMessage(`[{"topics": [123]}]`)
		_, err := server.HandleMethodDirect(ctx, "eth_newFilter", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("EthNewBlockFilter", func(t *testing.T) {
		result, err := server.HandleMethodDirect(ctx, "eth_newBlockFilter", json.RawMessage(`{}`))
		require.Nil(t, err)

		filterID := result.(string)
		assert.NotEmpty(t, filterID)
	})

	t.Run("EthNewPendingTransactionFilter", func(t *testing.T) {
		result, err := server.HandleMethodDirect(ctx, "eth_newPendingTransactionFilter", json.RawMessage(`{}`))
		require.Nil(t, err)

		filterID := result.(string)
		assert.NotEmpty(t, filterID)
	})

	t.Run("EthUninstallFilter", func(t *testing.T) {
		// Create filter first
		result, _ := server.HandleMethodDirect(ctx, "eth_newBlockFilter", json.RawMessage(`{}`))
		filterID := result.(string)

		// Uninstall
		params := json.RawMessage(`["` + filterID + `"]`)
		removed, err := server.HandleMethodDirect(ctx, "eth_uninstallFilter", params)
		require.Nil(t, err)
		assert.Equal(t, true, removed)

		// Double uninstall
		removed, err = server.HandleMethodDirect(ctx, "eth_uninstallFilter", params)
		require.Nil(t, err)
		assert.Equal(t, false, removed)
	})

	t.Run("EthUninstallFilter_EmptyParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "eth_uninstallFilter", json.RawMessage(`[]`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("EthGetFilterChanges_BlockFilter", func(t *testing.T) {
		// Create a block filter
		result, _ := server.HandleMethodDirect(ctx, "eth_newBlockFilter", json.RawMessage(`{}`))
		filterID := result.(string)

		params := json.RawMessage(`["` + filterID + `"]`)
		changes, err := server.HandleMethodDirect(ctx, "eth_getFilterChanges", params)
		require.Nil(t, err)
		require.NotNil(t, changes)
	})

	t.Run("EthGetFilterChanges_LogFilter", func(t *testing.T) {
		// Create a log filter
		result, _ := server.HandleMethodDirect(ctx, "eth_newFilter", json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64"}]`))
		filterID := result.(string)

		params := json.RawMessage(`["` + filterID + `"]`)
		changes, err := server.HandleMethodDirect(ctx, "eth_getFilterChanges", params)
		require.Nil(t, err)
		require.NotNil(t, changes)
	})

	t.Run("EthGetFilterChanges_PendingTxFilter", func(t *testing.T) {
		result, _ := server.HandleMethodDirect(ctx, "eth_newPendingTransactionFilter", json.RawMessage(`{}`))
		filterID := result.(string)

		params := json.RawMessage(`["` + filterID + `"]`)
		changes, err := server.HandleMethodDirect(ctx, "eth_getFilterChanges", params)
		require.Nil(t, err)
		require.NotNil(t, changes)
	})

	t.Run("EthGetFilterChanges_NotFound", func(t *testing.T) {
		params := json.RawMessage(`["nonexistent-filter-id"]`)
		_, err := server.HandleMethodDirect(ctx, "eth_getFilterChanges", params)
		require.NotNil(t, err)
		assert.Equal(t, FilterNotFound, err.Code)
	})

	t.Run("EthGetFilterChanges_EmptyParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "eth_getFilterChanges", json.RawMessage(`[]`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("EthGetFilterLogs", func(t *testing.T) {
		// Create a log filter
		result, _ := server.HandleMethodDirect(ctx, "eth_newFilter", json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64"}]`))
		filterID := result.(string)

		params := json.RawMessage(`["` + filterID + `"]`)
		logs, err := server.HandleMethodDirect(ctx, "eth_getFilterLogs", params)
		require.Nil(t, err)
		require.NotNil(t, logs)
	})

	t.Run("EthGetFilterLogs_NotLogFilter", func(t *testing.T) {
		result, _ := server.HandleMethodDirect(ctx, "eth_newBlockFilter", json.RawMessage(`{}`))
		filterID := result.(string)

		params := json.RawMessage(`["` + filterID + `"]`)
		_, err := server.HandleMethodDirect(ctx, "eth_getFilterLogs", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("EthGetFilterLogs_NotFound", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "eth_getFilterLogs", json.RawMessage(`["nonexistent"]`))
		require.NotNil(t, err)
		assert.Equal(t, FilterNotFound, err.Code)
	})

	t.Run("EthGetFilterLogs_EmptyParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "eth_getFilterLogs", json.RawMessage(`[]`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})
}

func TestEthGetLogs(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	header := &types.Header{
		Number:     big.NewInt(1),
		ParentHash: common.HexToHash("0x123"),
		Time:       1000,
		GasLimit:   8000000,
		GasUsed:    5000000,
	}
	block := types.NewBlockWithHeader(header)

	store := &mockStorage{
		latestHeight: 100,
		blocks:       map[uint64]*types.Block{1: block},
		blocksByHash: map[common.Hash]*types.Block{block.Hash(): block},
	}

	server := NewServer(store, logger)

	t.Run("BasicGetLogs", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64"}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_WithBlockHash", func(t *testing.T) {
		params := json.RawMessage(`[{"blockHash": "` + block.Hash().Hex() + `"}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_BlockHashWithRange_Error", func(t *testing.T) {
		params := json.RawMessage(`[{"blockHash": "0x123", "fromBlock": "0x1"}]`)
		_, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetLogs_EmptyParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "eth_getLogs", json.RawMessage(`[]`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetLogs_WithAddress", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64", "address": "0x1234567890123456789012345678901234567890"}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_WithMultipleAddresses", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64", "address": ["0x1111111111111111111111111111111111111111"]}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_WithTopics", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64", "topics": ["0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"]}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_WithDecode", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "0x1", "toBlock": "0x64", "decode": true}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_DefaultBlocks", func(t *testing.T) {
		params := json.RawMessage(`[{}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_InvalidAddressType", func(t *testing.T) {
		params := json.RawMessage(`[{"address": 123}]`)
		_, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetLogs_InvalidTopicType", func(t *testing.T) {
		params := json.RawMessage(`[{"topics": [123]}]`)
		_, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetLogs_LatestBlock", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "latest", "toBlock": "latest"}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_EarliestBlock", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "earliest", "toBlock": "0x64"}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_PendingBlock", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "pending", "toBlock": "pending"}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_DecimalBlock", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "10", "toBlock": "20"}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_NumericBlock", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": 10, "toBlock": 20}]`)
		result, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetLogs_InvalidBlockFormat", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": "invalid-block"}]`)
		_, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetLogs_InvalidBlockType", func(t *testing.T) {
		params := json.RawMessage(`[{"fromBlock": true}]`)
		_, err := server.HandleMethodDirect(ctx, "eth_getLogs", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})
}

func TestABIMethods(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	server := NewServer(store, logger)

	t.Run("ListContractABIs", func(t *testing.T) {
		result, err := server.HandleMethodDirect(ctx, "listContractABIs", json.RawMessage(`{}`))
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		contracts := m["contracts"].([]string)
		assert.Empty(t, contracts)
		assert.Equal(t, 0, m["count"])
	})

	t.Run("SetContractABI_MissingParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "setContractABI", json.RawMessage(`[]`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("SetContractABI_MissingAddress", func(t *testing.T) {
		params := json.RawMessage(`[{"abi": "[]"}]`)
		_, err := server.HandleMethodDirect(ctx, "setContractABI", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("SetContractABI_MissingABI", func(t *testing.T) {
		params := json.RawMessage(`[{"address": "0x1234567890123456789012345678901234567890"}]`)
		_, err := server.HandleMethodDirect(ctx, "setContractABI", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("SetContractABI_InvalidABI", func(t *testing.T) {
		params := json.RawMessage(`[{"address": "0x1234567890123456789012345678901234567890", "abi": "not-valid-json"}]`)
		_, err := server.HandleMethodDirect(ctx, "setContractABI", params)
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("SetContractABI_ValidABI", func(t *testing.T) {
		abi := `[{"type":"event","name":"Transfer","inputs":[{"name":"from","type":"address","indexed":true},{"name":"to","type":"address","indexed":true},{"name":"value","type":"uint256","indexed":false}]}]`
		paramObj := []map[string]interface{}{
			{
				"address": "0x1234567890123456789012345678901234567890",
				"name":    "ERC20",
				"abi":     abi,
			},
		}
		paramBytes, _ := json.Marshal(paramObj)
		result, err := server.HandleMethodDirect(ctx, "setContractABI", json.RawMessage(paramBytes))
		require.Nil(t, err)
		require.NotNil(t, result)

		m := result.(map[string]interface{})
		assert.Equal(t, true, m["success"])
	})

	t.Run("GetContractABI_MissingParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getContractABI", json.RawMessage(`[]`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("DeleteContractABI_MissingParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "deleteContractABI", json.RawMessage(`[]`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("DeleteContractABI_Success", func(t *testing.T) {
		paramObj := []map[string]interface{}{
			{"address": "0x1234567890123456789012345678901234567890"},
		}
		paramBytes, _ := json.Marshal(paramObj)
		result, err := server.HandleMethodDirect(ctx, "deleteContractABI", json.RawMessage(paramBytes))
		require.Nil(t, err)

		m := result.(map[string]interface{})
		assert.Equal(t, true, m["success"])
	})

	t.Run("DecodeLog_MissingParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "decodeLog", json.RawMessage(`[]`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})
}

func TestNotificationMethods(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	server := NewServer(store, logger)

	// Without notification service, all methods should return InternalError
	notificationMethods := []struct {
		method string
		params string
	}{
		{"notification_getSettings", `{}`},
		{"notification_getSetting", `{"id": "test-id"}`},
		{"notification_createSetting", `{"type": "email", "config": {}}`},
		{"notification_updateSetting", `{"id": "test-id"}`},
		{"notification_deleteSetting", `{"id": "test-id"}`},
		{"notification_list", `{}`},
		{"notification_get", `{"id": "test-id"}`},
		{"notification_getStats", `{}`},
		{"notification_getHistory", `{}`},
		{"notification_test", `{"id": "test-id"}`},
		{"notification_retry", `{"id": "test-id"}`},
		{"notification_cancel", `{"id": "test-id"}`},
	}

	for _, tc := range notificationMethods {
		t.Run(tc.method+"_NoService", func(t *testing.T) {
			_, err := server.HandleMethodDirect(ctx, tc.method, json.RawMessage(tc.params))
			require.NotNil(t, err, "expected error for %s without service", tc.method)
			assert.Equal(t, InternalError, err.Code)
		})
	}
}

func TestWBFTExtendedMethods(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	store := &mockWBFTStorage{}
	server := NewServer(store, logger)

	t.Run("GetAllValidatorsSigningStats", func(t *testing.T) {
		params := json.RawMessage(`{"fromBlock": 1, "toBlock": 200}`)
		result, err := server.HandleMethodDirect(ctx, "getAllValidatorsSigningStats", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetAllValidatorsSigningStats_MissingParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getAllValidatorsSigningStats", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetValidatorSigningActivity", func(t *testing.T) {
		params := json.RawMessage(`{"validatorAddress": "0x1111111111111111111111111111111111111111", "fromBlock": 1, "toBlock": 200}`)
		result, err := server.HandleMethodDirect(ctx, "getValidatorSigningActivity", params)
		require.Nil(t, err)
		require.NotNil(t, result)
	})

	t.Run("GetValidatorSigningActivity_MissingParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getValidatorSigningActivity", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

	t.Run("GetBlockSigners_MissingParams", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "getBlockSigners", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, InvalidParams, err.Code)
	})

}

func TestServerEdgeCases(t *testing.T) {
	logger := zap.NewNop()

	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	server := NewServer(store, logger)

	t.Run("EmptyBatchRequest", func(t *testing.T) {
		reqBody := `[]`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var resp Response
		json.NewDecoder(w.Body).Decode(&resp)
		require.NotNil(t, resp.Error)
		assert.Equal(t, InvalidRequest, resp.Error.Code)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		reqBody := `{invalid json`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var resp Response
		json.NewDecoder(w.Body).Decode(&resp)
		require.NotNil(t, resp.Error)
		assert.Equal(t, ParseError, resp.Error.Code)
	})

	t.Run("MissingMethod", func(t *testing.T) {
		reqBody := `{"jsonrpc":"2.0","params":{},"id":1}`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var resp Response
		json.NewDecoder(w.Body).Decode(&resp)
		require.NotNil(t, resp.Error)
		assert.Equal(t, InvalidRequest, resp.Error.Code)
	})

	t.Run("BatchWithMixedValidity", func(t *testing.T) {
		reqBody := `[
			{"jsonrpc":"2.0","method":"getLatestHeight","params":{},"id":1},
			{"jsonrpc":"1.0","method":"getLatestHeight","params":{},"id":2},
			{"jsonrpc":"2.0","method":"","params":{},"id":3}
		]`
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var batch BatchResponse
		json.NewDecoder(w.Body).Decode(&batch)
		assert.Len(t, batch, 3)
		assert.Nil(t, batch[0].Error)
		assert.NotNil(t, batch[1].Error)
		assert.NotNil(t, batch[2].Error)
	})

	t.Run("LargeBatchRequest", func(t *testing.T) {
		// Build a batch of 101 requests (exceeds limit of 100)
		batch := make([]map[string]interface{}, 101)
		for i := range batch {
			batch[i] = map[string]interface{}{
				"jsonrpc": "2.0",
				"method":  "getLatestHeight",
				"params":  map[string]interface{}{},
				"id":      i + 1,
			}
		}
		body, _ := json.Marshal(batch)
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(body))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		var resp Response
		json.NewDecoder(w.Body).Decode(&resp)
		require.NotNil(t, resp.Error)
		assert.Equal(t, InvalidRequest, resp.Error.Code)
	})
}

func TestMethodNotFound(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	server := NewServer(store, logger)

	// Test that calling a non-existent method returns proper error
	t.Run("UnknownMethod", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "nonExistentMethod", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, MethodNotFound, err.Code)
	})

	t.Run("EmptyMethod", func(t *testing.T) {
		_, err := server.HandleMethodDirect(ctx, "", json.RawMessage(`{}`))
		require.NotNil(t, err)
		assert.Equal(t, MethodNotFound, err.Code)
	})
}

func TestFilterManager(t *testing.T) {
	t.Run("NewFilter_Types", func(t *testing.T) {
		fm := NewFilterManager(context.Background(), 5*time.Minute)
		defer fm.Close()

		logID := fm.NewFilter(LogFilterType, &storage.LogFilter{FromBlock: 0, ToBlock: 100}, 0, false)
		assert.NotEmpty(t, logID)

		blockID := fm.NewFilter(BlockFilterType, nil, 50, false)
		assert.NotEmpty(t, blockID)

		pendingID := fm.NewFilter(PendingTxFilterType, nil, 50, false)
		assert.NotEmpty(t, pendingID)

		// All IDs should be different
		assert.NotEqual(t, logID, blockID)
		assert.NotEqual(t, blockID, pendingID)
	})

	t.Run("GetFilter", func(t *testing.T) {
		fm := NewFilterManager(context.Background(), 5*time.Minute)
		defer fm.Close()

		filterID := fm.NewFilter(LogFilterType, &storage.LogFilter{FromBlock: 10, ToBlock: 20}, 10, true)

		filter, exists := fm.GetFilter(filterID)
		assert.True(t, exists)
		assert.Equal(t, LogFilterType, filter.Type)
		assert.Equal(t, true, filter.Decode)

		_, exists = fm.GetFilter("nonexistent")
		assert.False(t, exists)
	})

	t.Run("RemoveFilter", func(t *testing.T) {
		fm := NewFilterManager(context.Background(), 5*time.Minute)
		defer fm.Close()

		filterID := fm.NewFilter(BlockFilterType, nil, 0, false)

		removed := fm.RemoveFilter(filterID)
		assert.True(t, removed)

		removed = fm.RemoveFilter(filterID)
		assert.False(t, removed)
	})

	t.Run("UpdateLastPollBlock", func(t *testing.T) {
		fm := NewFilterManager(context.Background(), 5*time.Minute)
		defer fm.Close()

		filterID := fm.NewFilter(BlockFilterType, nil, 50, false)

		fm.UpdateLastPollBlock(filterID, 100)

		filter, exists := fm.GetFilter(filterID)
		require.True(t, exists)
		assert.Equal(t, uint64(100), filter.LastPollBlock)
	})

	t.Run("FilterClose", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		fm := NewFilterManager(ctx, 5*time.Minute)

		filterID := fm.NewFilter(BlockFilterType, nil, 0, false)
		_, exists := fm.GetFilter(filterID)
		assert.True(t, exists)

		// Cancel and close should not panic
		cancel()
		fm.Close()
	})
}
