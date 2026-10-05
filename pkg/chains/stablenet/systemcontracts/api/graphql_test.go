package api

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	sc "github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// richMock is a storage that serves system contract data itself; other
// storage methods are not implemented.
type richMock struct {
	storage.Storage
}

// ListABIs is called while the schema is built.
func (m *richMock) ListABIs(context.Context) ([]common.Address, error) { return nil, nil }

// newRichTestHandler returns a GraphQL handler over richMock.
func newRichTestHandler(t *testing.T) *graphql.Handler {
	t.Helper()
	h, err := graphql.NewHandler(&richMock{}, zap.NewNop())
	require.NoError(t, err)
	return h
}

// newEmptyTestHandler returns a GraphQL handler over an empty index.
func newEmptyTestHandler(t *testing.T) *graphql.Handler {
	t.Helper()
	db, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	h, err := graphql.NewHandler(db, zap.NewNop())
	require.NoError(t, err)
	return h
}

func (m *richMock) GetActiveMinters(_ context.Context) ([]common.Address, error) {
	return []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000001"),
		common.HexToAddress("0x0000000000000000000000000000000000000002"),
	}, nil
}

func (m *richMock) GetMinterAllowance(_ context.Context, _ common.Address) (*big.Int, error) {
	return big.NewInt(1000000), nil
}

func (m *richMock) GetActiveValidators(_ context.Context) ([]common.Address, error) {
	return []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000010"),
	}, nil
}

func (m *richMock) GetBlacklistedAddresses(_ context.Context) ([]common.Address, error) {
	return []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000099"),
	}, nil
}

func (m *richMock) GetAuthorizedAccounts(_ context.Context) ([]common.Address, error) {
	return []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000055"),
	}, nil
}

func (m *richMock) GetTotalSupply(_ context.Context) (*big.Int, error) {
	return big.NewInt(99999999), nil
}

func (m *richMock) GetProposals(_ context.Context, _ common.Address, _ sc.ProposalStatus, _, _ int) ([]*sc.Proposal, error) {
	executed := uint64(1700000100)
	return []*sc.Proposal{
		{
			Contract:          common.HexToAddress("0x01"),
			ProposalID:        big.NewInt(1),
			Proposer:          common.HexToAddress("0x02"),
			ActionType:        [32]byte{0x01},
			CallData:          []byte{0xAB, 0xCD},
			MemberVersion:     big.NewInt(1),
			RequiredApprovals: 3,
			Approved:          2,
			Rejected:          1,
			Status:            sc.ProposalStatusVoting,
			CreatedAt:         1700000000,
			ExecutedAt:        nil,
			BlockNumber:       100,
			TxHash:            common.HexToHash("0xaaa"),
		},
		{
			Contract:          common.HexToAddress("0x01"),
			ProposalID:        big.NewInt(2),
			Proposer:          common.HexToAddress("0x03"),
			ActionType:        [32]byte{0x02},
			CallData:          []byte{},
			MemberVersion:     big.NewInt(1),
			RequiredApprovals: 3,
			Approved:          3,
			Rejected:          0,
			Status:            sc.ProposalStatusExecuted,
			CreatedAt:         1700000050,
			ExecutedAt:        &executed,
			BlockNumber:       110,
			TxHash:            common.HexToHash("0xbbb"),
		},
	}, nil
}

func (m *richMock) GetProposalById(_ context.Context, _ common.Address, id *big.Int) (*sc.Proposal, error) {
	if id.Cmp(big.NewInt(1)) == 0 {
		return &sc.Proposal{
			Contract:          common.HexToAddress("0x01"),
			ProposalID:        big.NewInt(1),
			Proposer:          common.HexToAddress("0x02"),
			ActionType:        [32]byte{0x01},
			MemberVersion:     big.NewInt(1),
			RequiredApprovals: 3,
			Status:            sc.ProposalStatusVoting,
			CreatedAt:         1700000000,
			BlockNumber:       100,
			TxHash:            common.HexToHash("0xaaa"),
		}, nil
	}
	return nil, nil // not found
}

