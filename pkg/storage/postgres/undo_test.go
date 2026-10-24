package postgres

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/userop"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// untracked lists the tables whose changes a rollback keeps: the schema
// version, undo itself, orphans and the outbox (migration 0006).
var untracked = map[string]bool{
	"schema_migrations": true, "undo_log": true, "undo_blocks": true, "undo_by_height": true,
	"reorgs": true, "orphaned_blocks": true, "orphaned_transactions": true,
	"outbox": true, "outbox_sequence": true, "outbox_cursors": true,
	"capture_locks": true, "capture_records": true, // immutable source facts, not projection writes
	"exec_control": true, "exec_cursors": true, "exec_parts": true, "exec_publications": true, "exec_rewinds": true, "exec_retention": true, // execution rewind coordinator owns these
	"state_accounts": true, "state_account_changes": true, // execution history/rewind owns parent and leaf partitions
	"state_storage": true, "state_storage_roots": true, "state_storage_changes": true, "state_storage_clears": true, // execution storage/clear history and rewind
	"state_code": true, "state_contracts": true, "state_contract_changes": true, // execution code retention and contract rewind
}

// trackedTables returns the tables of the store's schema that are not in
// untracked, and whether each is undone: by the undo trigger, or by height
// (listed in undo_by_height, with its undo_height column; migration 0012).
func trackedTables(t *testing.T, s *Store) map[string]bool {
	t.Helper()
	rows, err := s.pool.Query(context.Background(), `SELECT c.relname,
			EXISTS (SELECT 1 FROM pg_trigger g WHERE g.tgrelid = c.oid AND g.tgname = 'undo_record')
			OR (EXISTS (SELECT 1 FROM undo_by_height u WHERE u.tbl = c.relname)
				AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'undo_height')),
			COALESCE((SELECT p.relname FROM pg_inherits i JOIN pg_class p ON p.oid=i.inhparent
				WHERE i.inhrelid=c.oid AND c.relispartition AND p.relnamespace=c.relnamespace LIMIT 1),'')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind = 'r'`)
	require.NoError(t, err)
	out := map[string]bool{}
	for rows.Next() {
		var (
			name, parent string
			tracked      bool
		)
		require.NoError(t, rows.Scan(&name, &tracked, &parent))
		if !untracked[name] && !untracked[parent] {
			out[name] = tracked
		}
	}
	require.NoError(t, rows.Err())
	return out
}

// TestEveryTableIsUndoTracked: every table of indexed data records its
// changes for undo. A migration that adds a table calls undo_track for it,
// or adds it to untracked when a rollback must keep its rows.
func TestEveryTableIsUndoTracked(t *testing.T) {
	s := newTestStore(t)
	tables := trackedTables(t, s)
	require.NotEmpty(t, tables)
	for name, tracked := range tables {
		assert.True(t, tracked, "table %s is not undone: call undo_track('%s') (or undo_by_height_track for a table blocks only insert into) in its migration", name, name)
	}
}

// snapshot returns the rows of every tracked table, as JSON in a fixed
// order.
func snapshot(t *testing.T, s *Store) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name := range trackedTables(t, s) {
		var rows string
		err := s.pool.QueryRow(context.Background(), "SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text, '[]') FROM "+
			pgx.Identifier{name}.Sanitize()+" t").Scan(&rows)
		require.NoError(t, err, name)
		out[name] = rows
	}
	return out
}

