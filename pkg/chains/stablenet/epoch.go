package stablenet

import (
	"context"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// WBFT epochs. An epoch block carries, in its extra data, the candidates
// and validators of the next epoch. The validator set that applies to block
// N is the one recorded in the last epoch block at or below N-1
// (go-stablenet consensus/wbft/engine getEpochInfo); genesis carries the
// first set.

// EpochInfo is the EpochInfo element of WBFT extra data
// (core/types/istanbul.go).
type EpochInfo struct {
	Candidates    []*EpochCandidate
	Validators    []uint32 // indexes into Candidates, in seal bitmap order
	BLSPublicKeys [][]byte
}

// EpochCandidate is a validator candidate.
type EpochCandidate struct {
	Addr      common.Address
	Diligence uint64
}

// ValidatorAddresses returns the validators in seal bitmap order.
func (e *EpochInfo) ValidatorAddresses() ([]common.Address, error) {
	out := make([]common.Address, len(e.Validators))
	for i, idx := range e.Validators {
		if int(idx) >= len(e.Candidates) || e.Candidates[idx] == nil {
			return nil, fmt.Errorf("validator index %d out of range of %d candidates", idx, len(e.Candidates))
		}
		out[i] = e.Candidates[idx].Addr
	}
	return out, nil
}

// extraEpochInfo is the position of the epoch info in the WBFT extra list.
const extraEpochInfo = 9

// ExtraEpochInfo decodes the epoch info of WBFT extra data; it is nil for
// blocks that are not epoch blocks and for extra data that is not WBFT.
func ExtraEpochInfo(extra []byte) (*EpochInfo, error) {
	var elems []rlp.RawValue
	if err := rlp.DecodeBytes(extra, &elems); err != nil || len(elems) != extraFields {
		return nil, nil
	}
	raw := elems[extraEpochInfo]
	if len(raw) == 1 && (raw[0] == 0xc0 || raw[0] == 0x80) {
		return nil, nil // absent
	}
	var info EpochInfo
	if err := rlp.DecodeBytes(raw, &info); err != nil {
		return nil, fmt.Errorf("decode WBFT epoch info: %w", err)
	}
	return &info, nil
}

// EpochTracker finds the epoch that applies to each block. Blocks are
// normally processed in order, so it remembers the last block and the
// epoch for the next one; otherwise (restart, gap, rollback) it reads back
// through earlier blocks to the last epoch block.
type EpochTracker struct {
	mu   sync.Mutex
	last *trackedBlock
}

type trackedBlock struct {
	hash    common.Hash
	number  uint64
	applies *EpochInfo // the epoch of this block
	next    *EpochInfo // the epoch of the block after it
}

// For returns the epoch that applies to block b (nil for genesis) and
// remembers b.
func (t *EpochTracker) For(ctx context.Context, b *model.Block, blocks chains.AccountingEnv) (*EpochInfo, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var info *EpochInfo
	switch {
	case b.Number == 0:
	case t.last != nil && t.last.number == b.Number-1 && t.last.hash == b.ParentHash:
		info = t.last.next
	default:
		var err error
		if info, err = lastEpochAtOrBelow(ctx, b.Number-1, blocks); err != nil {
			return nil, err
		}
	}
	next := info
	own, err := ExtraEpochInfo(b.Extra)
	if err != nil {
		return nil, fmt.Errorf("stablenet: block %d: %w", b.Number, err)
	}
	if own != nil {
		next = own
	}
	t.last = &trackedBlock{hash: b.Hash, number: b.Number, applies: info, next: next}
	return info, nil
}

// Applied returns the epoch that applied to the remembered block if it is
// the block with hash h.
func (t *EpochTracker) Applied(h common.Hash) (*EpochInfo, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil || t.last.hash != h {
		return nil, false
	}
	return t.last.applies, true
}

// EpochOf returns the epoch that applies to block n without changing the
// tracker.
func EpochOf(ctx context.Context, n uint64, blocks chains.AccountingEnv) (*EpochInfo, error) {
	if n == 0 {
		return nil, nil
	}
	return lastEpochAtOrBelow(ctx, n-1, blocks)
}

// lastEpochAtOrBelow reads blocks back from n to the newest one carrying
// epoch info.
func lastEpochAtOrBelow(ctx context.Context, n uint64, blocks chains.AccountingEnv) (*EpochInfo, error) {
	if blocks == nil {
		return nil, fmt.Errorf("stablenet: cannot read earlier blocks for epoch info")
	}
	for h := n; ; h-- {
		blk, err := blocks.Block(ctx, h)
		if err != nil {
			return nil, fmt.Errorf("stablenet: read block %d for epoch info: %w", h, err)
		}
		info, err := ExtraEpochInfo(blk.Extra)
		if err != nil {
			return nil, fmt.Errorf("stablenet: block %d: %w", h, err)
		}
		if info != nil {
			return info, nil
		}
		if h == 0 {
			return nil, nil
		}
	}
}
