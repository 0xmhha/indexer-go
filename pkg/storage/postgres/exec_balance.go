package postgres

import (
	"context"
	"errors"
	"math/big"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

// executionBalance keeps the feature projection's Before value inside a
// publication transaction. Untouched accounts fall back to verified covered
// state, never a genesis RPC or an invented zero. API calls share one snapshot.
func (s *Store) executionBalance(ctx context.Context, addr common.Address, height uint64) (balance *big.Int, err error) {
	err = s.ReadSnapshot(ctx, func(ctx context.Context) error {
		point, err := s.executionStatePoint(ctx)
		if err != nil {
			return err
		}
		if height > point.Ref.Height {
			return port.ErrExecutionCoverage
		}
		var text string
		if height == 0 {
			err = s.q(ctx).QueryRow(ctx, "SELECT balance::text FROM balances WHERE address=$1", addr.Bytes()).Scan(&text)
		} else {
			err = s.q(ctx).QueryRow(ctx, `SELECT balance::text FROM balance_history
				WHERE address=$1 AND id < COALESCE(
				(SELECT min(id) FROM balance_history WHERE address=$1 AND block_number>$2),9223372036854775807)
				ORDER BY id DESC LIMIT 1`, addr.Bytes(), bigintOf(height)).Scan(&text)
		}
		if err == nil {
			balance, err = bigOf(text)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if height == 0 {
			height = point.Ref.Height
		}
		account, err := s.ExecutionAccountAt(ctx, execwire.Address(addr), height)
		if err != nil {
			return err
		}
		balance = new(big.Int).SetBytes(account.Balance[:])
		return nil
	})
	return balance, err
}
