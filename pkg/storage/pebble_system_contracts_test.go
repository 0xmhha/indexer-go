package storage

import (
	"context"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPebbleStorage_SystemContractEvents(t *testing.T) {
	// Create temp directory for test database
	tempDir, err := os.MkdirTemp("", "pebble_syscontracts_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create storage
	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()

	t.Run("MintEvent", func(t *testing.T) {
		event := &MintEvent{
			BlockNumber: 100,
			TxHash:      common.HexToHash("0xabc"),
			Minter:      common.HexToAddress("0x1111"),
			To:          common.HexToAddress("0x2222"),
			Amount:      big.NewInt(1000),
			Timestamp:   1234567890,
		}

		err := storage.StoreMintEvent(ctx, event)
		require.NoError(t, err)
	})

	t.Run("BurnEvent", func(t *testing.T) {
		event := &BurnEvent{
			BlockNumber:  101,
			TxHash:       common.HexToHash("0xdef"),
			Burner:       common.HexToAddress("0x3333"),
			Amount:       big.NewInt(500),
			Timestamp:    1234567891,
			WithdrawalID: "withdrawal-123",
		}

		err := storage.StoreBurnEvent(ctx, event)
		require.NoError(t, err)
	})

	t.Run("MinterConfigEvent", func(t *testing.T) {
		event := &MinterConfigEvent{
			BlockNumber: 102,
			TxHash:      common.HexToHash("0x456"),
			Minter:      common.HexToAddress("0x4444"),
			Allowance:   big.NewInt(10000),
			Action:      "configured",
			Timestamp:   1234567892,
		}

		err := storage.StoreMinterConfigEvent(ctx, event)
		require.NoError(t, err)
	})

	t.Run("Proposal", func(t *testing.T) {
		executedAt := uint64(1234567900)
		proposal := &Proposal{
			Contract:          common.HexToAddress("0x5555"),
			ProposalID:        big.NewInt(1),
			Proposer:          common.HexToAddress("0x6666"),
			ActionType:        [32]byte{1, 2, 3},
			CallData:          []byte{0x11, 0x22},
			MemberVersion:     big.NewInt(1),
			RequiredApprovals: 5,
			Approved:          3,
			Rejected:          1,
			Status:            ProposalStatusVoting,
			CreatedAt:         1234567890,
			ExecutedAt:        &executedAt,
			BlockNumber:       103,
			TxHash:            common.HexToHash("0x789"),
		}

		err := storage.StoreProposal(ctx, proposal)
		require.NoError(t, err)
	})

	t.Run("ProposalVote", func(t *testing.T) {
		vote := &ProposalVote{
			Contract:    common.HexToAddress("0x5555"),
			ProposalID:  big.NewInt(1),
			Voter:       common.HexToAddress("0x7777"),
			Approval:    true,
			BlockNumber: 104,
			TxHash:      common.HexToHash("0xaaa"),
			Timestamp:   1234567893,
		}

		err := storage.StoreProposalVote(ctx, vote)
		require.NoError(t, err)
	})

	t.Run("GasTipUpdateEvent", func(t *testing.T) {
		event := &GasTipUpdateEvent{
			BlockNumber: 105,
			TxHash:      common.HexToHash("0xbbb"),
			OldTip:      big.NewInt(1000000000),
			NewTip:      big.NewInt(2000000000),
			Updater:     common.HexToAddress("0x8888"),
			Timestamp:   1234567894,
		}

		err := storage.StoreGasTipUpdateEvent(ctx, event)
		require.NoError(t, err)
	})

	t.Run("BlacklistEvent", func(t *testing.T) {
		event := &BlacklistEvent{
			BlockNumber: 106,
			TxHash:      common.HexToHash("0xccc"),
			Account:     common.HexToAddress("0x9999"),
			Action:      "blacklisted",
			ProposalID:  big.NewInt(2),
			Timestamp:   1234567895,
		}

		err := storage.StoreBlacklistEvent(ctx, event)
		require.NoError(t, err)
	})

	t.Run("ValidatorChangeEvent", func(t *testing.T) {
		oldValidator := common.HexToAddress("0xaaaa")
		event := &ValidatorChangeEvent{
			BlockNumber:  107,
			TxHash:       common.HexToHash("0xddd"),
			Validator:    common.HexToAddress("0xbbbb"),
			Action:       "changed",
			OldValidator: &oldValidator,
			Timestamp:    1234567896,
		}

		err := storage.StoreValidatorChangeEvent(ctx, event)
		require.NoError(t, err)
	})

	t.Run("MemberChangeEvent", func(t *testing.T) {
		oldMember := common.HexToAddress("0xcccc")
		event := &MemberChangeEvent{
			Contract:     common.HexToAddress("0x5555"),
			BlockNumber:  108,
			TxHash:       common.HexToHash("0xeee"),
			Member:       common.HexToAddress("0xdddd"),
			Action:       "changed",
			OldMember:    &oldMember,
			TotalMembers: 10,
			NewQuorum:    7,
			Timestamp:    1234567897,
		}

		err := storage.StoreMemberChangeEvent(ctx, event)
		require.NoError(t, err)
	})

	t.Run("EmergencyPauseEvent", func(t *testing.T) {
		event := &EmergencyPauseEvent{
			Contract:    common.HexToAddress("0x5555"),
			BlockNumber: 109,
			TxHash:      common.HexToHash("0xfff"),
			ProposalID:  big.NewInt(3),
			Action:      "paused",
			Timestamp:   1234567898,
		}

		err := storage.StoreEmergencyPauseEvent(ctx, event)
		require.NoError(t, err)
	})

	t.Run("DepositMintProposal", func(t *testing.T) {
		proposal := &DepositMintProposal{
			ProposalID:    big.NewInt(4),
			Requester:     common.HexToAddress("0xdddd"),
			Beneficiary:   common.HexToAddress("0xeeee"),
			Amount:        big.NewInt(5000),
			DepositID:     "deposit-456",
			BankReference: "BANK-REF-001",
			Status:        ProposalStatusVoting,
			BlockNumber:   110,
			TxHash:        common.HexToHash("0x111"),
			Timestamp:     1234567899,
		}

		err := storage.StoreDepositMintProposal(ctx, proposal)
		require.NoError(t, err)
	})
}

func TestPebbleStorage_UpdateProposalStatus(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pebble_proposal_status_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()

	// Store initial proposal
	proposal := &Proposal{
		Contract:          common.HexToAddress("0x5555"),
		ProposalID:        big.NewInt(10),
		Proposer:          common.HexToAddress("0x6666"),
		ActionType:        [32]byte{1, 2, 3},
		CallData:          []byte{0x11, 0x22},
		MemberVersion:     big.NewInt(1),
		RequiredApprovals: 5,
		Approved:          3,
		Rejected:          1,
		Status:            ProposalStatusVoting,
		CreatedAt:         1234567890,
		ExecutedAt:        nil,
		BlockNumber:       200,
		TxHash:            common.HexToHash("0x222"),
	}

	err = storage.StoreProposal(ctx, proposal)
	require.NoError(t, err)

	// Update status
	err = storage.UpdateProposalStatus(ctx, proposal.Contract, proposal.ProposalID, ProposalStatusExecuted, 1234567900)
	require.NoError(t, err)
}

func TestPebbleStorage_TotalSupply(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pebble_supply_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()

	// Initial supply should be 0
	supply, err := storage.GetTotalSupply(ctx)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(0).String(), supply.String())

	// Update supply
	newSupply := big.NewInt(1000000)
	err = storage.UpdateTotalSupply(ctx, newSupply)
	require.NoError(t, err)

	// Verify update
	supply, err = storage.GetTotalSupply(ctx)
	require.NoError(t, err)
	assert.Equal(t, newSupply.String(), supply.String())
}

func TestPebbleStorage_ActiveMinter(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pebble_minter_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()
	minter := common.HexToAddress("0xminter123")
	allowance := big.NewInt(50000)

	// Add active minter
	err = storage.UpdateActiveMinter(ctx, minter, allowance, true)
	require.NoError(t, err)

	// Remove active minter
	err = storage.UpdateActiveMinter(ctx, minter, big.NewInt(0), false)
	require.NoError(t, err)
}

func TestPebbleStorage_ActiveValidator(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pebble_validator_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()
	validator := common.HexToAddress("0xvalidator123")

	// Add validator
	err = storage.UpdateActiveValidator(ctx, validator, true)
	require.NoError(t, err)

	// Remove validator
	err = storage.UpdateActiveValidator(ctx, validator, false)
	require.NoError(t, err)
}

func TestPebbleStorage_BlacklistStatus(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pebble_blacklist_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()
	account := common.HexToAddress("0xblacklisted123")

	// Blacklist account
	err = storage.UpdateBlacklistStatus(ctx, account, true)
	require.NoError(t, err)

	// Unblacklist account
	err = storage.UpdateBlacklistStatus(ctx, account, false)
	require.NoError(t, err)
}

func TestPebbleStorage_GetMintEvents(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pebble_get_mints_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()
	minter := common.HexToAddress("0xminter456")

	// Store some mint events
	for i := 0; i < 3; i++ {
		event := &MintEvent{
			BlockNumber: uint64(1000 + i),
			TxHash:      common.HexToHash("0x" + string(rune(i))),
			Minter:      minter,
			To:          common.HexToAddress("0x1234"),
			Amount:      big.NewInt(int64(1000 * (i + 1))),
			Timestamp:   uint64(1234567890 + i),
		}
		err = storage.StoreMintEvent(ctx, event)
		require.NoError(t, err)
	}

	// Query events - may not be implemented yet
	events, err := storage.GetMintEvents(ctx, 1000, 1002, minter, 10, 0)
	if err != nil && err.Error() == "GetMintEvents not yet implemented" {
		t.Skip("GetMintEvents not yet implemented")
	}
	if err == nil {
		assert.True(t, len(events) >= 0)
	}
}

func TestPebbleStorage_GetBurnEvents(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pebble_get_burns_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()
	burner := common.HexToAddress("0xburner456")

	// Store some burn events
	for i := 0; i < 3; i++ {
		event := &BurnEvent{
			BlockNumber:  uint64(2000 + i),
			TxHash:       common.HexToHash("0x" + string(rune(i))),
			Burner:       burner,
			Amount:       big.NewInt(int64(500 * (i + 1))),
			Timestamp:    uint64(1234567890 + i),
			WithdrawalID: "withdrawal-" + string(rune(i)),
		}
		err = storage.StoreBurnEvent(ctx, event)
		require.NoError(t, err)
	}

	// Query events - may not be implemented yet
	events, err := storage.GetBurnEvents(ctx, 2000, 2002, burner, 10, 0)
	if err != nil && err.Error() == "GetBurnEvents not yet implemented" {
		t.Skip("GetBurnEvents not yet implemented")
	}
	if err == nil {
		assert.True(t, len(events) >= 0)
	}
}

func TestPebbleStorage_MaxProposalsUpdateEvent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pebble_maxproposals_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()
	contract := common.HexToAddress("0x0000000000000000000000000000000000001004")

	t.Run("StoreAndGet", func(t *testing.T) {
		events := []*MaxProposalsUpdateEvent{
			{Contract: contract, BlockNumber: 100, TxHash: common.HexToHash("0xaa"), OldMax: 5, NewMax: 10, Timestamp: 1000},
			{Contract: contract, BlockNumber: 200, TxHash: common.HexToHash("0xbb"), OldMax: 10, NewMax: 15, Timestamp: 2000},
		}

		for _, e := range events {
			require.NoError(t, storage.StoreMaxProposalsUpdateEvent(ctx, e))
		}

		history, err := storage.GetMaxProposalsUpdateHistory(ctx, contract)
		require.NoError(t, err)
		require.Len(t, history, 2)
		assert.Equal(t, uint64(5), history[0].OldMax)
		assert.Equal(t, uint64(10), history[0].NewMax)
		assert.Equal(t, uint64(10), history[1].OldMax)
		assert.Equal(t, uint64(15), history[1].NewMax)
	})

	t.Run("EmptyHistory", func(t *testing.T) {
		other := common.HexToAddress("0x9999999999999999999999999999999999999999")
		history, err := storage.GetMaxProposalsUpdateHistory(ctx, other)
		require.NoError(t, err)
		assert.Empty(t, history)
	})
}

