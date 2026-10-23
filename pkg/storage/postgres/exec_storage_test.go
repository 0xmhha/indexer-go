package postgres

import (
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func storageWord(n byte) execwire.Word { return execwire.Word{31: n} }
func storageFixture(t *testing.T) (*Store, port.ExecutionConfig, execwire.Address, execwire.Account) {
	t.Helper()
	s, cfg := executionFixture(t)
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	addr := execwire.Address{1}
	a := execwire.Account{Exists: true, CodeHash: execwire.Hash(types.EmptyCodeHash), StorageRoot: execwire.Hash{11}}
	seed := port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot, Accounts: []execwire.AccountChange{baselineAccount(addr, a)}, Storage: []execwire.StorageChange{
		{Address: addr, Slot: storageWord(1), Before: storageWord(3), After: storageWord(3), Baseline: true},
		{Address: addr, Slot: storageWord(2), Before: storageWord(7), After: storageWord(7), Baseline: true},
	}}
	require.NoError(t, s.SeedExecution(t.Context(), seed))
	return s, cfg, addr, a
}
func TestExecutionStorageBaselineCASAndIndependentWriter(t *testing.T) {
	s, cfg, addr, a := storageFixture(t)
	value, err := s.ExecutionStorageAt(t.Context(), addr, storageWord(1), 0)
	require.NoError(t, err)
	require.Equal(t, storageWord(3), value)
	b, c := a, a
	b.StorageRoot[0] = 12
	c.StorageRoot[0] = 13
	r := accountResult(cfg, addr, a, b, c)
	r.Steps[0].Storage = []execwire.StorageChange{{Address: addr, Slot: storageWord(1), Before: storageWord(3), After: storageWord(4)}}
	r.Steps[1].Storage = []execwire.StorageChange{{Address: addr, Slot: storageWord(1), Before: storageWord(4), After: storageWord(5)}}
	bad := r
	bad.Steps = append([]execwire.Step(nil), r.Steps...)
	bad.Steps[0].Storage = []execwire.StorageChange{{Address: addr, Slot: storageWord(1), Before: storageWord(9), After: storageWord(4)}}
	require.ErrorIs(t, applyStatePart(t, s, port.ExecutionStorage, bad), port.ErrExecutionConflict)
	require.NoError(t, applyStatePart(t, s, port.ExecutionStorage, r), "storage does not wait for accounts writer")
	for height, want := range map[uint64]execwire.Word{0: storageWord(3), 1: storageWord(5)} {
		got, err := s.ExecutionStorageAt(t.Context(), addr, storageWord(1), height)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, err = s.ExecutionStorageAt(t.Context(), addr, storageWord(9), 1)
	require.ErrorIs(t, err, port.ErrExecutionCoverage)
}
func TestExecutionStorageClearPreservesBeforeAndHistory(t *testing.T) {
	s, cfg, addr, a := storageFixture(t)
	b := a
	b.StorageRoot[0] = 14
	r := accountResult(cfg, addr, a, b, b)
	r.Steps[0].Accounts[0].StorageCleared = true
	// Before is the phase-before value, not zero after the clear.
	r.Steps[0].Storage = []execwire.StorageChange{{Address: addr, Slot: storageWord(1), Before: storageWord(3), After: storageWord(9)}}
	require.NoError(t, applyStatePart(t, s, port.ExecutionStorage, r))
	got, err := s.ExecutionStorageAt(t.Context(), addr, storageWord(1), 1)
	require.NoError(t, err)
	require.Equal(t, storageWord(9), got, "same-ordinal after write survives clear")
	got, err = s.ExecutionStorageAt(t.Context(), addr, storageWord(2), 1)
	require.NoError(t, err)
	require.Zero(t, got, "untouched old slot cannot reappear")
	got, err = s.ExecutionStorageAt(t.Context(), addr, storageWord(2), 0)
	require.NoError(t, err)
	require.Equal(t, storageWord(7), got, "as-of before clear retains old slot")
	got, err = s.ExecutionStorageAt(t.Context(), addr, storageWord(9), 1)
	require.NoError(t, err)
	require.Zero(t, got, "clear proves absent slots")
	var retained []byte
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT value FROM state_storage WHERE address=$1 AND slot=$2", addr[:], storageWordBytes(2)).Scan(&retained))
	require.Equal(t, storageWordBytes(7), retained, "latest-row retention alone cannot define logical slot state")
}
func storageWordBytes(n byte) []byte { w := storageWord(n); return w[:] }

func TestExecutionEmptyStorageProofAndUnknownCoverage(t *testing.T) {
	s, cfg, addr, a := accountFixture(t)
	got, err := s.ExecutionStorageAt(t.Context(), addr, storageWord(9), 0)
	require.NoError(t, err)
	require.Zero(t, got, "covered empty root proves zero")
	b := a
	b.StorageRoot[0] = 15
	r := accountResult(cfg, addr, a, b, b)
	r.Steps[0].Storage = []execwire.StorageChange{{Address: addr, Slot: storageWord(9), After: storageWord(1)}}
	require.NoError(t, applyStatePart(t, s, port.ExecutionStorage, r))
	got, err = s.ExecutionStorageAt(t.Context(), addr, storageWord(9), 1)
	require.NoError(t, err)
	require.Equal(t, storageWord(1), got)
	got, err = s.ExecutionStorageAt(t.Context(), addr, storageWord(8), 1)
	require.NoError(t, err)
	require.Zero(t, got, "an established empty baseline still covers never-written slots")
	s2, cfg2, addr2, a2 := storageFixture(t)
	r2 := accountResult(cfg2, addr2, a2, a2, a2)
	r2.Steps[0].Storage = []execwire.StorageChange{{Address: addr2, Slot: storageWord(9), After: storageWord(1)}}
	require.ErrorIs(t, applyStatePart(t, s2, port.ExecutionStorage, r2), port.ErrExecutionCoverage, "unknown nonempty-root slot is not silently initialized to zero")
}

func TestExecutionBaselineKeepsAnchorTimestamp(t *testing.T) {
	s, cfg := executionFixture(t)
	b, err := s.GetBlock(t.Context(), 0)
	require.NoError(t, err)
	b.Time = 1234
	require.NoError(t, s.SetBlock(t.Context(), b))
	require.NoError(t, s.InitExecution(t.Context(), cfg))
	a := execwire.Account{Exists: true, CodeHash: execwire.Hash(types.EmptyCodeHash), StorageRoot: execwire.Hash(types.EmptyRootHash)}
	require.NoError(t, s.SeedExecution(t.Context(), port.ExecutionBaseline{Ref: cfg.Anchor, Root: cfg.AnchorRoot, Accounts: []execwire.AccountChange{baselineAccount(execwire.Address{1}, a)}}))
	var stamp time.Time
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT block_time FROM state_account_changes WHERE height=0").Scan(&stamp))
	require.Equal(t, int64(1234), stamp.Unix())
}
