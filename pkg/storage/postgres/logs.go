package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.LogReader = (*Store)(nil)
	_ port.LogWriter = (*Store)(nil)
)

// A log is identified by its block, transaction index and log index;
// indexing it again replaces it.

const logColumns = "address, topics, data, block_number, block_hash, tx_hash, tx_index, log_index, removed"

// chainOrder sorts logs as the chain emitted them.
const chainOrder = " ORDER BY block_number, tx_index, log_index"

// IndexLogs implements port.LogWriter.
func (s *Store) IndexLogs(ctx context.Context, logs []*model.Log) error {
	for _, l := range logs {
		if l == nil {
			return fmt.Errorf("log cannot be nil")
		}
	}
	if len(logs) == 0 {
		return nil
	}
	return s.inTx(ctx, func(q querier) error {
		batch := &pgx.Batch{}
		for _, l := range logs {
			queueLog(batch, l)
		}
		return sendBatch(ctx, q, batch)
	})
}

// IndexLog implements port.LogWriter.
func (s *Store) IndexLog(ctx context.Context, l *model.Log) error {
	return s.IndexLogs(ctx, []*model.Log{l})
}

func queueLog(b *pgx.Batch, l *model.Log) {
	topic := func(i int) []byte {
		if i < len(l.Topics) {
			return l.Topics[i].Bytes()
		}
		return nil
	}
	topics := make([]byte, 0, 32*len(l.Topics))
	for _, t := range l.Topics {
		topics = append(topics, t.Bytes()...)
	}
	data := l.Data
	if data == nil {
		data = []byte{}
	}
	b.Queue(`INSERT INTO logs (block_number, tx_index, log_index, block_hash, tx_hash, address,
			topic0, topic1, topic2, topic3, topics, data, removed)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (block_number, tx_index, log_index) DO UPDATE SET block_hash = EXCLUDED.block_hash,
			tx_hash = EXCLUDED.tx_hash, address = EXCLUDED.address, topic0 = EXCLUDED.topic0,
			topic1 = EXCLUDED.topic1, topic2 = EXCLUDED.topic2, topic3 = EXCLUDED.topic3,
			topics = EXCLUDED.topics, data = EXCLUDED.data, removed = EXCLUDED.removed`,
		i64(l.BlockNumber), int64(l.TxIndex), int64(l.Index), l.BlockHash.Bytes(), l.TxHash.Bytes(), l.Address.Bytes(),
		topic(0), topic(1), topic(2), topic(3), topics, data, l.Removed)
}

// GetLogsByBlock implements port.LogReader.
func (s *Store) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*model.Log, error) {
	return s.queryLogs(ctx, "SELECT "+logColumns+" FROM logs WHERE block_number = $1"+chainOrder, i64(blockNumber))
}

// GetLogsByAddress implements port.LogReader.
func (s *Store) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*model.Log, error) {
	return s.queryLogs(ctx, "SELECT "+logColumns+" FROM logs WHERE address = $1 AND block_number BETWEEN $2 AND $3"+chainOrder,
		address.Bytes(), i64(fromBlock), i64(toBlock))
}

// GetLogsByTopic implements port.LogReader.
func (s *Store) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*model.Log, error) {
	if topicIndex < 0 || topicIndex > 3 {
		return nil, fmt.Errorf("topic index %d out of range [0, 3]", topicIndex)
	}
	col := "topic" + strconv.Itoa(topicIndex)
	return s.queryLogs(ctx, "SELECT "+logColumns+" FROM logs WHERE "+col+" = $1 AND block_number BETWEEN $2 AND $3"+chainOrder,
		topic.Bytes(), i64(fromBlock), i64(toBlock))
}

// GetLogs implements port.LogReader: ToBlock 0 means the latest height,
// and logs above the latest height are not returned.
func (s *Store) GetLogs(ctx context.Context, filter *port.LogFilter) ([]*model.Log, error) {
	if filter == nil {
		return nil, fmt.Errorf("filter cannot be nil")
	}
	if filter.ToBlock != 0 && filter.FromBlock > filter.ToBlock {
		return nil, fmt.Errorf("invalid block range: from %d > to %d", filter.FromBlock, filter.ToBlock)
	}
	latest, err := s.GetLatestHeight(ctx)
	if errors.Is(err, port.ErrNotFound) {
		return nil, nil // nothing indexed
	}
	if err != nil {
		return nil, err
	}
	to := filter.ToBlock
	if to == 0 || to > latest {
		to = latest
	}
	if filter.FromBlock > to {
		return nil, nil
	}

	where := []string{"block_number BETWEEN $1 AND $2"}
	args := []any{i64(filter.FromBlock), i64(to)}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if len(filter.Addresses) > 0 {
		addrs := make([][]byte, len(filter.Addresses))
		for i, a := range filter.Addresses {
			addrs[i] = a.Bytes()
		}
		where = append(where, "address = ANY("+arg(addrs)+")")
	}
	for i, options := range filter.Topics {
		if len(options) == 0 {
			continue
		}
		if i > 3 {
			return nil, nil // no log has a topic at this position
		}
		opts := make([][]byte, len(options))
		for j, o := range options {
			opts[j] = o.Bytes()
		}
		where = append(where, "topic"+strconv.Itoa(i)+" = ANY("+arg(opts)+")")
	}
	return s.queryLogs(ctx, "SELECT "+logColumns+" FROM logs WHERE "+strings.Join(where, " AND ")+chainOrder, args...)
}

func (s *Store) queryLogs(ctx context.Context, sql string, args ...any) ([]*model.Log, error) {
	rows, err := s.q(ctx).Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanLog)
}

func scanLog(row pgx.CollectableRow) (*model.Log, error) {
	var (
		address, topics, data, blockHash, txHash []byte
		block                                    int64
		txIndex, logIndex                        int32
		removed                                  bool
	)
	if err := row.Scan(&address, &topics, &data, &block, &blockHash, &txHash, &txIndex, &logIndex, &removed); err != nil {
		return nil, err
	}
	if len(topics)%32 != 0 {
		return nil, fmt.Errorf("log %d/%d/%d: topics of %d bytes", block, txIndex, logIndex, len(topics))
	}
	l := &model.Log{
		Address: common.BytesToAddress(address), Topics: make([]common.Hash, 0, len(topics)/32), Data: data,
		BlockNumber: uint64(block), BlockHash: common.BytesToHash(blockHash), TxHash: common.BytesToHash(txHash),
		TxIndex: uint(txIndex), Index: uint(logIndex), Removed: removed,
	}
	for i := 0; i < len(topics); i += 32 {
		l.Topics = append(l.Topics, common.BytesToHash(topics[i:i+32]))
	}
	return l, nil
}
