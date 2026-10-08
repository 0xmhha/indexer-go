package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/history"
)

var (
	_ port.HistoricalReader     = (*Store)(nil)
	_ port.HistoricalWriter     = (*Store)(nil)
	_ port.BalanceRecordChecker = (*Store)(nil)
)

// The statistics come from pkg/storage/history, shared with the Pebble
// store; the time queries read the blocks table by its time index, and
// balances have tables of their own.

// bigintOf bounds a uint64 range end to the bigint PostgreSQL stores.
func bigintOf(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// defaultPage gives a page with no limit the Pebble store's default of
// these lists.
func defaultPage(page port.Page) port.Page {
	if page.Limit <= 0 {
		page.Limit = constants.DefaultPaginationLimit
	}
	return page
}

// ========== Time index ==========

// SetBlockTimestamp implements port.HistoricalWriter. The blocks table is
// indexed by time, so a stored block is found by its time without an entry
// of its own; an entry for a block that is not stored would only be
// skipped, so nothing is written.
func (s *Store) SetBlockTimestamp(ctx context.Context, timestamp, height uint64) error {
	return s.write()
}

// blockAtTime is a block with its position in the time index.
type blockAtTime struct {
	block        *model.Block
	time, number int64
}

func scanBlockAtTime(row pgx.CollectableRow) (blockAtTime, error) {
	var (
		data []byte
		b    blockAtTime
		err  error
	)
	if err = row.Scan(&data, &b.time, &b.number); err != nil {
		return b, err
	}
	b.block, err = model.DecodeBlock(data)
	return b, err
}

// GetBlocksByTimeRange implements port.HistoricalReader: in time order,
// then by height.
func (s *Store) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, page port.Page) ([]*model.Block, string, error) {
	if fromTime > toTime {
		return nil, "", fmt.Errorf("fromTime (%d) cannot be greater than toTime (%d)", fromTime, toTime)
	}
	items, next, err := listQuery[blockAtTime]{
		list: "blocks-time:" + u64s(fromTime) + "-" + u64s(toTime),
		sql:  "SELECT data, time, number FROM blocks WHERE time >= $1 AND time <= $2",
		args: []any{bigintOf(fromTime), bigintOf(toTime)},
		keys: []keyCol{{"time", kindInt, false}, {"number", kindInt, false}},
		scan: scanBlockAtTime,
		keyOf: func(b blockAtTime) []string {
			return []string{strconv.FormatInt(b.time, 10), strconv.FormatInt(b.number, 10)}
		},
	}.run(ctx, s.q(ctx), defaultPage(page))
	if err != nil {
		return nil, "", err
	}
	out := make([]*model.Block, len(items))
	for i, it := range items {
		out[i] = it.block
	}
	return out, next, nil
}

// GetBlockByTimestamp implements port.HistoricalReader.
func (s *Store) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*model.Block, error) {
	b, err := s.blockWhere(ctx, "time >= $1 ORDER BY time, number", bigintOf(timestamp))
	if errors.Is(err, port.ErrNotFound) {
		// Every block is earlier: the last one.
		return s.blockWhere(ctx, "time <= $1 ORDER BY time DESC, number DESC", bigintOf(timestamp))
	}
	return b, err
}