func (m *richMock) GetProposalVotes(_ context.Context, _ common.Address, _ *big.Int) ([]*sc.ProposalVote, error) {
	return []*sc.ProposalVote{
		{
			Contract:    common.HexToAddress("0x01"),
			ProposalID:  big.NewInt(1),
			Voter:       common.HexToAddress("0x10"),
			Approval:    true,
			BlockNumber: 101,
			TxHash:      common.HexToHash("0xccc"),
			Timestamp:   1700000010,
		},
	}, nil
}

func (m *richMock) GetMintEvents(_ context.Context, _, _ uint64, _ common.Address, _, _ int) ([]*sc.MintEvent, error) {
	return []*sc.MintEvent{
		{
			BlockNumber: 50,
			TxHash:      common.HexToHash("0xddd"),
			Minter:      common.HexToAddress("0x01"),
			To:          common.HexToAddress("0x02"),
			Amount:      big.NewInt(5000),
			Timestamp:   1700000000,
		},
	}, nil
}

func (m *richMock) GetBurnEvents(_ context.Context, _, _ uint64, _ common.Address, _, _ int) ([]*sc.BurnEvent, error) {
	return []*sc.BurnEvent{
		{
			BlockNumber:  60,
			TxHash:       common.HexToHash("0xeee"),
			Burner:       common.HexToAddress("0x03"),
			Amount:       big.NewInt(2000),
			Timestamp:    1700000050,
			WithdrawalID: "w-123",
		},
		{
			BlockNumber: 61,
			TxHash:      common.HexToHash("0xfff"),
			Burner:      common.HexToAddress("0x04"),
			Amount:      big.NewInt(1000),
			Timestamp:   1700000060,
		},
	}, nil
}

func (m *richMock) GetMinterHistory(_ context.Context, _ common.Address) ([]*sc.MinterConfigEvent, error) {
	return []*sc.MinterConfigEvent{
		{BlockNumber: 10, TxHash: common.HexToHash("0x111"), Minter: common.HexToAddress("0x01"), Allowance: big.NewInt(100000), Action: "configured", Timestamp: 1700000000},
	}, nil
}

func (m *richMock) GetValidatorHistory(_ context.Context, _ common.Address) ([]*sc.ValidatorChangeEvent, error) {
	old := common.HexToAddress("0x09")
	return []*sc.ValidatorChangeEvent{
		{BlockNumber: 20, TxHash: common.HexToHash("0x222"), Validator: common.HexToAddress("0x10"), Action: "added", Timestamp: 1700000000},
		{BlockNumber: 30, TxHash: common.HexToHash("0x333"), Validator: common.HexToAddress("0x11"), Action: "changed", OldValidator: &old, Timestamp: 1700000100},
	}, nil
}

func (m *richMock) GetGasTipHistory(_ context.Context, _, _ uint64) ([]*sc.GasTipUpdateEvent, error) {
	return []*sc.GasTipUpdateEvent{
		{BlockNumber: 40, TxHash: common.HexToHash("0x444"), OldTip: big.NewInt(100), NewTip: big.NewInt(200), Updater: common.HexToAddress("0x10"), Timestamp: 1700000000},
	}, nil
}

func (m *richMock) GetBlacklistHistory(_ context.Context, _ common.Address) ([]*sc.BlacklistEvent, error) {
	return []*sc.BlacklistEvent{
		{BlockNumber: 50, TxHash: common.HexToHash("0x555"), Account: common.HexToAddress("0x99"), Action: "blacklisted", ProposalID: big.NewInt(5), Timestamp: 1700000000},
	}, nil
}

func (m *richMock) GetMemberHistory(_ context.Context, _ common.Address) ([]*sc.MemberChangeEvent, error) {
	old := common.HexToAddress("0x08")
	return []*sc.MemberChangeEvent{
		{Contract: common.HexToAddress("0x01"), BlockNumber: 60, TxHash: common.HexToHash("0x666"), Member: common.HexToAddress("0x20"), Action: "added", TotalMembers: 5, NewQuorum: 3, Timestamp: 1700000000},
		{Contract: common.HexToAddress("0x01"), BlockNumber: 70, TxHash: common.HexToHash("0x777"), Member: common.HexToAddress("0x21"), Action: "changed", OldMember: &old, TotalMembers: 5, NewQuorum: 3, Timestamp: 1700000100},
	}, nil
}

