package postgres

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

const executionAccountColumns = "nonce::text,balance::text,code_hash,storage_root,extra::text,exists_now"
const executionAccountAfterColumns = "nonce_after::text,balance_after::text,code_hash_after,storage_root_after,extra_after::text,exists_after"

func scanExecutionAccount(row pgx.Row) (execwire.Account, error) {
	var a execwire.Account
	var nonce, balance, extra string
	var code, root []byte
	if err := row.Scan(&nonce, &balance, &code, &root, &extra, &a.Exists); err != nil {
		return a, err
	}
	return executionAccountValue(nonce, balance, code, root, extra, a.Exists)
}

func executionAccountValue(nonce, balance string, code, root []byte, extra string, exists bool) (execwire.Account, error) {
	a := execwire.Account{Exists: exists}
	var err error
	a.Nonce, err = strconv.ParseUint(nonce, 10, 64)
	if err != nil {
		return a, err
	}
	a.Extra, err = strconv.ParseUint(extra, 10, 64)
	if err != nil {
		return a, err
	}
	n, ok := new(big.Int).SetString(balance, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 256 || len(code) != 32 || len(root) != 32 {
		return a, port.ErrInvalidData
	}
	n.FillBytes(a.Balance[:])
	copy(a.CodeHash[:], code)
	copy(a.StorageRoot[:], root)
	if !a.Exists && a != (execwire.Account{}) {
		return a, port.ErrInvalidData
	}
	return a, nil
}
func accountNumeric(a execwire.Account) (string, string, string) {
	return strconv.FormatUint(a.Nonce, 10), new(big.Int).SetBytes(a.Balance[:]).String(), strconv.FormatUint(a.Extra, 10)
}
func (s *Store) ensureExecutionPartition(ctx context.Context, table string, height, size uint64) error {
	if (table != "state_account_changes" && table != "state_storage_changes" && table != "state_storage_clears" && table != "state_contract_changes") || height > math.MaxInt64 || size == 0 || size > 1_000_000_000 {
		return port.ErrInvalidData
	}
	start := height / size * size
	end := "MAXVALUE"
	if start <= math.MaxInt64-size {
		end = strconv.FormatUint(start+size, 10)
	}
	name := table + "_p" + strconv.FormatUint(start, 10)
	// PostgreSQL partition DDL requires literal bounds. Both are checked
	// integers; the generated identifier is quoted, never caller SQL.
	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM (%d) TO (%s)", pgx.Identifier{name}.Sanitize(), pgx.Identifier{table}.Sanitize(), start, end)
	_, err := s.q(ctx).Exec(ctx, sql)
	return err
}

var executionAccountChangeColumns = []string{"height", "ordinal", "generation", "block_hash", "block_time", "tx_index", "tx_hash", "cause", "address", "existed_before", "exists_after", "nonce_before", "nonce_after", "balance_before", "balance_after", "code_hash_before", "code_hash_after", "storage_root_before", "storage_root_after", "extra_before", "extra_after", "storage_cleared", "baseline"}

func (s *Store) writeExecutionAccounts(ctx context.Context, c port.ExecutionControl, r execwire.Result, baseline bool) error {
	bt := s.boundTx(ctx)
	if bt == nil {
		return port.ErrClosed
	}
	keys := make([][]byte, 0)
	seen := map[execwire.Address]bool{}
	for _, step := range r.Steps {
		for _, a := range step.Accounts {
			if !seen[a.Address] {
				seen[a.Address] = true
				keys = append(keys, a.Address[:])
			}
		}
	}
	current := map[execwire.Address]execwire.Account{}
	rows, err := s.q(ctx).Query(ctx, "SELECT address,"+executionAccountColumns+" FROM state_accounts WHERE address=ANY($1::bytea[])", keys)
	if err != nil {
		return err
	}
	for rows.Next() {
		var addr, code, root []byte
		var nonce, balance, extra string
		var exists bool
		if err := rows.Scan(&addr, &nonce, &balance, &code, &root, &extra, &exists); err != nil {
			rows.Close()
			return err
		}
		a, err := executionAccountValue(nonce, balance, code, root, extra, exists)
		if err != nil {
			rows.Close()
			return err
		}
		var key execwire.Address
		copy(key[:], addr)
		current[key] = a
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	changes := make([][]any, 0)
	type latest struct {
		value   execwire.Account
		ordinal uint32
		txHash  []byte
	}
	last := map[execwire.Address]latest{}
	for _, step := range r.Steps {
		var txHash []byte
		if step.TxIndex >= 0 {
			txHash = step.TxHash[:]
		}
		for _, change := range step.Accounts {
			old, known := current[change.Address]
			if baseline {
				if known {
					return port.ErrExecutionConflict
				}
			} else {
				if !known && change.Before.Exists {
					return port.ErrExecutionCoverage
				}
				if old != change.Before {
					return port.ErrExecutionConflict
				}
			}
			before, after := change.Before, change.After
			nb, bb, eb := accountNumeric(before)
			na, ba, ea := accountNumeric(after)
			changes = append(changes, []any{int64(r.Ref.Height), int32(step.Ordinal), int64(r.Ref.Generation), r.Ref.Hash[:], time.Unix(int64(r.Time), 0).UTC(), step.TxIndex, txHash, int16(step.Cause), change.Address[:], before.Exists, after.Exists, nb, na, bb, ba, before.CodeHash[:], after.CodeHash[:], before.StorageRoot[:], after.StorageRoot[:], eb, ea, change.StorageCleared, baseline})
			current[change.Address] = after
			last[change.Address] = latest{after, step.Ordinal, txHash}
		}
	}
	if len(changes) == 0 {
		return nil
	}
	if err := s.ensureExecutionPartition(ctx, "state_account_changes", r.Ref.Height, c.PartitionBlocks); err != nil {
		return err
	}
	if err := bt.flush(ctx); err != nil {
		return err
	}
	if _, err := bt.tx.CopyFrom(ctx, pgx.Identifier{"state_account_changes"}, executionAccountChangeColumns, pgx.CopyFromRows(changes)); err != nil {
		return err
	}
	for addr, v := range last {
		nonce, balance, extra := accountNumeric(v.value)
		bt.queue(`INSERT INTO state_accounts(address,exists_now,nonce,balance,code_hash,storage_root,extra,first_height,last_height,last_ordinal,last_tx_hash)
   VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,$9,$10) ON CONFLICT(address) DO UPDATE SET exists_now=EXCLUDED.exists_now,nonce=EXCLUDED.nonce,balance=EXCLUDED.balance,code_hash=EXCLUDED.code_hash,storage_root=EXCLUDED.storage_root,extra=EXCLUDED.extra,last_height=EXCLUDED.last_height,last_ordinal=EXCLUDED.last_ordinal,last_tx_hash=EXCLUDED.last_tx_hash`, addr[:], v.value.Exists, nonce, balance, v.value.CodeHash[:], v.value.StorageRoot[:], extra, int64(r.Ref.Height), int32(v.ordinal), v.txHash)
	}
	return nil
}
