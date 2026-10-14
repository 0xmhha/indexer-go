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
// normally processed in order. A bounded, hash-keyed cache also retains
// verified ancestor resolutions for speculative, repeated and forked blocks.
// Height alone never identifies a cached epoch.
type EpochTracker struct {
	mu    sync.Mutex
	cache map[common.Hash]*trackedBlock
	order [epochCacheSize]common.Hash
	pos   int
}

const epochCacheSize = 256

type trackedBlock struct {
	hash    common.Hash
	number  uint64
	applies *EpochInfo // the epoch of this block
	next    *EpochInfo // the epoch of the block after it
	// Ancestor walks can resolve next without knowing the epoch of an
	// epoch block itself (its own set applies only after that block).
	appliesKnown bool
}

// For returns the epoch that applies to block b (nil for genesis) and
// remembers b.
func (t *EpochTracker) For(ctx context.Context, b *model.Block, blocks chains.AccountingEnv) (*EpochInfo, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if known := t.lookup(b.Hash, b.Number); known != nil && known.appliesKnown {
		return known.applies, nil
	}
	own, err := ExtraEpochInfo(b.Extra)
	if err != nil {
		return nil, fmt.Errorf("stablenet: block %d: %w", b.Number, err)
	}

	var info *EpochInfo
	if b.Number > 0 {
		if info, err = t.resolveParent(ctx, b.Number-1, b.ParentHash, blocks); err != nil {
			return nil, err
		}
	}
	next := info
	if own != nil {
		next = own
	}
	t.remember(&trackedBlock{hash: b.Hash, number: b.Number, applies: info, next: next, appliesKnown: true})
	return info, nil
}

func (t *EpochTracker) lookup(hash common.Hash, n uint64) *trackedBlock {
	if hash == (common.Hash{}) {
		return nil // a missing hash cannot identify an immutable header
	}
	if entry := t.cache[hash]; entry != nil && entry.number == n {
		return entry
	}
	return nil
}

// remember is called under mu, after the entire ancestor path is verified.
func (t *EpochTracker) remember(entry *trackedBlock) {
	if entry.hash == (common.Hash{}) {
		return
	}
	if t.cache == nil {
		t.cache = make(map[common.Hash]*trackedBlock)
	}
	if old := t.cache[entry.hash]; old != nil {
		if !old.appliesKnown || entry.appliesKnown {
			t.cache[entry.hash] = entry
		}
		return
	}
	delete(t.cache, t.order[t.pos])
	t.order[t.pos] = entry.hash
	t.pos = (t.pos + 1) % epochCacheSize
	t.cache[entry.hash] = entry
}

func (t *EpochTracker) resolveParent(ctx context.Context, n uint64, expected common.Hash, blocks chains.AccountingEnv) (*EpochInfo, error) {
	var path []*trackedBlock
	var info *EpochInfo
	var bottomEpoch common.Hash
	for h := n; ; h-- {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if known := t.lookup(expected, h); known != nil {
			info = known.next
			break
		}
		if blocks == nil {
			return nil, fmt.Errorf("stablenet: cannot read earlier blocks for epoch info")
		}
		blk, err := blocks.Block(ctx, h)
		if err != nil {
			return nil, fmt.Errorf("stablenet: read block %d for epoch info: %w", h, err)
		}
		if blk == nil || blk.Number != h {
			return nil, fmt.Errorf("stablenet: invalid epoch header at %d", h)
		}
		if blk.Hash != expected {
			return nil, fmt.Errorf("stablenet: epoch header %d hash %s differs from parent %s: %w", h, blk.Hash, expected, chains.ErrAccountingFork)
		}
		own, err := ExtraEpochInfo(blk.Extra)
		if err != nil {
			return nil, fmt.Errorf("stablenet: block %d: %w", h, err)
		}
		if len(path) < epochCacheSize {
			path = append(path, &trackedBlock{hash: blk.Hash, number: blk.Number})
		}
		if own != nil || h == 0 {
			info = own
			if own != nil {
				bottomEpoch = blk.Hash
			}
			break
		}
		expected = blk.ParentHash
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// All non-epoch headers above the stopping point use its next set for
	// themselves and their children. The stopping epoch's own set is unknown.
	for i := len(path) - 1; i >= 0; i-- {
		entry := path[i]
		entry.appliesKnown = entry.hash != bottomEpoch || entry.number == 0
		entry.applies, entry.next = info, info
		if !entry.appliesKnown || entry.number == 0 {
			entry.applies = nil
		}
		t.remember(entry)
	}
	return info, nil
}

// Applied returns the resolved epoch of a retained block with hash h.
func (t *EpochTracker) Applied(h common.Hash) (*EpochInfo, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.cache[h]
	if entry == nil || !entry.appliesKnown {
		return nil, false
	}
	return entry.applies, true
}

// EpochOf returns the epoch that applies to block n without changing the
// tracker.
func EpochOf(ctx context.Context, n uint64, blocks chains.AccountingEnv) (*EpochInfo, error) {
	if n == 0 {
		return nil, nil
	}
	return lastEpochAtOrBelow(ctx, n-1, blocks, nil)
}

// lastEpochAtOrBelow reads blocks back from n to the newest one carrying
// epoch info.
func lastEpochAtOrBelow(ctx context.Context, n uint64, blocks chains.AccountingEnv, expected *common.Hash) (*EpochInfo, error) {
	if blocks == nil {
		return nil, fmt.Errorf("stablenet: cannot read earlier blocks for epoch info")
	}
	for h := n; ; h-- {
		blk, err := blocks.Block(ctx, h)
		if err != nil {
			return nil, fmt.Errorf("stablenet: read block %d for epoch info: %w", h, err)
		}
		if expected != nil && blk.Hash != *expected {
			return nil, fmt.Errorf("stablenet: epoch header %d hash %s differs from parent %s: %w", h, blk.Hash, *expected, chains.ErrAccountingFork)
		}
		expected = &blk.ParentHash
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