// GetNetworkMetrics implements port.HistoricalReader.
func (s *Store) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*port.NetworkMetrics, error) {
	if fromTime > toTime {
		return nil, fmt.Errorf("fromTime (%d) cannot be greater than toTime (%d)", fromTime, toTime)
	}
	return history.NetworkMetrics(ctx, fromTime, toTime, func(yield func(*model.Block) error) error {
		rows, err := s.q(ctx).Query(ctx, "SELECT data FROM blocks WHERE time >= $1 AND time <= $2 ORDER BY time, number",
			bigintOf(fromTime), bigintOf(toTime))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var data []byte
			if err := rows.Scan(&data); err != nil {
				return err
			}
			b, err := model.DecodeBlock(data)
			if err != nil {
				return err
			}
			if err := yield(b); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

// ========== Address transactions ==========

// addressScanBatch is how many address list entries a filtered read loads
// at a time.
const addressScanBatch = 256

// eachAddressTransaction calls fn with the entries of addr's transaction
// list after id afterID, in list order, until fn returns false. Entries are
// read in full batches, so fn may use the store.
func (s *Store) eachAddressTransaction(ctx context.Context, addr common.Address, afterID int64, fn func(id int64, txHash common.Hash) (bool, error)) error {
	for {
		rows, err := s.q(ctx).Query(ctx, "SELECT id, tx_hash FROM address_transactions WHERE address = $1 AND id > $2 ORDER BY id LIMIT $3",
			addr.Bytes(), afterID, addressScanBatch)
		if err != nil {
			return err
		}
		type entry struct {
			ID     int64
			TxHash []byte
		}
		entries, err := pgx.CollectRows(rows, pgx.RowToStructByPos[entry])
		if err != nil {
			return err
		}
		for _, e := range entries {
			more, err := fn(e.ID, common.BytesToHash(e.TxHash))
			if err != nil || !more {
				return err
			}
			afterID = e.ID
		}
		if len(entries) < addressScanBatch {
			return nil
		}
	}
}

// GetTransactionsByAddressFiltered implements port.HistoricalReader: the
// filter is applied while reading the list, Offset counts matches, and the
// cursor continues after the last match.
func (s *Store) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *port.TransactionFilter, page port.Page) ([]*port.TransactionWithReceipt, string, error) {
	if filter == nil {
		filter = port.DefaultTransactionFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, "", fmt.Errorf("invalid filter: %w", err)
	}
	list := "addrtx-filtered:" + addr.Hex()
	after, err := decodeCursor(list, page.After, 1)
	if err != nil {
		return nil, "", err
	}
	var afterID int64
	skip := 0
	if after != nil {
		if afterID, err = strconv.ParseInt(after[0], 10, 64); err != nil {
			return nil, "", port.ErrInvalidCursor
		}
	} else if page.Offset > 0 {
		skip = page.Offset
	}
	limit := defaultPage(page).Limit

	var (
		out []*port.TransactionWithReceipt
		ids []int64
	)
	err = s.eachAddressTransaction(ctx, addr, afterID, func(id int64, txHash common.Hash) (bool, error) {
		m, ok, err := history.AddressTransaction(ctx, s, addr, filter, txHash)
		if err != nil || !ok {
			return true, err
		}
		if skip > 0 {
			skip--
			return true, nil
		}
		out, ids = append(out, m), append(ids, id)
		return len(out) <= limit, nil // one more than the page: a next page exists
	})
	if err != nil {
		return nil, "", err
	}
	if len(out) <= limit {
		return out, "", nil
	}
	return out[:limit], encodeCursor(list, strconv.FormatInt(ids[limit-1], 10)), nil
}

// GetAddressStats implements port.HistoricalReader.
func (s *Store) GetAddressStats(ctx context.Context, addr common.Address) (*port.AddressStats, error) {
	return history.AddressStats(ctx, s, addr, func(yield func(common.Hash) error) error {
		return s.eachAddressTransaction(ctx, addr, 0, func(_ int64, txHash common.Hash) (bool, error) {
			return true, yield(txHash)
		})
	})
}

// ========== Balances ==========

// GetAddressBalance implements port.HistoricalReader: at blockNumber, the
// last snapshot before the first one past it (0 is the latest balance);
// zero when none was recorded, unless the genesis lookup finds the
// account's allocation (SetGenesisBalanceResolver).
func (s *Store) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	var (
		text string
		err  error
	)
	if blockNumber == 0 {
		err = s.q(ctx).QueryRow(ctx, "SELECT balance::text FROM balances WHERE address = $1", addr.Bytes()).Scan(&text)
	} else {
		err = s.q(ctx).QueryRow(ctx, `SELECT balance::text FROM balance_history
			WHERE address = $1 AND id < COALESCE(
				(SELECT min(id) FROM balance_history WHERE address = $1 AND block_number > $2), 9223372036854775807)
			ORDER BY id DESC LIMIT 1`, addr.Bytes(), bigintOf(blockNumber)).Scan(&text)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return s.genesisBalance(ctx, addr, blockNumber, new(big.Int)), nil
	}
	if err != nil {
		return nil, err
	}
	balance, err := bigOf(text)
	if err != nil || balance.Sign() != 0 {
		return balance, err
	}
	return s.genesisBalance(ctx, addr, blockNumber, balance), nil
}

// HasBalanceRecord implements port.BalanceRecordChecker.
func (s *Store) HasBalanceRecord(ctx context.Context, addr common.Address) (bool, error) {
	return s.exists(ctx, "SELECT EXISTS (SELECT 1 FROM balances WHERE address = $1)", addr.Bytes())
}

// GetBalanceHistory implements port.HistoricalReader: in write order.
func (s *Store) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, page port.Page) ([]port.BalanceSnapshot, string, error) {
	if fromBlock > toBlock {
		return nil, "", fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}
	type entry struct {
		id   int64
		snap port.BalanceSnapshot
	}
	entries, next, err := listQuery[entry]{
		list: "balances:" + addr.Hex(),
		sql: `SELECT id, block_number, balance::text, delta::text, tx_hash FROM balance_history
			WHERE address = $1 AND block_number >= $2 AND block_number <= $3`,
		args: []any{addr.Bytes(), bigintOf(fromBlock), bigintOf(toBlock)},
		keys: []keyCol{{"id", kindInt, false}},
		scan: func(row pgx.CollectableRow) (entry, error) {
			var (
				e              entry
				block          int64
				balance, delta string
				tx             []byte
			)
			if err := row.Scan(&e.id, &block, &balance, &delta, &tx); err != nil {
				return e, err
			}
			b, err := bigOf(balance)
			if err != nil {
				return e, err
			}
			d, err := bigOf(delta)
			if err != nil {
				return e, err
			}
			e.snap = port.BalanceSnapshot{BlockNumber: uint64(block), Balance: b, Delta: d, TxHash: common.BytesToHash(tx)}
			return e, nil
		},
		keyOf: func(e entry) []string { return []string{strconv.FormatInt(e.id, 10)} },
	}.run(ctx, s.q(ctx), defaultPage(page))
	if err != nil {
		return nil, "", err
	}
	out := make([]port.BalanceSnapshot, len(entries))
	for i, e := range entries {
		out[i] = e.snap
	}
	return out, next, nil
}