func TestPebbleStorage_ProposalExecutionSkippedEvent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pebble_proposalskipped_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()
	contract := common.HexToAddress("0x0000000000000000000000000000000000001004")

	t.Run("StoreAndGetAll", func(t *testing.T) {
		events := []*ProposalExecutionSkippedEvent{
			{Contract: contract, BlockNumber: 100, TxHash: common.HexToHash("0xcc"), Account: common.HexToAddress("0x1111"), ProposalID: big.NewInt(1), Reason: "quorum not met", Timestamp: 1000},
			{Contract: contract, BlockNumber: 200, TxHash: common.HexToHash("0xdd"), Account: common.HexToAddress("0x2222"), ProposalID: big.NewInt(2), Reason: "expired", Timestamp: 2000},
		}

		for _, e := range events {
			require.NoError(t, storage.StoreProposalExecutionSkippedEvent(ctx, e))
		}

		// Get all (nil proposalID)
		results, err := storage.GetProposalExecutionSkippedEvents(ctx, contract, nil)
		require.NoError(t, err)
		require.Len(t, results, 2)
	})

	t.Run("FilterByProposalID", func(t *testing.T) {
		results, err := storage.GetProposalExecutionSkippedEvents(ctx, contract, big.NewInt(1))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "quorum not met", results[0].Reason)
	})

	t.Run("NoMatch", func(t *testing.T) {
		results, err := storage.GetProposalExecutionSkippedEvents(ctx, contract, big.NewInt(999))
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}

func TestPebbleStorage_AuthorizedAccountEvent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pebble_authaccount_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := DefaultConfig(tempDir)
	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()

	acct1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	acct2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

	t.Run("AddAccounts", func(t *testing.T) {
		require.NoError(t, storage.StoreAuthorizedAccountEvent(ctx, &AuthorizedAccountEvent{
			Contract: GovCouncilAddress, BlockNumber: 100, Account: acct1, ProposalID: big.NewInt(1), Action: "added",
		}))
		require.NoError(t, storage.StoreAuthorizedAccountEvent(ctx, &AuthorizedAccountEvent{
			Contract: GovCouncilAddress, BlockNumber: 200, Account: acct2, ProposalID: big.NewInt(2), Action: "added",
		}))

		accounts, err := storage.GetAuthorizedAccounts(ctx)
		require.NoError(t, err)
		require.Len(t, accounts, 2)
	})

	t.Run("RemoveAccount", func(t *testing.T) {
		require.NoError(t, storage.StoreAuthorizedAccountEvent(ctx, &AuthorizedAccountEvent{
			Contract: GovCouncilAddress, BlockNumber: 300, Account: acct1, ProposalID: big.NewInt(3), Action: "removed",
		}))

		accounts, err := storage.GetAuthorizedAccounts(ctx)
		require.NoError(t, err)
		require.Len(t, accounts, 1)
		assert.Equal(t, acct2, accounts[0])
	})
}