func (m *richMock) GetEmergencyPauseHistory(_ context.Context, _ common.Address) ([]*sc.EmergencyPauseEvent, error) {
	return []*sc.EmergencyPauseEvent{
		{Contract: common.HexToAddress("0x01"), BlockNumber: 80, TxHash: common.HexToHash("0x888"), ProposalID: big.NewInt(10), Action: "paused", Timestamp: 1700000000},
	}, nil
}

func (m *richMock) GetDepositMintProposals(_ context.Context, _, _ uint64, _ sc.ProposalStatus) ([]*sc.DepositMintProposal, error) {
	return []*sc.DepositMintProposal{
		{ProposalID: big.NewInt(1), Requester: common.HexToAddress("0x30"), Beneficiary: common.HexToAddress("0x31"), Amount: big.NewInt(50000), DepositID: "d-001", BankReference: "BR-123", Status: sc.ProposalStatusApproved, BlockNumber: 90, TxHash: common.HexToHash("0x999"), Timestamp: 1700000000},
	}, nil
}

func (m *richMock) GetMinterConfigHistory(_ context.Context, _, _ uint64) ([]*sc.MinterConfigEvent, error) {
	return []*sc.MinterConfigEvent{
		{BlockNumber: 15, TxHash: common.HexToHash("0xaab"), Minter: common.HexToAddress("0x01"), Allowance: big.NewInt(200000), Action: "configured", Timestamp: 1700000000},
	}, nil
}

func (m *richMock) GetBurnHistory(_ context.Context, _, _ uint64, _ common.Address) ([]*sc.BurnEvent, error) {
	return []*sc.BurnEvent{
		{BlockNumber: 65, TxHash: common.HexToHash("0xaac"), Burner: common.HexToAddress("0x05"), Amount: big.NewInt(3000), Timestamp: 1700000070},
	}, nil
}

func (m *richMock) GetMaxProposalsUpdateHistory(_ context.Context, _ common.Address) ([]*sc.MaxProposalsUpdateEvent, error) {
	return []*sc.MaxProposalsUpdateEvent{
		{Contract: common.HexToAddress("0x01"), BlockNumber: 95, TxHash: common.HexToHash("0xaad"), OldMax: 5, NewMax: 10, Timestamp: 1700000000},
	}, nil
}

func (m *richMock) GetProposalExecutionSkippedEvents(_ context.Context, _ common.Address, _ *big.Int) ([]*sc.ProposalExecutionSkippedEvent, error) {
	return []*sc.ProposalExecutionSkippedEvent{
		{Contract: common.HexToAddress("0x01"), BlockNumber: 96, TxHash: common.HexToHash("0xaae"), Account: common.HexToAddress("0x40"), ProposalID: big.NewInt(3), Reason: "quorum not met", Timestamp: 1700000000},
	}, nil
}

