package systemcontracts

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// TestSystemContractEventsInOneBlockAreKept stores two events of the same
// kind in one block. Keys used to omit the log position (and hard-coded the
// transaction index to 0), so the second event overwrote the first (D21).
func TestSystemContractEventsInOneBlockAreKept(t *testing.T) {
	s := newTestPebble(t)
	ctx := context.Background()
	gov := common.HexToAddress("0x0000000000000000000000000000000000001001")
	alice := common.HexToAddress("0x00000000000000000000000000000000000000A1")
	bob := common.HexToAddress("0x00000000000000000000000000000000000000B2")

	require.NoError(t, s.StoreGasTipUpdateEvent(ctx, &GasTipUpdateEvent{BlockNumber: 7, LogIndex: 1, OldTip: big.NewInt(1), NewTip: big.NewInt(2)}))
	require.NoError(t, s.StoreGasTipUpdateEvent(ctx, &GasTipUpdateEvent{BlockNumber: 7, LogIndex: 4, OldTip: big.NewInt(2), NewTip: big.NewInt(3)}))
	tips, err := s.GetGasTipHistory(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, tips, 2)

	require.NoError(t, s.StoreMemberChangeEvent(ctx, &MemberChangeEvent{Contract: gov, BlockNumber: 7, LogIndex: 2, Member: alice, Action: "added"}))
	require.NoError(t, s.StoreMemberChangeEvent(ctx, &MemberChangeEvent{Contract: gov, BlockNumber: 7, LogIndex: 3, Member: bob, Action: "added"}))
	members, err := s.GetMemberHistory(ctx, gov)
	require.NoError(t, err)
	require.Len(t, members, 2)

	require.NoError(t, s.StoreBlacklistEvent(ctx, &BlacklistEvent{BlockNumber: 7, LogIndex: 5, Account: alice, Action: "blacklisted"}))
	require.NoError(t, s.StoreBlacklistEvent(ctx, &BlacklistEvent{BlockNumber: 7, LogIndex: 6, Account: alice, Action: "unblacklisted"}))
	bl, err := s.GetBlacklistHistory(ctx, alice)
	require.NoError(t, err)
	require.Len(t, bl, 2)
}

// TestMintAndBurnFilters stores two mints and two burns in one block and
// queries them with and without the minter/burner filter. The filtered
// queries read an index that was never written, so they returned nothing
// (D22), and unfiltered keys overwrote each other (D21).
func TestMintAndBurnFilters(t *testing.T) {
	s := newTestPebble(t)
	ctx := context.Background()
	m1 := common.HexToAddress("0x00000000000000000000000000000000000000C1")
	m2 := common.HexToAddress("0x00000000000000000000000000000000000000C2")

	require.NoError(t, s.StoreMintEvent(ctx, &MintEvent{BlockNumber: 9, TxIndex: 0, LogIndex: 0, Minter: m1, To: m2, Amount: big.NewInt(1)}))
	require.NoError(t, s.StoreMintEvent(ctx, &MintEvent{BlockNumber: 9, TxIndex: 1, LogIndex: 3, Minter: m2, To: m1, Amount: big.NewInt(2)}))
	all, err := s.GetMintEvents(ctx, 0, 20, common.Address{}, 0, 0)
	require.NoError(t, err)
	require.Len(t, all, 2)
	byM2, err := s.GetMintEvents(ctx, 0, 20, m2, 0, 0)
	require.NoError(t, err)
	require.Len(t, byM2, 1)
	require.Zero(t, byM2[0].Amount.Cmp(big.NewInt(2)))

	require.NoError(t, s.StoreBurnEvent(ctx, &BurnEvent{BlockNumber: 9, TxIndex: 2, LogIndex: 5, Burner: m1, Amount: big.NewInt(3)}))
	require.NoError(t, s.StoreBurnEvent(ctx, &BurnEvent{BlockNumber: 9, TxIndex: 2, LogIndex: 6, Burner: m1, Amount: big.NewInt(4)}))
	burns, err := s.GetBurnEvents(ctx, 0, 20, m1, 0, 0)
	require.NoError(t, err)
	require.Len(t, burns, 2)
}
