package postgres

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.Reader = (*Store)(nil)
	_ port.Writer = (*Store)(nil)
)

// metaLatestHeight is the meta row of the indexed-height cursor.
const metaLatestHeight = "latest_height"

// metaTransactionCount is the meta row of the number of stored
// transactions, kept by SetBlock (GetTransactionCount).
const metaTransactionCount = "transaction_count"

// SetBlock implements port.BlockWriter: the block (with its transactions)
// and every transaction with its location.
func (s *Store) SetBlock(ctx context.Context, b *model.Block) error {
	if b == nil {
		return fmt.Errorf("block cannot be nil")
	}
	data, err := model.EncodeBlock(b)
	if err != nil {
		return fmt.Errorf("encode block %d: %w", b.Number, err)
	}
	return s.inTx(ctx, func(q querier) error {
		if _, err := q.Exec(ctx, `INSERT INTO blocks (number, hash, parent_hash, time, miner, data)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (number) DO UPDATE SET hash = EXCLUDED.hash, parent_hash = EXCLUDED.parent_hash,
				time = EXCLUDED.time, miner = EXCLUDED.miner, data = EXCLUDED.data`,
			i64(b.Number), b.Hash.Bytes(), b.ParentHash.Bytes(), i64(b.Time), b.Miner.Bytes(), data); err != nil {
			return fmt.Errorf("store block %d: %w", b.Number, err)
		}
		if len(b.Transactions) == 0 {
			return nil
		}
		var (
			hashes, froms, tos, datas [][]byte
			indexes                   []int32
		)
		for i, tx := range b.Transactions {
			txData, err := model.EncodeTransaction(tx)
			if err != nil {
				return fmt.Errorf("encode transaction %d of block %d: %w", i, b.Number, err)
			}
			var to []byte
			if tx.To != nil {
				to = tx.To.Bytes()
			}
			hashes, froms, tos, datas = append(hashes, tx.Hash.Bytes()), append(froms, tx.From.Bytes()), append(tos, to), append(datas, txData)
			indexes = append(indexes, int32(i))
		}
		// One statement for the block's transactions; xmax = 0 marks the
		// rows inserted rather than updated, which the transaction count
		// adds.
		var added int64
		err := q.QueryRow(ctx, `WITH written AS (
				INSERT INTO transactions (hash, block_number, tx_index, block_hash, from_addr, to_addr, data)
				SELECT h, $2, i, $3, f, t, d FROM unnest($1::bytea[], $4::integer[], $5::bytea[], $6::bytea[], $7::bytea[]) AS x(h, i, f, t, d)
				ON CONFLICT (hash) DO UPDATE SET block_number = EXCLUDED.block_number, tx_index = EXCLUDED.tx_index,
					block_hash = EXCLUDED.block_hash, from_addr = EXCLUDED.from_addr, to_addr = EXCLUDED.to_addr, data = EXCLUDED.data
				RETURNING xmax = 0 AS inserted)
			SELECT count(*) FILTER (WHERE inserted) FROM written`,
			hashes, i64(b.Number), b.Hash.Bytes(), indexes, froms, tos, datas).Scan(&added)
		if err != nil {
			return fmt.Errorf("store transactions of block %d: %w", b.Number, err)
		}
		if added == 0 {
			return nil
		}
		_, err = q.Exec(ctx, `INSERT INTO meta (name, value) VALUES ($1, int8send($2::bigint))
			ON CONFLICT (name) DO UPDATE SET value = int8send(('x' || encode(meta.value, 'hex'))::bit(64)::bigint + $2::bigint)`, metaTransactionCount, added)
		return err
	})
}