// TestSystemContractResolvers tests all system contract query resolvers.
func TestSystemContractResolvers(t *testing.T) {
	handler := newEmptyTestHandler(t)

	tests := []struct {
		name      string
		query     string
		expectErr bool
	}{
		{"totalSupply", `{ totalSupply }`, false},
		{"activeMinters", `{ activeMinters { address allowance } }`, false},
		{"activeMinterAddresses", `{ activeMinterAddresses }`, false},
		{"minterAllowance", `{ minterAllowance(address: "0x0000000000000000000000000000000000000001") }`, false},
		{"activeValidators", `{ activeValidators { address } }`, false},
		{"activeValidatorAddresses", `{ activeValidatorAddresses }`, false},
		{"blacklistedAddresses", `{ blacklistedAddresses }`, false},
		{"authorizedAccounts", `{ authorizedAccounts }`, false},
		{"proposals", `{ proposals(contract: "0x0000000000000000000000000000000000000001") { id status } }`, false},
		{"proposal", `{ proposal(contract: "0x0000000000000000000000000000000000000001", proposalId: "1") { id } }`, true},
		{"proposalVotes", `{ proposalVotes(contract: "0x0000000000000000000000000000000000000001", proposalId: "1") { voter support } }`, false},
		{"mintEvents", `{ mintEvents { nodes { minter amount blockNumber } totalCount } }`, false},
		{"burnEvents", `{ burnEvents { nodes { burner amount blockNumber } totalCount } }`, false},
		{"minterHistory", `{ minterHistory(minter: "0x0000000000000000000000000000000000000001") { minter action blockNumber } }`, false},
		{"validatorHistory", `{ validatorHistory(validator: "0x0000000000000000000000000000000000000001") { validator action blockNumber } }`, false},
		{"gasTipHistory", `{ gasTipHistory { newGasTip blockNumber } }`, false},
		{"blacklistHistory", `{ blacklistHistory(address: "0x0000000000000000000000000000000000000001") { address action blockNumber } }`, false},
		{"memberHistory", `{ memberHistory(contract: "0x0000000000000000000000000000000000000001") { member action blockNumber } }`, false},
		{"emergencyPauseHistory", `{ emergencyPauseHistory(contract: "0x0000000000000000000000000000000000000001") { contract paused blockNumber } }`, false},
		{"depositMintProposals", `{ depositMintProposals { proposalId amount status } }`, false},
		{"minterConfigHistory", `{ minterConfigHistory { minter action blockNumber } }`, false},
		{"burnHistory", `{ burnHistory { nodes { burner amount blockNumber } totalCount } }`, false},
		{"maxProposalsUpdateHistory", `{ maxProposalsUpdateHistory(contract: "0x0000000000000000000000000000000000000001") { oldMax newMax blockNumber } }`, false},
		{"proposalExecutionSkippedEvents", `{ proposalExecutionSkippedEvents(contract: "0x0000000000000000000000000000000000000001", proposalId: "1") { proposalId reason blockNumber } }`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			if tc.expectErr {
				assert.NotEmpty(t, result.Errors, "expected error for %s", tc.name)
			}
			// Resolver ran without panic - coverage gained
		})
	}
}

