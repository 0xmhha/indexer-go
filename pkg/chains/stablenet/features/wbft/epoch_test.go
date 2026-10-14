package wbft

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

type epochBlockReader struct {
	blocks map[uint64]*model.Block
	reads  int
}

func (r *epochBlockReader) Block(_ context.Context, n uint64) (*model.Block, error) {
	r.reads++
	b, ok := r.blocks[n]
	if !ok {
		return nil, fmt.Errorf("missing parent %d", n)
	}
	return b, nil
}

func epochBlocks(t *testing.T) *epochBlockReader {
	r := &epochBlockReader{blocks: make(map[uint64]*model.Block)}
	for n := uint64(0); n <= 59; n++ {
		b := &model.Block{Number: n, Hash: common.BigToHash(new(big.Int).SetUint64(n + 1)), Time: n + 1000}
		if n > 0 {
			b.ParentHash = r.blocks[n-1].Hash
		}
		r.blocks[n] = b
	}
	info := &stablenet.EpochInfo{Candidates: []*stablenet.EpochCandidate{{Addr: common.Address{1}, Diligence: 1}}, Validators: []uint32{0}}
	elems := make([]rlp.RawValue, 10)
	for i := range elems {
		elems[i] = rlp.RawValue{0xc0}
	}
	var err error
	elems[9], err = rlp.EncodeToBytes(info)
	require.NoError(t, err)
	r.blocks[0].Extra, err = rlp.EncodeToBytes(elems)
	require.NoError(t, err)
	return r
}

func TestParentEpochPrimesCurrentBlock(t *testing.T) {
	env := epochBlocks(t)
	h := &handler{blocks: env}
	b := env.blocks[58]
	parentEpoch, parent, err := h.parentEpochFor(context.Background(), b)
	require.NoError(t, err)
	require.Equal(t, b.ParentHash, parent.Hash)
	require.Equal(t, uint64(1057), parent.Time, "reuse the already verified parent for its signing timestamp")
	currentEpoch, err := h.epochs.For(context.Background(), b, env)
	require.NoError(t, err)
	require.Equal(t, parentEpoch, currentEpoch)
	require.Equal(t, 58, env.reads, "one parent read and one linked walk suffice for both epochs")
	_, parent, err = h.parentEpochFor(context.Background(), env.blocks[59])
	require.NoError(t, err)
	require.Nil(t, parent, "an already resolved parent needs no node header")
	require.Equal(t, 58, env.reads)
}

func TestParentEpochRejectsForkAndMissingHeader(t *testing.T) {
	for _, name := range []string{"wrong-hash", "wrong-height", "nil", "missing"} {
		t.Run(name, func(t *testing.T) {
			env := epochBlocks(t)
			b := env.blocks[58]
			switch name {
			case "wrong-hash":
				env.blocks[57].Hash = common.Hash{99}
			case "wrong-height":
				env.blocks[57].Number = 56
			case "nil":
				env.blocks[57] = nil
			case "missing":
				delete(env.blocks, 57)
			}
			h := &handler{blocks: env}
			_, _, err := h.parentEpochFor(context.Background(), b)
			if name == "missing" {
				require.ErrorContains(t, err, "missing parent 57")
			} else {
				require.ErrorIs(t, err, chains.ErrAccountingFork)
			}
			_, known := h.epochs.Applied(b.ParentHash)
			require.False(t, known)
		})
	}
}

func TestParentEpochAtTransition(t *testing.T) {
	env := epochBlocks(t)
	var elems []rlp.RawValue
	require.NoError(t, rlp.DecodeBytes(env.blocks[0].Extra, &elems))
	info := &stablenet.EpochInfo{Candidates: []*stablenet.EpochCandidate{{Addr: common.Address{2}, Diligence: 1}}, Validators: []uint32{0}}
	var err error
	elems[9], err = rlp.EncodeToBytes(info)
	require.NoError(t, err)
	env.blocks[40].Extra, err = rlp.EncodeToBytes(elems)
	require.NoError(t, err)
	h := &handler{blocks: env}
	parentEpoch, _, err := h.parentEpochFor(context.Background(), env.blocks[41])
	require.NoError(t, err)
	currentEpoch, err := h.epochs.For(context.Background(), env.blocks[41], env)
	require.NoError(t, err)
	require.Equal(t, common.Address{1}, parentEpoch.Candidates[0].Addr, "the parent epoch header is signed by the old set")
	require.Equal(t, common.Address{2}, currentEpoch.Candidates[0].Addr, "the new set applies to its child")
	require.Equal(t, 41, env.reads)
}