// SetReceipt implements port.BlockWriter.
func (s *Store) SetReceipt(ctx context.Context, r *model.Receipt) error {
	if r == nil {
		return fmt.Errorf("receipt cannot be nil")
	}
	data, err := model.EncodeReceipt(r)
	if err != nil {
		return fmt.Errorf("encode receipt %s: %w", r.TxHash.Hex(), err)
	}
	var contract []byte
	if r.ContractAddress != nil {
		contract = r.ContractAddress.Bytes()
	}
	if err := s.write(); err != nil {
		return err
	}
	_, err = s.q(ctx).Exec(ctx, `INSERT INTO receipts (tx_hash, block_number, tx_index, block_hash, status, contract_address, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tx_hash) DO UPDATE SET block_number = EXCLUDED.block_number, tx_index = EXCLUDED.tx_index,
			block_hash = EXCLUDED.block_hash, status = EXCLUDED.status, contract_address = EXCLUDED.contract_address, data = EXCLUDED.data`,
		r.TxHash.Bytes(), i64(r.BlockNumber), int64(r.TxIndex), r.BlockHash.Bytes(), i64(r.Status), contract, data)
	if err != nil {
		return fmt.Errorf("store receipt %s: %w", r.TxHash.Hex(), err)
	}
	return nil
}

// GetBlock implements port.BlockReader.
func (s *Store) GetBlock(ctx context.Context, height uint64) (*model.Block, error) {
	return s.blockWhere(ctx, "number = $1", i64(height))
}

// GetBlockByHash implements port.BlockReader.
func (s *Store) GetBlockByHash(ctx context.Context, hash common.Hash) (*model.Block, error) {
	return s.blockWhere(ctx, "hash = $1", hash.Bytes())
}

func (s *Store) blockWhere(ctx context.Context, where string, arg any) (*model.Block, error) {
	var data []byte
	if err := s.q(ctx).QueryRow(ctx, "SELECT data FROM blocks WHERE "+where+" LIMIT 1", arg).Scan(&data); err != nil {
		return nil, notFound(err)
	}
	return model.DecodeBlock(data)
}

