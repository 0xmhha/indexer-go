package postgres

import (
	"context"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/core/types"
)

func (s *Store) rewindExecutionState(ctx context.Context, height uint64) error {
	q := s.q(ctx)
	// Preserve unknown coverage rather than manufacturing a new baseline from a
	// current row. Existing before-images require a surviving history prefix.

	checks := []struct {
		sql  string
		args []any
	}{
		{`SELECT EXISTS(SELECT 1 FROM (SELECT DISTINCT ON(address) address,existed_before FROM state_account_changes WHERE height>$1 ORDER BY address,height,ordinal) f WHERE f.existed_before AND NOT EXISTS(SELECT 1 FROM state_account_changes h WHERE h.address=f.address AND h.height<=$1)) OR EXISTS(SELECT 1 FROM state_accounts a WHERE first_height<=$1 AND last_height>$1 AND NOT EXISTS(SELECT 1 FROM state_account_changes h WHERE h.address=a.address AND h.height<=$1))`, []any{int64(height)}},
		{`SELECT EXISTS(SELECT 1 FROM (SELECT DISTINCT ON(address) address,existed_before FROM state_contract_changes WHERE height>$1 ORDER BY address,height,ordinal) f WHERE f.existed_before AND NOT EXISTS(SELECT 1 FROM state_contract_changes h WHERE h.address=f.address AND h.height<=$1)) OR EXISTS(SELECT 1 FROM state_contracts a WHERE first_height<=$1 AND last_height>$1 AND NOT EXISTS(SELECT 1 FROM state_contract_changes h WHERE h.address=a.address AND h.height<=$1))`, []any{int64(height)}},
		{`SELECT EXISTS(SELECT 1 FROM
 (SELECT DISTINCT ON(address,slot) address,slot,value_before FROM state_storage_changes WHERE height>$1 ORDER BY address,slot,height,ordinal) f
 WHERE NOT EXISTS(SELECT 1 FROM state_storage_changes h WHERE h.address=f.address AND h.slot=f.slot AND h.height<=$1)
 AND (f.value_before<>decode(repeat('00',32),'hex') OR (
 EXISTS(SELECT 1 FROM (SELECT exists_after,storage_root_after FROM state_account_changes a WHERE a.address=f.address AND a.height<=$1 ORDER BY height DESC,ordinal DESC LIMIT 1) a WHERE a.exists_after AND a.storage_root_after<>$2)
 AND NOT EXISTS(SELECT 1 FROM state_storage_clears c WHERE c.address=f.address AND c.height<=$1))))`, []any{int64(height), types.EmptyRootHash.Bytes()}},
	}
	for _, check := range checks {
		var missing bool
		if err := q.QueryRow(ctx, check.sql, check.args...).Scan(&missing); err != nil {
			return err
		}
		if missing {
			return port.ErrExecutionCoverage
		}
	}

	for _, sql := range []string{
		`CREATE TEMP TABLE exec_rewind_keys(address bytea PRIMARY KEY) ON COMMIT DROP`,
		`CREATE TEMP TABLE exec_rewind_slots(address bytea,slot bytea,PRIMARY KEY(address,slot)) ON COMMIT DROP`,
	} {
		if _, err := q.Exec(ctx, sql); err != nil {
			return err
		}
	}
	if _, err := q.Exec(ctx, `INSERT INTO pg_temp.exec_rewind_keys(address)
 SELECT address FROM state_accounts WHERE last_height>$1
 UNION SELECT address FROM state_storage_roots WHERE last_height>$1
 UNION SELECT address FROM state_contracts WHERE last_height>$1
 UNION SELECT address FROM state_account_changes WHERE height>$1
 UNION SELECT address FROM state_storage_changes WHERE height>$1
 UNION SELECT address FROM state_storage_clears WHERE height>$1
 UNION SELECT address FROM state_contract_changes WHERE height>$1`, int64(height)); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `INSERT INTO pg_temp.exec_rewind_slots(address,slot)
 SELECT address,slot FROM state_storage WHERE last_height>$1
 UNION SELECT address,slot FROM state_storage_changes WHERE height>$1`, int64(height)); err != nil {
		return err
	}
	rows, err := q.Query(ctx, `SELECT h.code_hash_after,c.code FROM
 (SELECT DISTINCT ON(h.address) h.address,h.code_hash_after,h.active_after FROM state_contract_changes h JOIN pg_temp.exec_rewind_keys k USING(address) WHERE h.height<=$1 ORDER BY h.address,h.height DESC,h.ordinal DESC) h
 LEFT JOIN state_code c ON c.code_hash=h.code_hash_after WHERE h.active_after`, int64(height))
	if err != nil {
		return err
	}
	for rows.Next() {
		var hash, code []byte
		if err := rows.Scan(&hash, &code); err != nil {
			rows.Close()
			return err
		}
		if code == nil {
			rows.Close()
			return port.ErrExecutionCoverage
		}
		var key execwire.Hash
		copy(key[:], hash)
		if err := validExecutionCode(execwire.Code{Hash: key, Bytes: code}); err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, sql := range []string{
		`DELETE FROM state_account_changes WHERE height>$1`,
		`DELETE FROM state_storage_changes WHERE height>$1`,
		`DELETE FROM state_storage_clears WHERE height>$1`,
		`DELETE FROM state_contract_changes WHERE height>$1`,
	} {
		if _, err := q.Exec(ctx, sql, int64(height)); err != nil {
			return err
		}
	}
	for _, sql := range []string{
		`DELETE FROM state_accounts WHERE address IN(SELECT address FROM pg_temp.exec_rewind_keys)`,
		`DELETE FROM state_storage_roots WHERE address IN(SELECT address FROM pg_temp.exec_rewind_keys)`,
		`DELETE FROM state_contracts WHERE address IN(SELECT address FROM pg_temp.exec_rewind_keys)`,
		`DELETE FROM state_storage s USING pg_temp.exec_rewind_slots k WHERE s.address=k.address AND s.slot=k.slot`,
	} {
		if _, err := q.Exec(ctx, sql); err != nil {
			return err
		}
	}
	for _, sql := range []string{
		`INSERT INTO state_accounts(address,exists_now,nonce,balance,code_hash,storage_root,extra,first_height,last_height,last_ordinal,last_tx_hash)
 SELECT DISTINCT ON(h.address) h.address,h.exists_after,h.nonce_after,h.balance_after,h.code_hash_after,h.storage_root_after,h.extra_after,min(h.height) OVER(PARTITION BY h.address),h.height,h.ordinal,h.tx_hash
 FROM state_account_changes h JOIN pg_temp.exec_rewind_keys k USING(address) WHERE h.height<=$1 ORDER BY h.address,h.height DESC,h.ordinal DESC`,
		`INSERT INTO state_storage_roots(address,exists_now,root,last_height,last_ordinal)
 SELECT DISTINCT ON(h.address) h.address,h.exists_after,h.storage_root_after,h.height,h.ordinal
 FROM state_account_changes h JOIN pg_temp.exec_rewind_keys k USING(address) WHERE h.height<=$1 ORDER BY h.address,h.height DESC,h.ordinal DESC`,
		`INSERT INTO state_contracts(address,account_exists,active,code_hash,first_height,last_height,last_ordinal,created_height,created_tx_hash,creator,code_activated_height,destroyed_height)
 SELECT DISTINCT ON(h.address) h.address,h.exists_after,h.active_after,h.code_hash_after,min(h.height) OVER(PARTITION BY h.address),h.height,h.ordinal,h.created_height,h.created_tx_hash,h.creator,h.code_activated_height,h.destroyed_height
 FROM state_contract_changes h JOIN pg_temp.exec_rewind_keys k USING(address) WHERE h.height<=$1 ORDER BY h.address,h.height DESC,h.ordinal DESC`,
		`INSERT INTO state_storage(address,slot,value,last_height,last_ordinal)
 SELECT DISTINCT ON(h.address,h.slot) h.address,h.slot,h.value_after,h.height,h.ordinal
 FROM state_storage_changes h JOIN pg_temp.exec_rewind_slots k USING(address,slot) WHERE h.height<=$1 ORDER BY h.address,h.slot,h.height DESC,h.ordinal DESC`,
	} {
		if _, err := q.Exec(ctx, sql, int64(height)); err != nil {
			return err
		}
	}
	// Code blobs are content addressed and retained. E1 never GC's node state
	// or removes a blob merely because one observed activation was orphaned.
	return nil
}
