package postgres

import (
	"bytes"
	"context"
	"math"
	"slices"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

var _ port.ExecutionCoreWriter = (*Store)(nil)

func validateExecutionCore(p port.ExecutionPart, b *model.Block, rs []*model.Receipt) error {
	if b == nil {
		return port.ErrInvalidData
	}
	if b.Number != p.Ref.Height || execwire.Hash(b.Hash) != p.Ref.Hash || execwire.Hash(b.ParentHash) != p.Parent {
		return port.ErrExecutionConflict
	}
	if len(b.Transactions) > 65536 || b.Time > math.MaxInt64 {
		return port.ErrInvalidData
	}
	seen := map[common.Hash]bool{}
	for i, tx := range b.Transactions {
		if tx == nil || tx.Hash == (common.Hash{}) || seen[tx.Hash] {
			return port.ErrInvalidData
		}
		seen[tx.Hash] = true
		if tx.BlockNumber != b.Number || tx.BlockHash != b.Hash || tx.Index != uint(i) {
			return port.ErrExecutionConflict
		}
	}
	if p.Writer == port.ExecutionBlocks || p.Writer == port.ExecutionTransactions {
		return nil
	}
	if len(rs) != len(b.Transactions) {
		return port.ErrExecutionConflict
	}
	next := uint(0)
	for i, r := range rs {
		if r == nil {
			return port.ErrInvalidData
		}
		if r.TxIndex != uint(i) || r.TxHash != b.Transactions[i].Hash || r.BlockHash != b.Hash || r.BlockNumber != b.Number {
			return port.ErrExecutionConflict
		}
		for _, l := range r.Logs {
			if l == nil {
				return port.ErrInvalidData
			}
			if l.BlockNumber != b.Number || l.BlockHash != b.Hash || l.TxHash != r.TxHash || l.TxIndex != uint(i) || l.Index != next || l.Removed {
				return port.ErrExecutionConflict
			}
			next++
		}
	}
	return nil
}
func (s *Store) ApplyExecutionCore(ctx context.Context, w port.ExecutionWriter, b *model.Block, rs []*model.Receipt) error {
	scope, _ := ctx.Value(executionPartKey{}).(*executionPartScope)
	if scope == nil || !scope.active.Load() || s.boundTx(ctx) == nil {
		return port.ErrClosed
	}
	if scope.part.Writer != w {
		return port.ErrExecutionConflict
	}
	if w != port.ExecutionBlocks && w != port.ExecutionTransactions && w != port.ExecutionReceipts && w != port.ExecutionLogs {
		return port.ErrInvalidData
	}
	if err := validateExecutionCore(scope.part, b, rs); err != nil {
		return err
	}
	q := s.q(ctx)
	switch w {
	case port.ExecutionBlocks:
		data, err := model.EncodeBlock(b)
		if err != nil {
			return err
		}
		return writeBlockRow(ctx, q, b, data, true)
	case port.ExecutionTransactions:
		return writeBlockTransactions(ctx, q, b, true)
	case port.ExecutionReceipts:
		return s.writeExecutionReceipts(ctx, b.Number, rs)
	case port.ExecutionLogs:
		return s.writeExecutionLogs(ctx, b.Number, rs)
	}
	return port.ErrInvalidData
}
func (s *Store) writeExecutionReceipts(ctx context.Context, height uint64, rs []*model.Receipt) error {
	wanted := map[common.Hash][]byte{}
	hashes := make([][]byte, 0, len(rs))
	for _, r := range rs {
		data, err := model.EncodeReceipt(r)
		if err != nil {
			return err
		}
		wanted[r.TxHash] = data
		hashes = append(hashes, r.TxHash.Bytes())
	}
	rows, err := s.q(ctx).Query(ctx, "SELECT tx_hash,data FROM receipts WHERE block_number=$1 OR tx_hash=ANY($2::bytea[])", int64(height), hashes)
	if err != nil {
		return err
	}
	for rows.Next() {
		var hash, data []byte
		if err := rows.Scan(&hash, &data); err != nil {
			rows.Close()
			return err
		}
		expected, ok := wanted[common.BytesToHash(hash)]
		if !ok || !bytes.Equal(expected, data) {
			rows.Close()
			return port.ErrExecutionConflict
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range rs {
		if err := s.SetReceipt(ctx, r); err != nil {
			return err
		}
	}
	return nil
}
func sameExecutionLog(a, b *model.Log) bool {
	return a.BlockNumber == b.BlockNumber && a.BlockHash == b.BlockHash && a.TxIndex == b.TxIndex && a.TxHash == b.TxHash && a.Index == b.Index && a.Address == b.Address && a.Removed == b.Removed && slices.Equal(a.Topics, b.Topics) && bytes.Equal(a.Data, b.Data)
}
func (s *Store) writeExecutionLogs(ctx context.Context, height uint64, rs []*model.Receipt) error {
	type key struct{ tx, index uint }
	wanted := map[key]*model.Log{}
	var logs []*model.Log
	for _, r := range rs {
		for _, l := range r.Logs {
			wanted[key{l.TxIndex, l.Index}] = l
			logs = append(logs, l)
		}
	}
	existing, err := s.GetLogsByBlock(ctx, height)
	if err != nil {
		return err
	}
	for _, l := range existing {
		expected, ok := wanted[key{l.TxIndex, l.Index}]
		if !ok || !sameExecutionLog(expected, l) {
			return port.ErrExecutionConflict
		}
	}
	return s.IndexLogs(ctx, logs)
}
