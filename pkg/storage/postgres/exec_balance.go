package postgres

import (
	"context"
	"fmt"
	"math/big"
	"strconv"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

var _ port.ExecutionBalanceJournal = (*Store)(nil)

// ValidateExecutionBalances accepts only a complete, exact journal inside the
// candidate publication transaction. Seven part proofs are checked by its owner.
func (s *Store) ValidateExecutionBalances(ctx context.Context, height uint64, hash common.Hash, changes []port.ExecutionBalanceChange) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.executionReadBound(ctx) {
		return port.ErrReadOnly
	}
	if s.boundTx(ctx) == nil || !s.executionMode.Load() {
		return port.ErrClosed
	}
	point, err := s.executionStatePoint(ctx)
	if err != nil {
		return err
	}
	if point.Ref.Height != height || common.Hash(point.Ref.Hash) != hash {
		return port.ErrExecutionConflict
	}
	rows, err := s.q(ctx).Query(ctx, `SELECT address,ordinal,balance_before::text,balance_after::text,tx_hash
 FROM state_account_changes WHERE height=$1 AND block_hash=$2 AND generation=$3 AND NOT baseline ORDER BY ordinal,address`, bigintOf(height), hash.Bytes(), int64(point.Ref.Generation))
	if err != nil {
		return err
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var address, tx []byte
		var ordinal int32
		var before, after string
		if err = rows.Scan(&address, &ordinal, &before, &after, &tx); err != nil {
			return err
		}
		if i >= len(changes) {
			return port.ErrExecutionConflict
		}
		c := changes[i]
		if c.Before == nil || c.After == nil || c.Before.Sign() < 0 || c.After.Sign() < 0 || c.Before.BitLen() > 256 || c.After.BitLen() > 256 {
			return port.ErrInvalidData
		}
		if common.BytesToAddress(address) != c.Address || uint32(ordinal) != c.Ordinal || common.BytesToHash(tx) != c.TxHash || c.Before.String() != before || c.After.String() != after {
			return port.ErrExecutionConflict
		}
		i++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if i != len(changes) {
		return port.ErrExecutionConflict
	}
	return ctx.Err()
}

// Execution balances have one authority. Even a stale/poisoned legacy projection
// cannot override the as-of-visible journal. No genesis RPC fallback is used.
func (s *Store) executionBalance(ctx context.Context, addr common.Address, height uint64) (balance *big.Int, err error) {
	err = s.ReadSnapshot(ctx, func(ctx context.Context) error {
		point, e := s.executionStatePoint(ctx)
		if e != nil {
			return e
		}
		if height == 0 {
			height = point.Ref.Height
		}
		if height > point.Ref.Height {
			return port.ErrExecutionCoverage
		}
		account, e := s.ExecutionAccountAt(ctx, execwire.Address(addr), height)
		if e != nil {
			return e
		}
		balance = new(big.Int).SetBytes(account.Balance[:])
		return nil
	})
	return balance, err
}

func (s *Store) executionBalanceHistory(ctx context.Context, addr common.Address, from, to uint64, page port.Page) (out []port.BalanceSnapshot, next string, err error) {
	err = s.ReadSnapshot(ctx, func(ctx context.Context) error {
		point, e := s.executionStatePoint(ctx)
		if e != nil {
			return e
		}
		control, e := s.ExecutionStatus(ctx)
		if e != nil {
			return e
		}
		if from < control.Anchor.Height {
			return port.ErrExecutionCoverage
		}
		if _, e = s.ExecutionAccountAt(ctx, execwire.Address(addr), point.Ref.Height); e != nil {
			return e
		}
		type entry struct {
			height, ordinal int64
			snap            port.BalanceSnapshot
		}
		entries, cursor, e := (listQuery[entry]{
			list: fmt.Sprintf("execution-balances-v1:%s:%d:%d:%d", addr.Hex(), from, to, point.Ref.Generation),
			sql: `SELECT height,ordinal,balance_before::text,balance_after::text,tx_hash,baseline FROM state_account_changes
    WHERE address=$1 AND height>=$2 AND height<=$3 AND height<=$4 AND (baseline OR balance_before<>balance_after)`,
			args: []any{addr.Bytes(), bigintOf(from), bigintOf(to), int64(point.Ref.Height)}, keys: []keyCol{{"height", kindInt, false}, {"ordinal", kindInt, false}},
			scan: func(row pgx.CollectableRow) (entry, error) {
				var v entry
				var before, after string
				var tx []byte
				var baseline bool
				if e := row.Scan(&v.height, &v.ordinal, &before, &after, &tx, &baseline); e != nil {
					return v, e
				}
				b, e := bigOf(before)
				if e != nil {
					return v, e
				}
				a, e := bigOf(after)
				if e != nil {
					return v, e
				}
				delta := new(big.Int).Sub(a, b)
				if baseline {
					delta.SetInt64(0)
				}
				v.snap = port.BalanceSnapshot{BlockNumber: uint64(v.height), Balance: a, Delta: delta, TxHash: common.BytesToHash(tx)}
				return v, nil
			}, keyOf: func(v entry) []string {
				return []string{strconv.FormatInt(v.height, 10), strconv.FormatInt(v.ordinal, 10)}
			},
		}).run(ctx, s.q(ctx), defaultPage(page))
		if e != nil {
			return e
		}
		out = make([]port.BalanceSnapshot, len(entries))
		for i, v := range entries {
			out[i] = v.snap
		}
		next = cursor
		return nil
	})
	return
}