// UpdateBalance implements port.HistoricalWriter: a delta that would make
// the balance negative is rejected (port.ErrNegativeBalance) and changes
// nothing.
func (s *Store) UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error {
	return s.inTx(ctx, func(q querier) error {
		current, err := latestBalance(ctx, q, addr)
		if err != nil {
			return err
		}
		return addSnapshot(ctx, q, addr, blockNumber, current, delta, txHash)
	})
}

// SetBalance implements port.HistoricalWriter: a snapshot whose delta is
// the change from the latest balance.
func (s *Store) SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error {
	return s.inTx(ctx, func(q querier) error {
		current, err := latestBalance(ctx, q, addr)
		if err != nil {
			return err
		}
		return addSnapshot(ctx, q, addr, blockNumber, current, new(big.Int).Sub(balance, current), common.Hash{})
	})
}

// latestBalance reads the latest balance of addr for an update, locking its
// row; zero when none was recorded. An account's first balance has no row
// to lock, so balances assume one writer per database (the ingest process,
// R4-1), as the Pebble store does.
func latestBalance(ctx context.Context, q querier, addr common.Address) (*big.Int, error) {
	var text string
	err := q.QueryRow(ctx, "SELECT balance::text FROM balances WHERE address = $1 FOR UPDATE", addr.Bytes()).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return new(big.Int), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get current balance: %w", err)
	}
	return bigOf(text)
}

// addSnapshot records the balance current+delta at blockNumber.
func addSnapshot(ctx context.Context, q querier, addr common.Address, blockNumber uint64, current, delta *big.Int, txHash common.Hash) error {
	balance := new(big.Int).Add(current, delta)
	if balance.Sign() < 0 {
		return fmt.Errorf("%w: %s at block %d (%s %+d)", port.ErrNegativeBalance, addr.Hex(), blockNumber, current, delta)
	}
	if _, err := q.Exec(ctx, `INSERT INTO balance_history (address, block_number, balance, delta, tx_hash)
		VALUES ($1, $2, $3::numeric, $4::numeric, $5)`, addr.Bytes(), i64(blockNumber), balance.String(), delta.String(), txHash.Bytes()); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `INSERT INTO balances (address, balance) VALUES ($1, $2::numeric)
		ON CONFLICT (address) DO UPDATE SET balance = EXCLUDED.balance`, addr.Bytes(), balance.String())
	return err
}

// ========== Counts and statistics ==========

// GetBlockCount implements port.HistoricalReader: heights 0 to the latest.
func (s *Store) GetBlockCount(ctx context.Context) (uint64, error) {
	h, err := s.GetLatestHeight(ctx)
	if errors.Is(err, port.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return h + 1, nil
}

// GetTransactionCount implements port.HistoricalReader: the count SetBlock
// keeps, so the table is not scanned.
func (s *Store) GetTransactionCount(ctx context.Context) (uint64, error) {
	n, err := s.metaUint(ctx, metaTransactionCount)
	if errors.Is(err, port.ErrNotFound) {
		return 0, nil
	}
	return n, err
}

// GetTopMiners implements port.HistoricalReader.
func (s *Store) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.MinerStats, error) {
	return history.TopMiners(ctx, s, limit, fromBlock, toBlock)
}

// GetTokenBalances implements port.HistoricalReader (history.DescribeToken
// names the tokens).
func (s *Store) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]port.TokenBalance, error) {
	return history.TokenBalances(ctx, s, addr, tokenType, s.describeToken)
}

// GetGasStatsByBlockRange implements port.HistoricalReader.
func (s *Store) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*port.GasStats, error) {
	return history.GasStatsByBlockRange(ctx, s, fromBlock, toBlock)
}

// GetGasStatsByAddress implements port.HistoricalReader.
func (s *Store) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*port.AddressGasStats, error) {
	return history.GasStatsByAddress(ctx, s, addr, fromBlock, toBlock)
}

// GetTopAddressesByGasUsed implements port.HistoricalReader.
func (s *Store) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressGasStats, error) {
	return history.TopAddressesByGasUsed(ctx, s, limit, fromBlock, toBlock)
}

// GetTopAddressesByTxCount implements port.HistoricalReader.
func (s *Store) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressActivityStats, error) {
	return history.TopAddressesByTxCount(ctx, s, limit, fromBlock, toBlock)
}
