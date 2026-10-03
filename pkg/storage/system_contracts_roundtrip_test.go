package storage

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// TestSystemContractEventsRoundTrip stores one event of every type that is
// written with a binary encoder and reads it back through the public query.
// Readers used to decode these values as JSON, so every query failed as soon
// as one event existed.
func TestSystemContractEventsRoundTrip(t *testing.T) {
	s := newTestPebble(t)
	ctx := context.Background()

	a := common.HexToAddress("0x00000000000000000000000000000000000000C1")
	b := common.HexToAddress("0x00000000000000000000000000000000000000C2")
	contract := common.HexToAddress("0x0000000000000000000000000000000000001004")
	tx := common.HexToHash("0xabc")

	t.Run("mint", func(t *testing.T) {
		ev := &MintEvent{BlockNumber: 5, TxHash: tx, Minter: a, To: b, Amount: big.NewInt(100), Timestamp: 10}
		require.NoError(t, s.StoreMintEvent(ctx, ev))
		got, err := s.GetMintEvents(ctx, 0, 10, common.Address{}, 10, 0)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, ev.Minter, got[0].Minter)
		require.Equal(t, ev.To, got[0].To)
		require.Equal(t, 0, ev.Amount.Cmp(got[0].Amount))
	})

	t.Run("burn", func(t *testing.T) {
		ev := &BurnEvent{BlockNumber: 6, TxHash: tx, Burner: a, Amount: big.NewInt(7), Timestamp: 11, WithdrawalID: "w1"}
		require.NoError(t, s.StoreBurnEvent(ctx, ev))
		got, err := s.GetBurnEvents(ctx, 0, 10, common.Address{}, 10, 0)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, ev.Burner, got[0].Burner)
		require.Equal(t, 0, ev.Amount.Cmp(got[0].Amount))
	})

	t.Run("minter config", func(t *testing.T) {
		ev := &MinterConfigEvent{BlockNumber: 7, TxHash: tx, Minter: a, Allowance: big.NewInt(1000), Action: "configured", Timestamp: 12}
		require.NoError(t, s.StoreMinterConfigEvent(ctx, ev))
		hist, err := s.GetMinterHistory(ctx, a)
		require.NoError(t, err)
		require.Len(t, hist, 1)
		require.Equal(t, ev.Action, hist[0].Action)
		cfg, err := s.GetMinterConfigHistory(ctx, 0, 10)
		require.NoError(t, err)
		require.Len(t, cfg, 1)
		require.Equal(t, 0, ev.Allowance.Cmp(cfg[0].Allowance))
	})

	t.Run("gas tip", func(t *testing.T) {
		ev := &GasTipUpdateEvent{BlockNumber: 8, TxHash: tx, OldTip: big.NewInt(1), NewTip: big.NewInt(2), Updater: a, Timestamp: 13}
		require.NoError(t, s.StoreGasTipUpdateEvent(ctx, ev))
		got, err := s.GetGasTipHistory(ctx, 0, 10)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, 0, ev.NewTip.Cmp(got[0].NewTip))
	})

	t.Run("validator change", func(t *testing.T) {
		ev := &ValidatorChangeEvent{BlockNumber: 9, TxHash: tx, Validator: a, Action: "added", Timestamp: 14}
		require.NoError(t, s.StoreValidatorChangeEvent(ctx, ev))
		got, err := s.GetValidatorHistory(ctx, a)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, ev.Action, got[0].Action)
	})

	t.Run("emergency pause", func(t *testing.T) {
		ev := &EmergencyPauseEvent{Contract: contract, BlockNumber: 9, TxHash: tx, ProposalID: big.NewInt(3), Action: "paused", Timestamp: 15}
		require.NoError(t, s.StoreEmergencyPauseEvent(ctx, ev))
		got, err := s.GetEmergencyPauseHistory(ctx, contract)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, ev.Action, got[0].Action)
	})

	t.Run("deposit mint proposal", func(t *testing.T) {
		ev := &DepositMintProposal{ProposalID: big.NewInt(4), Requester: a, Beneficiary: b, Amount: big.NewInt(50),
			DepositID: "d1", BankReference: "r1", Status: ProposalStatusVoting, BlockNumber: 9, TxHash: tx, Timestamp: 16}
		require.NoError(t, s.StoreDepositMintProposal(ctx, ev))
		got, err := s.GetDepositMintProposals(ctx, 0, 10, ProposalStatusAll)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, ev.DepositID, got[0].DepositID)
	})

	t.Run("blacklist", func(t *testing.T) {
		ev := &BlacklistEvent{BlockNumber: 9, TxHash: tx, Account: b, Action: "blacklisted", ProposalID: big.NewInt(5), Timestamp: 17}
		require.NoError(t, s.StoreBlacklistEvent(ctx, ev))
		got, err := s.GetBlacklistHistory(ctx, b)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, ev.Action, got[0].Action)
	})

	t.Run("member change", func(t *testing.T) {
		ev := &MemberChangeEvent{Contract: contract, BlockNumber: 9, TxHash: tx, Member: a, Action: "added", TotalMembers: 3, NewQuorum: 2, Timestamp: 18}
		require.NoError(t, s.StoreMemberChangeEvent(ctx, ev))
		got, err := s.GetMemberHistory(ctx, contract)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, ev.Member, got[0].Member)
		require.Equal(t, ev.TotalMembers, got[0].TotalMembers)
	})
}