// GetBlocks implements port.BlockReader.
func (s *Store) GetBlocks(ctx context.Context, start, end uint64) ([]*model.Block, error) {
	if start > end {
		return nil, nil
	}
	rows, err := s.q(ctx).Query(ctx, "SELECT data FROM blocks WHERE number BETWEEN $1 AND $2 ORDER BY number", i64(start), i64(end))
	if err != nil {
		return nil, err
	}
	datas, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		return nil, err
	}
	out := make([]*model.Block, 0, len(datas))
	for _, d := range datas {
		b, err := model.DecodeBlock(d)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// GetTransaction implements port.BlockReader.
func (s *Store) GetTransaction(ctx context.Context, hash common.Hash) (*model.Transaction, *port.TxLocation, error) {
	var (
		data      []byte
		number    int64
		index     int32
		blockHash []byte
	)
	err := s.q(ctx).QueryRow(ctx, "SELECT data, block_number, tx_index, block_hash FROM transactions WHERE hash = $1", hash.Bytes()).
		Scan(&data, &number, &index, &blockHash)
	if err != nil {
		return nil, nil, notFound(err)
	}
	tx, err := model.DecodeTransaction(data)
	if err != nil {
		return nil, nil, err
	}
	return tx, &port.TxLocation{BlockHeight: uint64(number), TxIndex: uint64(index), BlockHash: common.BytesToHash(blockHash)}, nil
}

// GetReceipt implements port.BlockReader.
func (s *Store) GetReceipt(ctx context.Context, hash common.Hash) (*model.Receipt, error) {
	var data []byte
	if err := s.q(ctx).QueryRow(ctx, "SELECT data FROM receipts WHERE tx_hash = $1", hash.Bytes()).Scan(&data); err != nil {
		return nil, notFound(err)
	}
	return model.DecodeReceipt(data)
}

// GetLatestHeight implements port.Reader.
func (s *Store) GetLatestHeight(ctx context.Context) (uint64, error) {
	v, err := s.metaUint(ctx, metaLatestHeight)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// SetLatestHeight implements port.Writer.
func (s *Store) SetLatestHeight(ctx context.Context, height uint64) error {
	return s.setMetaUint(ctx, metaLatestHeight, height)
}

// metaUint reads an 8-byte meta value; port.ErrNotFound when absent.
func (s *Store) metaUint(ctx context.Context, name string) (uint64, error) {
	var v []byte
	if err := s.q(ctx).QueryRow(ctx, "SELECT value FROM meta WHERE name = $1", name).Scan(&v); err != nil {
		return 0, notFound(err)
	}
	if len(v) != 8 {
		return 0, fmt.Errorf("meta %s has %d bytes, want 8", name, len(v))
	}
	return binary.BigEndian.Uint64(v), nil
}

func (s *Store) setMetaUint(ctx context.Context, name string, v uint64) error {
	if err := s.write(); err != nil {
		return err
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	_, err := s.q(ctx).Exec(ctx, `INSERT INTO meta (name, value) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`, name, b[:])
	return err
}

// GetTransactions implements port.Reader.
func (s *Store) GetTransactions(ctx context.Context, hashes []common.Hash) ([]*model.Transaction, []*port.TxLocation, error) {
	txs := make([]*model.Transaction, len(hashes))
	locs := make([]*port.TxLocation, len(hashes))
	var first error
	for i, h := range hashes {
		tx, loc, err := s.GetTransaction(ctx, h)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		txs[i], locs[i] = tx, loc
	}
	return txs, locs, first
}

// GetReceipts implements port.Reader.
func (s *Store) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*model.Receipt, error) {
	out := make([]*model.Receipt, len(hashes))
	var first error
	for i, h := range hashes {
		r, err := s.GetReceipt(ctx, h)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		out[i] = r
	}
	return out, first
}

// GetReceiptsByBlockHash implements port.Reader.
func (s *Store) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*model.Receipt, error) {
	var number int64
	if err := s.q(ctx).QueryRow(ctx, "SELECT number FROM blocks WHERE hash = $1 LIMIT 1", blockHash.Bytes()).Scan(&number); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("block %s: %w", blockHash.Hex(), port.ErrNotFound)
		}
		return nil, err
	}
	return s.GetReceiptsByBlockNumber(ctx, uint64(number))
}

// GetReceiptsByBlockNumber implements port.Reader: the receipts of the
// stored block's transactions, in transaction order.
func (s *Store) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*model.Receipt, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT r.data FROM blocks b
		JOIN transactions t ON t.block_hash = b.hash AND t.block_number = b.number
		JOIN receipts r ON r.tx_hash = t.hash
		WHERE b.number = $1 ORDER BY t.tx_index`, i64(blockNumber))
	if err != nil {
		return nil, err
	}
	datas, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		return nil, err
	}
	out := make([]*model.Receipt, 0, len(datas))
	for _, d := range datas {
		r, err := model.DecodeReceipt(d)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// GetMissingReceipts implements port.Reader.
func (s *Store) GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT t.hash FROM blocks b
		JOIN transactions t ON t.block_hash = b.hash AND t.block_number = b.number
		WHERE b.number = $1 AND NOT EXISTS (SELECT 1 FROM receipts r WHERE r.tx_hash = t.hash)
		ORDER BY t.tx_index`, i64(blockNumber))
	if err != nil {
		return nil, err
	}
	return collectHashes(rows)
}

// HasBlock implements port.Reader.
func (s *Store) HasBlock(ctx context.Context, height uint64) (bool, error) {
	return s.exists(ctx, "SELECT EXISTS (SELECT 1 FROM blocks WHERE number = $1)", i64(height))
}

// HasTransaction implements port.Reader.
func (s *Store) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	return s.exists(ctx, "SELECT EXISTS (SELECT 1 FROM transactions WHERE hash = $1)", hash.Bytes())
}

// HasReceipt implements port.Reader.
func (s *Store) HasReceipt(ctx context.Context, hash common.Hash) (bool, error) {
	return s.exists(ctx, "SELECT EXISTS (SELECT 1 FROM receipts WHERE tx_hash = $1)", hash.Bytes())
}

