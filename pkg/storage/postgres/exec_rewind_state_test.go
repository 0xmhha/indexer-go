package postgres

import (
	"context"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func rewindStateFixture(t *testing.T) (*Store, port.ExecutionConfig, execwire.Address, execwire.Account, execwire.Result) {
	t.Helper()
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	addr := execwire.Address{1}
	code := []byte{0x60, 1, 0}
	a := execwire.Account{Exists: true, CodeHash: execwire.Hash(crypto.Keccak256Hash(code)), StorageRoot: execwire.Hash{11}, Nonce: 1}
	a.Balance = storageWord(9)
	require.NoError(t, s.SeedExecution(t.Context(), port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot, Accounts: []execwire.AccountChange{baselineAccount(addr, a)}, Storage: []execwire.StorageChange{{Address: addr, Slot: storageWord(1), Before: storageWord(3), After: storageWord(3), Baseline: true}, {Address: addr, Slot: storageWord(2), Baseline: true}}, Code: []execwire.Code{{Hash: a.CodeHash, Bytes: code}}}))
	nextCode := []byte{0x60, 2, 0}
	b := a
	b.CodeHash = execwire.Hash(crypto.Keccak256Hash(nextCode))
	b.StorageRoot = execwire.Hash{12}
	b.Nonce++
	r := accountResult(cfg, addr, a, b, b)
	r.Steps[0].Storage = []execwire.StorageChange{{Address: addr, Slot: storageWord(1), Before: storageWord(3), After: storageWord(4)}, {Address: addr, Slot: storageWord(2), After: storageWord(5)}}
	r.Steps[0].Code = []execwire.Code{{Hash: b.CodeHash, Bytes: nextCode}}
	writeRewindStateResult(t, s, r, true)
	r2 := accountResult(cfg, addr, b, execwire.Account{}, execwire.Account{})
	r2.Ref.Height = 2
	r2.Ref.Hash = execwire.Hash{3}
	r2.Parent = r.Ref.Hash
	r2.Steps[0].TxHash = execwire.Hash{8}
	r2.Steps[0].Accounts[0].StorageCleared = true
	r2.Steps[0].Accounts = append(r2.Steps[0].Accounts, execwire.AccountChange{Address: execwire.Address{2}, After: execwire.Account{Exists: true, Nonce: 1, CodeHash: execwire.Hash(types.EmptyCodeHash), StorageRoot: execwire.Hash(types.EmptyRootHash)}})
	writeRewindStateResult(t, s, r2, false)
	return s, cfg, addr, a, r
}
func writeRewindStateResult(t *testing.T, s *Store, r execwire.Result, publish bool) {
	t.Helper()
	b := &model.Block{Number: r.Ref.Height, Hash: common.Hash(r.Ref.Hash), ParentHash: common.Hash(r.Parent), StateRoot: common.Hash(r.StateRoot), Time: r.Time, Transactions: []*model.Transaction{{Hash: common.Hash(r.Steps[0].TxHash), From: common.Address{1}, BlockNumber: r.Ref.Height, BlockHash: common.Hash(r.Ref.Hash), Index: 0, Value: big.NewInt(0)}}}
	rs := []*model.Receipt{{TxHash: b.Transactions[0].Hash, TxIndex: 0, BlockNumber: b.Number, BlockHash: b.Hash, Status: 1}}
	for _, w := range port.ExecutionWriters() {
		p := statePart(t, w, r)
		require.NoError(t, s.WriteExecutionPart(t.Context(), p, func(ctx context.Context) error {
			switch w {
			case port.ExecutionBlocks, port.ExecutionTransactions, port.ExecutionReceipts, port.ExecutionLogs:
				return s.ApplyExecutionCore(ctx, w, b, rs)
			default:
				return s.ApplyExecutionState(ctx, w, r)
			}
		}))
	}
	if publish {
		p := statePart(t, port.ExecutionBlocks, r)
		_, err := s.CommitExecution(t.Context(), port.ExecutionPublication{Ref: r.Ref, Parent: r.Parent, Digest: p.Digest, Root: r.StateRoot}, func(ctx context.Context) error { return s.Put(ctx, []byte("state-rewind-feature"), []byte("one")) }, func(context.Context) error { return nil })
		require.NoError(t, err)
	}
}
func TestExecutionRewindRestoresStateClearCodeAndRemovesNewAccount(t *testing.T) {
	s, cfg, addr, a, r := rewindStateFixture(t)
	before, err := s.VisibleExecutionAccount(t.Context(), addr)
	require.NoError(t, err)
	require.Equal(t, r.Steps[0].Accounts[0].After, before.Account)
	marker, err := s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, err)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	account, err := s.VisibleExecutionAccount(t.Context(), addr)
	require.NoError(t, err)
	require.Equal(t, a, account.Account)
	slot, err := s.VisibleExecutionStorage(t.Context(), addr, storageWord(1))
	require.NoError(t, err)
	require.Equal(t, storageWord(3), slot.Value)
	code, err := s.VisibleExecutionCode(t.Context(), addr)
	require.NoError(t, err)
	require.Equal(t, []byte{0x60, 1, 0}, code.Code)
	_, err = s.VisibleExecutionAccount(t.Context(), execwire.Address{2})
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
	newAddr := execwire.Address{2}
	var latest int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM state_accounts WHERE address=$1", newAddr[:]).Scan(&latest))
	require.Zero(t, latest)
	r.Ref.Generation = marker.Target.Generation
	writeRewindStateResult(t, s, r, true)
}

func TestExecutionRewindMissingZeroBaselineFailsClosed(t *testing.T) {
	s, cfg, addr, _, _ := rewindStateFixture(t)
	key := storageWord(2)
	_, err := s.pool.Exec(t.Context(), "DELETE FROM state_storage_changes WHERE height=0 AND address=$1 AND slot=$2", addr[:], key[:])
	require.NoError(t, err)
	_, err = s.BeginExecutionRewind(t.Context(), cfg.Anchor, cfg.AnchorRoot)
	require.NoError(t, err)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.ErrorIs(t, err, port.ErrExecutionCoverage, "a missing explicit zero in a nonempty trie is not arbitrary zero coverage")
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, c.Rewinding)
}
