package stablenet

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

type countedEpochHeaders struct {
	blocks map[uint64]*model.Block
	reads  atomic.Int64
}

func (e *countedEpochHeaders) Block(_ context.Context, n uint64) (*model.Block, error) {
	e.reads.Add(1)
	b, ok := e.blocks[n]
	if !ok {
		return nil, fmt.Errorf("missing header %d", n)
	}
	return b, nil
}

func epochFixture(t testing.TB, tag byte, end uint64, changes map[uint64]*EpochInfo) *countedEpochHeaders {
	t.Helper()
	env := &countedEpochHeaders{blocks: make(map[uint64]*model.Block)}
	for n := uint64(0); n <= end; n++ {
		b := &model.Block{Number: n, Hash: crypto.Keccak256Hash([]byte(fmt.Sprintf("%d/%d", tag, n)))}
		if n > 0 {
			b.ParentHash = env.blocks[n-1].Hash
		}
		if info := changes[n]; info != nil {
			elems := make([]rlp.RawValue, extraFields)
			for i := range elems {
				elems[i] = rlp.RawValue{0xc0}
			}
			var err error
			elems[extraEpochInfo], err = rlp.EncodeToBytes(info)
			require.NoError(t, err)
			b.Extra, err = rlp.EncodeToBytes(elems)
			require.NoError(t, err)
		}
		env.blocks[n] = b
	}
	return env
}

func fixtureEpoch(tag byte) *EpochInfo {
	return &EpochInfo{Candidates: []*EpochCandidate{{Addr: common.Address{tag}, Diligence: 1}}, Validators: []uint32{0}, BLSPublicKeys: [][]byte{}}
}

func TestEpochTrackerRepeatedAndOutOfOrder(t *testing.T) {
	info := fixtureEpoch(1)
	env := epochFixture(t, 1, 60, map[uint64]*EpochInfo{0: info})
	var tracker EpochTracker
	for _, n := range []uint64{58, 60, 59, 58, 60, 59, 58, 59, 60, 58, 60, 59} {
		got, err := tracker.For(context.Background(), env.blocks[n], env)
		require.NoError(t, err)
		require.Equal(t, info, got)
	}
	t.Logf("ancestor reads for 12 repeated/out-of-order blocks: %d", env.reads.Load())
	require.Equal(t, int64(59), env.reads.Load(), "resolve the first 58 ancestors once, then only the missing header 59")
	got, known := tracker.Applied(env.blocks[58].Hash)
	require.True(t, known)
	require.Equal(t, info, got)
}

func TestEpochTrackerCachedEpochBoundary(t *testing.T) {
	old, next := fixtureEpoch(1), fixtureEpoch(2)
	env := epochFixture(t, 1, 42, map[uint64]*EpochInfo{0: old, 40: next})
	var tracker EpochTracker
	got, err := tracker.For(context.Background(), env.blocks[41], env)
	require.NoError(t, err)
	require.Equal(t, next, got)
	_, known := tracker.Applied(env.blocks[40].Hash)
	require.False(t, known, "the epoch header's own set does not apply to that header")
	got, err = tracker.For(context.Background(), env.blocks[40], env)
	require.NoError(t, err)
	require.Equal(t, old, got)
	for _, n := range []uint64{39, 42, 40, 41} {
		got, err = tracker.For(context.Background(), env.blocks[n], env)
		require.NoError(t, err)
		want := old
		if n > 40 {
			want = next
		}
		require.Equal(t, want, got, "block %d", n)
	}
}

func TestEpochTrackerCacheRejectsWrongFork(t *testing.T) {
	a, b := fixtureEpoch(1), fixtureEpoch(2)
	left := epochFixture(t, 1, 60, map[uint64]*EpochInfo{0: a})
	right := epochFixture(t, 2, 60, map[uint64]*EpochInfo{0: b})
	var tracker EpochTracker
	_, err := tracker.For(context.Background(), left.blocks[60], left)
	require.NoError(t, err)
	_, err = tracker.For(context.Background(), right.blocks[60], left)
	require.ErrorIs(t, err, chains.ErrAccountingFork)
	_, known := tracker.Applied(right.blocks[60].Hash)
	require.False(t, known)
	got, err := tracker.For(context.Background(), right.blocks[60], right)
	require.NoError(t, err)
	require.Equal(t, b, got)
	got, err = tracker.For(context.Background(), left.blocks[60], left)
	require.NoError(t, err)
	require.Equal(t, a, got, "forks at the same height retain distinct epoch sets")
}