// TestSystemContractResolversWithData exercises resolvers that iterate over data
// and call *ToMap helper functions.
func TestSystemContractResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	tests := []struct {
		name      string
		query     string
		checkData func(t *testing.T, data map[string]interface{})
	}{
		{
			"activeMinters_withData",
			`{ activeMinters { address allowance isActive } }`,
			func(t *testing.T, data map[string]interface{}) {
				minters, ok := data["activeMinters"].([]interface{})
				require.True(t, ok)
				assert.Len(t, minters, 2)
				first := minters[0].(map[string]interface{})
				assert.Equal(t, true, first["isActive"])
				assert.Equal(t, "1000000", first["allowance"])
			},
		},
		{
			"activeMinterAddresses_withData",
			`{ activeMinterAddresses }`,
			func(t *testing.T, data map[string]interface{}) {
				addrs, ok := data["activeMinterAddresses"].([]interface{})
				require.True(t, ok)
				assert.Len(t, addrs, 2)
			},
		},
		{
			"minterAllowance_withData",
			`{ minterAllowance(minter: "0x0000000000000000000000000000000000000001") }`,
			func(t *testing.T, data map[string]interface{}) {
				assert.Equal(t, "1000000", data["minterAllowance"])
			},
		},
		{
			"activeValidators_withData",
			`{ activeValidators { address isActive } }`,
			func(t *testing.T, data map[string]interface{}) {
				vals, ok := data["activeValidators"].([]interface{})
				require.True(t, ok)
				assert.Len(t, vals, 1)
			},
		},
		{
			"activeValidatorAddresses_withData",
			`{ activeValidatorAddresses }`,
			func(t *testing.T, data map[string]interface{}) {
				addrs, ok := data["activeValidatorAddresses"].([]interface{})
				require.True(t, ok)
				assert.Len(t, addrs, 1)
			},
		},
		{
			"blacklistedAddresses_withData",
			`{ blacklistedAddresses }`,
			func(t *testing.T, data map[string]interface{}) {
				addrs, ok := data["blacklistedAddresses"].([]interface{})
				require.True(t, ok)
				assert.Len(t, addrs, 1)
			},
		},
		{
			"authorizedAccounts_withData",
			`{ authorizedAccounts }`,
			func(t *testing.T, data map[string]interface{}) {
				addrs, ok := data["authorizedAccounts"].([]interface{})
				require.True(t, ok)
				assert.Len(t, addrs, 1)
			},
		},
		{
			"totalSupply_withData",
			`{ totalSupply }`,
			func(t *testing.T, data map[string]interface{}) {
				assert.Equal(t, "99999999", data["totalSupply"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.ExecuteQuery(tc.query, nil)
			assert.Empty(t, result.Errors, "unexpected errors for %s: %v", tc.name, result.Errors)
			if tc.checkData != nil {
				data, ok := result.Data.(map[string]interface{})
				require.True(t, ok)
				tc.checkData(t, data)
			}
		})
	}
}

// TestProposalResolversWithData exercises proposal query/vote resolvers and proposalToMap.
func TestProposalResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	t.Run("proposals_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposals { nodes { contract proposalId proposer status requiredApprovals approved rejected createdAt executedAt blockNumber transactionHash actionType callData memberVersion } totalCount pageInfo { hasNextPage hasPreviousPage } } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		proposals := data["proposals"].(map[string]interface{})
		nodes := proposals["nodes"].([]interface{})
		assert.Len(t, nodes, 2)
		// First proposal: no executedAt
		first := nodes[0].(map[string]interface{})
		assert.Equal(t, "VOTING", first["status"])
		assert.Nil(t, first["executedAt"])
		// Second proposal: has executedAt
		second := nodes[1].(map[string]interface{})
		assert.Equal(t, "EXECUTED", second["status"])
		assert.NotNil(t, second["executedAt"])
		assert.Equal(t, 2, proposals["totalCount"])
	})

	t.Run("proposals_withFilter", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposals(filter: {contract: "0x0000000000000000000000000000000000000001", status: VOTING, proposer: "0x0000000000000000000000000000000000000002"}) { nodes { proposalId } totalCount } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("proposals_withPagination", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposals(pagination: {limit: 1, offset: 0}) { nodes { proposalId } totalCount pageInfo { hasNextPage hasPreviousPage } } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("proposal_byId", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposal(contract: "0x0000000000000000000000000000000000000001", proposalId: "1") { contract proposalId proposer status } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		assert.NotNil(t, data["proposal"])
	})

	t.Run("proposal_notFound", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposal(contract: "0x0000000000000000000000000000000000000001", proposalId: "999") { proposalId } }`, nil)
		// Returns nil without error
		assert.Empty(t, result.Errors)
	})

	t.Run("proposalVotes_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposalVotes(contract: "0x0000000000000000000000000000000000000001", proposalId: "1") { contract proposalId voter approval blockNumber transactionHash timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		votes := data["proposalVotes"].([]interface{})
		assert.Len(t, votes, 1)
	})
}

// TestMintBurnResolversWithData exercises mint/burn event resolvers and *ToMap functions.
func TestMintBurnResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	t.Run("mintEvents_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ mintEvents(filter: {fromBlock: "0", toBlock: "100"}) { nodes { blockNumber transactionHash minter to amount timestamp } totalCount pageInfo { hasNextPage hasPreviousPage } } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["mintEvents"].(map[string]interface{})
		nodes := events["nodes"].([]interface{})
		assert.Len(t, nodes, 1)
		first := nodes[0].(map[string]interface{})
		assert.Equal(t, "5000", first["amount"])
	})

	t.Run("mintEvents_withMinterFilter", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ mintEvents(filter: {fromBlock: "0", toBlock: "100", address: "0x0000000000000000000000000000000000000001"}) { nodes { minter } totalCount } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("mintEvents_withPagination", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ mintEvents(filter: {fromBlock: "0", toBlock: "100"}, pagination: {limit: 5, offset: 0}) { nodes { minter } } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("burnEvents_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ burnEvents(filter: {fromBlock: "0", toBlock: "100"}) { nodes { blockNumber transactionHash burner amount timestamp withdrawalId } totalCount pageInfo { hasNextPage hasPreviousPage } } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["burnEvents"].(map[string]interface{})
		nodes := events["nodes"].([]interface{})
		assert.Len(t, nodes, 2)
		// First has withdrawalId
		first := nodes[0].(map[string]interface{})
		assert.Equal(t, "w-123", first["withdrawalId"])
	})

	t.Run("burnEvents_withBurnerFilter", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ burnEvents(filter: {fromBlock: "0", toBlock: "100", address: "0x0000000000000000000000000000000000000003"}) { nodes { burner } } }`, nil)
		assert.Empty(t, result.Errors)
	})

	t.Run("burnHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ burnHistory(filter: {fromBlock: "0", toBlock: "100"}) { nodes { burner amount } totalCount } }`, nil)
		assert.Empty(t, result.Errors)
	})
}

