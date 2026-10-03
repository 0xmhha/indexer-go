package storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
)

// ErrBlockTxDone is returned when a finished block transaction is used again.
var ErrBlockTxDone = errors.New("storage: block transaction already finished")

// BlockTransactor is implemented by storages that can index one block
// atomically. The fetcher uses it to make all writes for a block, including
// the cursor, become durable together or not at all.
type BlockTransactor interface {
	BeginBlock(ctx context.Context) (context.Context, *BlockTx, error)
}

// BlockTx is an open block transaction. Every PebbleStorage call made with
// the context returned by BeginBlock reads and writes through one indexed
// batch, so reads see earlier writes of the same block and nothing reaches
// the database until Commit.
//
// In-memory state that storage methods advance while writing (per-address
// sequence numbers and the transaction counter) is staged here and published
// only on a successful Commit, so a rolled back block leaves no trace and
// re-processing it produces the same keys.
//
// Only one block transaction may be open per storage (single writer); a
// second BeginBlock waits until the first finishes. A BlockTx must not be
// used from more than one goroutine.
type BlockTx struct {
	s       *PebbleStorage
	batch   *pebble.Batch
	seqNext map[common.Address]uint64 // staged next sequence per address
	txDelta uint64                    // staged transaction count increment
	// genesisSeen records lazy genesis lookups made in this block.
	genesisSeen map[common.Address]bool
	done        bool
}

// BeginBlock opens a block transaction and returns a context bound to it.
// The caller must call Commit or Rollback; deferring Rollback is safe because
// it does nothing after Commit.
func (s *PebbleStorage) BeginBlock(ctx context.Context) (context.Context, *BlockTx, error) {
	if err := s.ensureNotClosed(); err != nil {
		return ctx, nil, err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return ctx, nil, err
	}
	if s.boundTx(ctx) != nil {
		return ctx, nil, errors.New("storage: block transaction already open in this context")
	}
	s.writeMu.Lock()
	tx := &BlockTx{
		s:       s,
		batch:   s.db.NewIndexedBatch(),
		seqNext: map[common.Address]uint64{},
	}
	bound := context.WithValue(ctx, blockTxKey{}, &blockTxBinding{owner: s, batch: tx.batch, tx: tx})
	return bound, tx, nil
}

// Commit makes all writes of the block durable atomically (fsync) and then
// publishes the staged in-memory state.
func (tx *BlockTx) Commit() error {
	if tx.done {
		return ErrBlockTxDone
	}
	tx.done = true
	defer tx.s.writeMu.Unlock()
	defer tx.batch.Close()

	if err := tx.batch.Commit(pebble.Sync); err != nil {
		return err
	}
	if len(tx.seqNext) > 0 {
		tx.s.addrSeqMu.Lock()
		for addr, next := range tx.seqNext {
			tx.s.addrSeq[addr] = next
		}
		tx.s.addrSeqMu.Unlock()
	}
	if tx.txDelta > 0 {
		tx.s.txCount.Add(tx.txDelta)
	}
	tx.publishGenesisTried()
	return nil
}

// Rollback discards the block's writes and staged state. It is a no-op after
// Commit or a previous Rollback.
func (tx *BlockTx) Rollback() {
	if tx == nil || tx.done {
		return
	}
	tx.done = true
	_ = tx.batch.Close()
	tx.s.writeMu.Unlock()
}

// Len returns the size in bytes of the pending batch (for metrics and limits).
func (tx *BlockTx) Len() int { return tx.batch.Len() }

func (s *PebbleStorage) boundTx(ctx context.Context) *BlockTx {
	if ctx == nil {
		return nil
	}
	if b, ok := ctx.Value(blockTxKey{}).(*blockTxBinding); ok && b.owner == s {
		return b.tx
	}
	return nil
}

// nextAddrSeq returns the sequence number for the next per-address entry
// (address transaction index and balance history share one counter) and
// advances it. Inside a block transaction the advance is staged.
//
// The first time an address is seen in this process its counter is restored
// from disk, so entries written before a restart are never overwritten.
func (s *PebbleStorage) nextAddrSeq(ctx context.Context, addr common.Address) (uint64, error) {
	if tx := s.boundTx(ctx); tx != nil {
		next, ok := tx.seqNext[addr]
		if !ok {
			s.addrSeqMu.RLock()
			next, ok = s.addrSeq[addr]
			s.addrSeqMu.RUnlock()
		}
		if !ok {
			restored, err := s.restoreAddrSeq(ctx, addr)
			if err != nil {
				return 0, err
			}
			next = restored
		}
		tx.seqNext[addr] = next + 1
		return next, nil
	}

	s.addrSeqMu.Lock()
	defer s.addrSeqMu.Unlock()
	next, ok := s.addrSeq[addr]
	if !ok {
		restored, err := s.restoreAddrSeq(ctx, addr)
		if err != nil {
			return 0, err
		}
		next = restored
	}
	s.addrSeq[addr] = next + 1
	return next, nil
}

// restoreAddrSeq returns one past the highest sequence stored for addr in
// either per-address prefix, or 0 when there is none.
func (s *PebbleStorage) restoreAddrSeq(ctx context.Context, addr common.Address) (uint64, error) {
	var next uint64
	for _, prefix := range [][]byte{AddressTransactionKeyPrefix(addr), AddressBalanceKeyPrefix(addr)} {
		last, found, err := s.lastSeqUnder(ctx, prefix)
		if err != nil {
			return 0, fmt.Errorf("restore address sequence for %s: %w", addr.Hex(), err)
		}
		if found && last+1 > next {
			next = last + 1
		}
	}
	return next, nil
}

// lastSeqUnder finds the highest "%020d" sequence key directly under prefix,
// skipping keys that are not sequence keys.
func (s *PebbleStorage) lastSeqUnder(ctx context.Context, prefix []byte) (uint64, bool, error) {
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return 0, false, err
	}
	defer iter.Close()
	for valid := iter.Last(); valid; valid = iter.Prev() {
		suffix := iter.Key()[len(prefix):]
		if len(suffix) != 20 {
			continue
		}
		if v, err := strconv.ParseUint(string(suffix), 10, 64); err == nil {
			return v, true, nil
		}
	}
	return 0, false, iter.Error()
}

// addTxCount advances the transaction counter by n and returns the value to
// persist. Inside a block transaction the advance is staged.
func (s *PebbleStorage) addTxCount(ctx context.Context, n uint64) uint64 {
	if tx := s.boundTx(ctx); tx != nil {
		tx.txDelta += n
		return s.txCount.Load() + tx.txDelta
	}
	return s.txCount.Add(n)
}

// subTxCount undoes addTxCount after a failed write outside a transaction.
func (s *PebbleStorage) subTxCount(ctx context.Context, n uint64) {
	if tx := s.boundTx(ctx); tx != nil {
		tx.txDelta -= n
		return
	}
	s.txCount.Add(^(n - 1))
}

var _ BlockTransactor = (*PebbleStorage)(nil)