func TestEpochTrackerFailedWalkAndCancellation(t *testing.T) {
	env := epochFixture(t, 1, 58, map[uint64]*EpochInfo{0: fixtureEpoch(1)})
	saved := env.blocks[20]
	delete(env.blocks, 20)
	var tracker EpochTracker
	_, err := tracker.For(context.Background(), env.blocks[58], env)
	require.ErrorContains(t, err, "missing header 20")
	first := env.reads.Load()
	env.blocks[20] = saved
	_, err = tracker.For(context.Background(), env.blocks[58], env)
	require.NoError(t, err)
	require.Equal(t, first+58, env.reads.Load(), "an incomplete walk publishes no partial resolution")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = tracker.For(ctx, env.blocks[58], env)
	require.ErrorIs(t, err, context.Canceled, "cached results respect cancellation")
}

func TestEpochTrackerCacheEvictsOldHeaders(t *testing.T) {
	env := epochFixture(t, 1, 260, map[uint64]*EpochInfo{0: fixtureEpoch(1)})
	var tracker EpochTracker
	for n := uint64(0); n <= 260; n++ {
		_, err := tracker.For(context.Background(), env.blocks[n], env)
		require.NoError(t, err)
	}
	require.Zero(t, env.reads.Load(), "sequential blocks reuse the parent resolution")
	_, err := tracker.For(context.Background(), env.blocks[1], env)
	require.NoError(t, err)
	require.Equal(t, int64(1), env.reads.Load(), "old block 1 and its parent must have left the bounded cache")
}

func TestEpochTrackerLongAncestorWalk(t *testing.T) {
	env := epochFixture(t, 1, 1025, map[uint64]*EpochInfo{0: fixtureEpoch(1)})
	var tracker EpochTracker
	_, err := tracker.For(context.Background(), env.blocks[1024], env)
	require.NoError(t, err)
	require.Equal(t, int64(1024), env.reads.Load())
	for _, n := range []uint64{1025, 900, 1024} {
		_, err = tracker.For(context.Background(), env.blocks[n], env)
		require.NoError(t, err)
	}
	require.Equal(t, int64(1024), env.reads.Load(), "a long walk retains the headers nearest its child")
}

func TestEpochTrackerRejectsInvalidAncestor(t *testing.T) {
	for _, name := range []string{"nil", "wrong-height"} {
		t.Run(name, func(t *testing.T) {
			env := epochFixture(t, 1, 2, map[uint64]*EpochInfo{0: fixtureEpoch(1)})
			if name == "nil" {
				env.blocks[1] = nil
			} else {
				wrong := *env.blocks[1]
				wrong.Number = 0
				env.blocks[1] = &wrong
			}
			var tracker EpochTracker
			_, err := tracker.For(context.Background(), env.blocks[2], env)
			require.ErrorContains(t, err, "invalid epoch header at 1")
			_, known := tracker.Applied(env.blocks[2].Hash)
			require.False(t, known)
		})
	}
}

func TestEpochTrackerConcurrentForks(t *testing.T) {
	left := epochFixture(t, 1, 61, map[uint64]*EpochInfo{0: fixtureEpoch(1)})
	right := epochFixture(t, 2, 61, map[uint64]*EpochInfo{0: fixtureEpoch(2)})
	var tracker EpochTracker
	start := make(chan struct{})
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for _, env := range []*countedEpochHeaders{left, right} {
		want, err := ExtraEpochInfo(env.blocks[0].Extra)
		require.NoError(t, err)
		for n := uint64(58); n <= 61; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for range 10 {
					got, err := tracker.For(context.Background(), env.blocks[n], env)
					if err != nil || got == nil || got.Candidates[0].Addr != want.Candidates[0].Addr {
						errs <- fmt.Errorf("block %d got epoch %v: %v", n, got, err)
						return
					}
				}
			}()
		}
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.LessOrEqual(t, left.reads.Load()+right.reads.Load(), int64(122), "each fork's linked ancestor headers are resolved once")
}

func BenchmarkEpochTrackerOutOfOrder(b *testing.B) {
	env := epochFixture(b, 1, 60, map[uint64]*EpochInfo{0: fixtureEpoch(1)})
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var tracker EpochTracker
		for _, n := range []uint64{58, 60, 59, 58, 60, 59, 58, 59, 60, 58, 60, 59} {
			if _, err := tracker.For(ctx, env.blocks[n], env); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(env.reads.Load())/float64(b.N), "headers/op")
}