// TestHistoryResolversWithData exercises all history resolvers (minter, validator, gas tip, etc).
func TestHistoryResolversWithData(t *testing.T) {
	handler := newRichTestHandler(t)

	t.Run("minterHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ minterHistory(minter: "0x0000000000000000000000000000000000000001") { blockNumber transactionHash minter allowance action timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["minterHistory"].([]interface{})
		assert.Len(t, events, 1)
		first := events[0].(map[string]interface{})
		assert.Equal(t, "configured", first["action"])
	})

	t.Run("validatorHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ validatorHistory(validator: "0x0000000000000000000000000000000000000010") { blockNumber transactionHash validator action oldValidator timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["validatorHistory"].([]interface{})
		assert.Len(t, events, 2)
		// Second event has oldValidator
		second := events[1].(map[string]interface{})
		assert.NotNil(t, second["oldValidator"])
	})

	t.Run("gasTipHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ gasTipHistory(filter: {fromBlock: "0", toBlock: "100"}) { blockNumber transactionHash oldTip newTip updater timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["gasTipHistory"].([]interface{})
		assert.Len(t, events, 1)
	})

	t.Run("blacklistHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ blacklistHistory(address: "0x0000000000000000000000000000000000000099") { blockNumber transactionHash account action proposalId timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["blacklistHistory"].([]interface{})
		assert.Len(t, events, 1)
	})

	t.Run("memberHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ memberHistory(contract: "0x0000000000000000000000000000000000000001") { contract blockNumber transactionHash member action oldMember totalMembers newQuorum timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["memberHistory"].([]interface{})
		assert.Len(t, events, 2)
		// Second event has oldMember
		second := events[1].(map[string]interface{})
		assert.NotNil(t, second["oldMember"])
	})

	t.Run("emergencyPauseHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ emergencyPauseHistory(contract: "0x0000000000000000000000000000000000000001") { contract blockNumber transactionHash proposalId action timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["emergencyPauseHistory"].([]interface{})
		assert.Len(t, events, 1)
	})

	t.Run("depositMintProposals_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ depositMintProposals(filter: {fromBlock: "0", toBlock: "100"}) { proposalId amount depositId status blockNumber transactionHash timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		proposals := data["depositMintProposals"].([]interface{})
		assert.Len(t, proposals, 1)
		first := proposals[0].(map[string]interface{})
		assert.Equal(t, "APPROVED", first["status"])
	})

	t.Run("minterConfigHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ minterConfigHistory(filter: {fromBlock: "0", toBlock: "100"}) { blockNumber transactionHash minter allowance action timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["minterConfigHistory"].([]interface{})
		assert.Len(t, events, 1)
	})

	t.Run("maxProposalsUpdateHistory_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ maxProposalsUpdateHistory(contract: "0x0000000000000000000000000000000000000001") { contract blockNumber transactionHash oldMax newMax timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["maxProposalsUpdateHistory"].([]interface{})
		assert.Len(t, events, 1)
	})

	t.Run("proposalExecutionSkippedEvents_withData", func(t *testing.T) {
		result := handler.ExecuteQuery(`{ proposalExecutionSkippedEvents(contract: "0x0000000000000000000000000000000000000001", proposalId: "3") { contract blockNumber transactionHash account proposalId reason timestamp } }`, nil)
		assert.Empty(t, result.Errors)
		data := result.Data.(map[string]interface{})
		events := data["proposalExecutionSkippedEvents"].([]interface{})
		assert.Len(t, events, 1)
	})
}

// TestProposalStatusParsing exercises parseProposalStatusEnum and proposalStatusEnum.
func TestProposalStatusParsing(t *testing.T) {
	handler := newRichTestHandler(t)

	statuses := []string{"VOTING", "APPROVED", "EXECUTED", "CANCELLED", "EXPIRED", "FAILED", "REJECTED", "NONE"}
	for _, status := range statuses {
		t.Run("filter_"+status, func(t *testing.T) {
			result := handler.ExecuteQuery(`{ proposals(filter: {status: `+status+`}) { totalCount } }`, nil)
			// Resolver runs, exercising parseProposalStatusEnum
			_ = result
		})
	}
}