// TestRollbackRestoresEveryTable: a block whose writes insert, update and
// delete rows across the ports rolls back to exactly the rows every tracked
// table held before it.
func TestRollbackRestoresEveryTable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a, b, token := common.HexToAddress("0x0a"), common.HexToAddress("0x0b"), common.HexToAddress("0x70")
	tx1, tx2 := common.Hash{0x11}, common.Hash{0x22}

	write := func(height uint64, fn func(ctx context.Context)) {
		t.Helper()
		txCtx, tx, err := s.BeginBlock(ctx)
		require.NoError(t, err)
		tx.SetHeight(height)
		require.NoError(t, s.SetBlock(txCtx, &model.Block{Number: height, Hash: common.Hash{byte(height)}, Time: 1000 + height,
			Transactions: []*model.Transaction{{Hash: common.Hash{0xee, byte(height)}, From: a, To: &b, Value: big.NewInt(1)}}}))
		require.NoError(t, s.SetReceipt(txCtx, &model.Receipt{TxHash: common.Hash{0xee, byte(height)}, BlockNumber: height,
			BlockHash: common.Hash{byte(height)}, Status: model.ReceiptStatusSuccessful, Logs: []*model.Log{}}))
		require.NoError(t, s.SetLatestHeight(txCtx, height))
		fn(txCtx)
		require.NoError(t, tx.Commit())
	}

	// Block 1 creates the rows block 2 changes.
	write(1, func(ctx context.Context) {
		require.NoError(t, s.Put(ctx, []byte("k/updated"), []byte("1")))
		require.NoError(t, s.Put(ctx, []byte("k/deleted"), []byte("1")))
		require.NoError(t, s.AddTransactionToAddressIndex(ctx, a, tx1))
		require.NoError(t, s.UpdateBalance(ctx, a, 1, big.NewInt(100), tx1))
		require.NoError(t, s.ProcessERC20TransferForHolders(ctx, &port.ERC20Transfer{ContractAddress: token, From: common.Address{}, To: a, Value: big.NewInt(50), TransactionHash: tx1, BlockNumber: 1}))
		require.NoError(t, s.SaveInternalTransactions(ctx, tx1, []*port.InternalTransaction{{TransactionHash: tx1, BlockNumber: 1, Type: "CALL", From: a, To: b, Value: big.NewInt(1)}}))
		require.NoError(t, s.SaveInstalledModule(ctx, &port.InstalledModule{Account: a, Module: b, ModuleType: port.ModuleTypeValidator, InstalledAt: 1, Active: true}))
		require.NoError(t, s.SetFeatureState(ctx, "f", port.FeatureState{Active: true}))
	})
	before := snapshot(t, s)

	write(2, func(ctx context.Context) {
		require.NoError(t, s.Put(ctx, []byte("k/updated"), []byte("2")))
		require.NoError(t, s.Delete(ctx, []byte("k/deleted")))
		require.NoError(t, s.Put(ctx, []byte("k/added"), []byte("2")))
		require.NoError(t, s.AddTransactionToAddressIndex(ctx, b, tx2))
		require.NoError(t, s.UpdateBalance(ctx, a, 2, big.NewInt(-40), tx2))
		require.NoError(t, s.SetBalance(ctx, b, 2, big.NewInt(7)))
		require.NoError(t, s.ProcessERC20TransferForHolders(ctx, &port.ERC20Transfer{ContractAddress: token, From: a, To: b, Value: big.NewInt(20), TransactionHash: tx2, BlockNumber: 2, LogIndex: 1}))
		require.NoError(t, s.SaveERC20Transfer(ctx, &port.ERC20Transfer{ContractAddress: token, From: a, To: b, Value: big.NewInt(20), TransactionHash: tx2, BlockNumber: 2, LogIndex: 1}))
		require.NoError(t, s.SaveInternalTransactions(ctx, tx1, nil))
		require.NoError(t, s.RemoveModule(ctx, a, b, 2, tx2))
		require.NoError(t, s.SaveUserOp(ctx, &userop.UserOperation{Hash: common.Hash{0x0f}, Sender: a, Bundler: b, BlockNumber: 2}))
		require.NoError(t, s.SetABI(ctx, token, []byte("[]")))
		require.NoError(t, s.IncrementSetCodeStats(ctx, a, true, false, 2))
		require.NoError(t, s.SetFeatureState(ctx, "f", port.FeatureState{Through: 2}))
		// A row of another block number written by block 2, as the
		// genesis balance lookup writes block-0 history.
		require.NoError(t, s.SetBalance(ctx, common.HexToAddress("0x0c"), 0, big.NewInt(9)))
	})
	require.NotEqual(t, before, snapshot(t, s), "block 2 changed rows")

	_, err := s.RollbackTo(ctx, 1, nil)
	require.NoError(t, err)
	// The rollback records the reorganization's sequence; that row is the
	// rollback's own write, not block 2's.
	_, err = s.pool.Exec(ctx, "DELETE FROM meta WHERE name = $1", metaReorgSeq)
	require.NoError(t, err)
	after := snapshot(t, s)
	for name, rows := range before {
		assert.Equal(t, rows, after[name], "table %s after the rollback", name)
	}
}

// TestUndoByHeightGuard: a block transaction may not update or delete a
// row another height wrote in a table undone by height, since rolling the
// block back would not restore it; outside block transactions (rollback,
// maintenance) it may.
func TestUndoByHeightGuard(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := common.HexToAddress("0x0a")
	txCtx, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	tx.SetHeight(1)
	require.NoError(t, s.AddTransactionToAddressIndex(txCtx, a, common.Hash{0x11}))
	require.NoError(t, tx.Commit())

	var height *int64
	require.NoError(t, s.pool.QueryRow(ctx, "SELECT undo_height FROM address_transactions").Scan(&height))
	require.NotNil(t, height)
	assert.Equal(t, int64(1), *height, "the row records the height that wrote it")

	txCtx, tx, err = s.BeginBlock(ctx)
	require.NoError(t, err)
	tx.SetHeight(2)
	_, err = s.q(txCtx).Exec(txCtx, "UPDATE address_transactions SET tx_hash = $1", common.Hash{0x22}.Bytes())
	require.ErrorContains(t, err, "may not change a row of address_transactions written by block 1")
	tx.Rollback()

	_, err = s.pool.Exec(ctx, "DELETE FROM address_transactions")
	require.NoError(t, err, "outside a block transaction")
}
