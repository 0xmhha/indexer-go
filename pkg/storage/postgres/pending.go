package postgres

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Writes of a block transaction whose results nobody reads are queued and
// sent together (one round trip for many statements) instead of one round
// trip each: a block of 50 transactions otherwise costs hundreds of round
// trips, which dominate ingest when the database is across a network
// (open-items.md, PostgreSQL write performance). The queue is sent, in
// order, before any other statement of the transaction runs and before it
// commits, so the transaction always reads its own writes. A queued
// statement that fails fails the statement or the commit that sent it.

// pendingTx is a block transaction seen through its queue: every statement
// sends the queued writes first.
type pendingTx struct{ t *blockTx }

// q returns the block transaction as a querier that sends queued writes
// before each statement.
func (t *blockTx) q() querier { return pendingTx{t} }

// flush sends the queued writes, ending with one upsert per balance
// changed since the last flush.
func (t *blockTx) flush(ctx context.Context) error {
	t.queueDirtyBalances()
	if t.pending == nil || t.pending.Len() == 0 {
		return nil
	}
	b := t.pending
	t.pending = nil
	res := t.tx.SendBatch(ctx, b)
	for i := 0; i < b.Len(); i++ {
		if _, err := res.Exec(); err != nil {
			_ = res.Close()
			return fmt.Errorf("postgres: queued write: %w", err)
		}
	}
	return res.Close()
}

// queue adds a write to be sent with the next statement or the commit.
func (t *blockTx) queue(sql string, args ...any) {
	if t.pending == nil {
		t.pending = &pgx.Batch{}
	}
	t.pending.Queue(sql, args...)
}

func (p pendingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := p.t.flush(ctx); err != nil {
		return pgconn.CommandTag{}, err
	}
	return (transactionQuery{p.t.tx}).Exec(ctx, sql, args...)
}

func (p pendingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if err := p.t.flush(ctx); err != nil {
		return nil, err
	}
	return (transactionQuery{p.t.tx}).Query(ctx, sql, args...)
}

func (p pendingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if err := p.t.flush(ctx); err != nil {
		return errRow{err}
	}
	return (transactionQuery{p.t.tx}).QueryRow(ctx, sql, args...)
}

// errRow is a row whose Scan fails with err.
type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

// execLater runs a write whose result is not read: queued in the block
// transaction bound to ctx, else run now.
func (s *Store) execLater(ctx context.Context, sql string, args ...any) error {
	if s.executionReadBound(ctx) {
		return port.ErrReadOnly
	}
	if bt := s.boundTx(ctx); bt != nil {
		bt.queue(sql, args...)
		return nil
	}
	_, err := s.pool.Exec(ctx, sql, args...)
	return err
}

var _ port.BalancePrefetcher = (*Store)(nil)

// PrefetchBalances implements port.BalancePrefetcher: one query reads and
// locks the balance rows of the accounts the block transaction has not
// read yet, and the transaction keeps them (balance, recorded).
func (s *Store) PrefetchBalances(ctx context.Context, addrs []common.Address) error {
	bt := s.boundTx(ctx)
	if bt == nil {
		return nil
	}
	var want [][]byte
	for _, a := range addrs {
		if _, ok := bt.recorded[a]; !ok {
			want = append(want, a.Bytes())
		}
	}
	if len(want) == 0 {
		return nil
	}
	rows, err := bt.q().Query(ctx, `SELECT address, balance::text FROM balances WHERE address = ANY($1)
		ORDER BY address FOR UPDATE`, want)
	if err != nil {
		return fmt.Errorf("prefetch balances: %w", err)
	}
	found := map[common.Address]*big.Int{}
	for rows.Next() {
		var (
			raw  []byte
			text string
		)
		if err := rows.Scan(&raw, &text); err != nil {
			rows.Close()
			return err
		}
		b, err := bigOf(text)
		if err != nil {
			rows.Close()
			return err
		}
		found[common.BytesToAddress(raw)] = b
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if bt.balances == nil {
		bt.balances = map[common.Address]*big.Int{}
	}
	if bt.recorded == nil {
		bt.recorded = map[common.Address]bool{}
	}
	for _, raw := range want {
		a := common.BytesToAddress(raw)
		b, ok := found[a]
		if !ok {
			b = new(big.Int)
		}
		bt.balances[a] = b
		bt.recorded[a] = ok
	}
	return nil
}

// balance returns an account's latest balance in the block transaction:
// read (and its row locked) the first time, then kept as the transaction
// changes it, so each further update of the account costs no round trip.
func (t *blockTx) balance(ctx context.Context, addr common.Address) (*big.Int, error) {
	if b, ok := t.balances[addr]; ok {
		return new(big.Int).Set(b), nil
	}
	b, err := latestBalance(ctx, t.q(), addr)
	if err != nil {
		return nil, err
	}
	if t.balances == nil {
		t.balances = map[common.Address]*big.Int{}
	}
	t.balances[addr] = new(big.Int).Set(b)
	return b, nil
}

// addSnapshot queues the balance current+delta at blockNumber.
func (t *blockTx) addSnapshot(addr common.Address, blockNumber uint64, current, delta *big.Int, txHash common.Hash) error {
	balance := new(big.Int).Add(current, delta)
	if balance.Sign() < 0 {
		return fmt.Errorf("%w: %s at block %d (%s %+d)", port.ErrNegativeBalance, addr.Hex(), blockNumber, current, delta)
	}
	t.queue(`INSERT INTO balance_history (address, block_number, balance, delta, tx_hash)
		VALUES ($1, $2, $3::numeric, $4::numeric, $5)`, addr.Bytes(), i64(blockNumber), balance.String(), delta.String(), txHash.Bytes())
	t.balances[addr] = balance
	if t.recorded != nil {
		t.recorded[addr] = true
	}
	if t.dirty == nil {
		t.dirty = map[common.Address]bool{}
	}
	t.dirty[addr] = true
	return nil
}

// queueDirtyBalances queues one upsert per account whose balance changed
// since the last flush, in address order: a block that changes an account
// many times writes its row once.
func (t *blockTx) queueDirtyBalances() {
	if len(t.dirty) == 0 {
		return
	}
	addrs := make([]common.Address, 0, len(t.dirty))
	for a := range t.dirty {
		addrs = append(addrs, a)
	}
	sort.Slice(addrs, func(i, j int) bool { return bytes.Compare(addrs[i][:], addrs[j][:]) < 0 })
	for _, a := range addrs {
		t.queue(`INSERT INTO balances (address, balance) VALUES ($1, $2::numeric)
			ON CONFLICT (address) DO UPDATE SET balance = EXCLUDED.balance`, a.Bytes(), t.balances[a].String())
	}
	t.dirty = nil
}
