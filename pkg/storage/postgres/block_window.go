package postgres

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var _ port.LatestBlockWindowReader = (*Store)(nil)

// GetLatestBlockWindow combines the dependent tip/range reads in one statement
// snapshot and one protocol exchange. It uses the existing transaction/API
// reader selection, without reserving another connection or caching the tip.
func (s *Store) GetLatestBlockWindow(ctx context.Context, offset, limit int) (uint64, []*model.Block, error) {
	if offset < 0 || limit <= 0 {
		return 0, nil, fmt.Errorf("invalid block window: offset %d, limit %d", offset, limit)
	}
	rows, err := s.q(ctx).Query(ctx, `WITH tip AS MATERIALIZED (
		SELECT value, CASE WHEN octet_length(value) = 8
			THEN ('x' || encode(value, 'hex'))::bit(64)::bigint END AS height
		FROM meta WHERE name = $1
	), bounds AS (
		SELECT value, height, CASE WHEN height >= $2 THEN height - $2 ELSE 0 END AS hi FROM tip
	)
	SELECT bounds.value, b.data FROM bounds LEFT JOIN blocks b
		ON bounds.height >= $2 AND b.number BETWEEN greatest(0, bounds.hi - ($3::bigint - 1)) AND bounds.hi
	ORDER BY b.number`, metaLatestHeight, int64(offset), int64(limit))
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	var tip []byte
	var datas [][]byte
	for rows.Next() {
		var value, data []byte
		if err := rows.Scan(&value, &data); err != nil {
			return 0, nil, err
		}
		if len(value) != 8 {
			return 0, nil, fmt.Errorf("meta %s has %d bytes, want 8", metaLatestHeight, len(value))
		}
		tip = value
		if data != nil { // LEFT JOIN preserves a tip even for an empty window.
			datas = append(datas, data)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	if tip == nil {
		return 0, nil, port.ErrNotFound
	}
	height := binary.BigEndian.Uint64(tip)
	if height < uint64(offset) {
		return height, []*model.Block{}, nil
	}
	if height > math.MaxInt64 {
		// Preserve the existing uint64 -> SQL bigint range behavior at the
		// unusual signed boundary, rather than interpreting a negative tip
		// as an empty chain. Normal chain heights use the single statement.
		end := height - uint64(offset)
		start := uint64(0)
		if end >= uint64(limit-1) {
			start = end - uint64(limit-1)
		}
		blocks, err := s.GetBlocks(ctx, start, end)
		return height, blocks, err
	}
	if finish := observeDecode(ctx); finish != nil {
		defer finish()
	}
	blocks := make([]*model.Block, 0, len(datas))
	for _, data := range datas {
		block, err := model.DecodeBlock(data)
		if err != nil {
			return 0, nil, err
		}
		blocks = append(blocks, block)
	}
	return height, blocks, nil
}