func (s *Store) exists(ctx context.Context, sql string, args ...any) (bool, error) {
	var ok bool
	err := s.q(ctx).QueryRow(ctx, sql, args...).Scan(&ok)
	return ok, err
}

// DeleteBlock implements port.Writer: the block and its hash and time
// entries; its transactions and receipts stay, as in the Pebble store.
func (s *Store) DeleteBlock(ctx context.Context, height uint64) error {
	if err := s.write(); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, "DELETE FROM blocks WHERE number = $1", i64(height))
	return err
}

// AddTransactionToAddressIndex implements port.Writer.
func (s *Store) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	if err := s.write(); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, "INSERT INTO address_transactions (address, tx_hash) VALUES ($1, $2)", addr.Bytes(), txHash.Bytes())
	return err
}

// defaultPageLimit is the page size of lists asked for no limit.
const defaultPageLimit = 100

// GetTransactionsByAddress implements port.Reader.
func (s *Store) GetTransactionsByAddress(ctx context.Context, addr common.Address, page port.Page) ([]common.Hash, string, error) {
	return s.transactionsByAddress(ctx, addr, false, page)
}

// GetTransactionsByAddressNewestFirst implements port.AddressTransactionsNewestFirst.
func (s *Store) GetTransactionsByAddressNewestFirst(ctx context.Context, addr common.Address, page port.Page) ([]common.Hash, string, error) {
	return s.transactionsByAddress(ctx, addr, true, page)
}

func (s *Store) transactionsByAddress(ctx context.Context, addr common.Address, newestFirst bool, page port.Page) ([]common.Hash, string, error) {
	list, cmp, order := "addrtx:"+addr.Hex(), ">", "id"
	if newestFirst {
		list, cmp, order = "addrtx-desc:"+addr.Hex(), "<", "id DESC"
	}
	after, err := decodeCursor(list, page.After, 1)
	if err != nil {
		return nil, "", err
	}
	limit := pageLimit(page, false)
	where, args := "address = $1", []any{addr.Bytes()}
	if after != nil {
		id, err := strconv.ParseInt(after[0], 10, 64)
		if err != nil {
			return nil, "", port.ErrInvalidCursor
		}
		where, args = where+" AND id "+cmp+" $2", append(args, id)
	}
	rows, err := s.q(ctx).Query(ctx, "SELECT id, tx_hash FROM address_transactions WHERE "+where+" ORDER BY "+order+limitClause(page, limit), args...)
	if err != nil {
		return nil, "", err
	}
	type item struct {
		ID     int64
		TxHash []byte
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[item])
	if err != nil {
		return nil, "", err
	}
	items, more := trimPage(items, limit)
	next := ""
	if more {
		next = encodeCursor(list, strconv.FormatInt(items[len(items)-1].ID, 10))
	}
	out := make([]common.Hash, len(items))
	for i, it := range items {
		out[i] = common.BytesToHash(it.TxHash)
	}
	return out, next, nil
}

func collectHashes(rows pgx.Rows) ([]common.Hash, error) {
	raw, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		return nil, err
	}
	out := make([]common.Hash, len(raw))
	for i, b := range raw {
		out[i] = common.BytesToHash(b)
	}
	return out, nil
}

// sendBatch runs queued statements and checks each result.
func sendBatch(ctx context.Context, q querier, b *pgx.Batch) error {
	if b.Len() == 0 {
		return nil
	}
	sender, ok := q.(interface {
		SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
	})
	if !ok {
		return fmt.Errorf("postgres: %T cannot send batches", q)
	}
	res := sender.SendBatch(ctx, b)
	for i := 0; i < b.Len(); i++ {
		if _, err := res.Exec(); err != nil {
			_ = res.Close()
			return err
		}
	}
	return res.Close()
}

func itoa(v int) string { return strconv.Itoa(v) }
