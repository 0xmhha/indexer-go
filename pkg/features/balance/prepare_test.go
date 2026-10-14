package balance

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/rpcbatch"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type preparationRecords struct {
	port.HistoricalReader
	port.HistoricalWriter
	mu       sync.Mutex
	balances map[common.Address]*big.Int
	failure  error
}

func (s *preparationRecords) HasBalanceRecord(_ context.Context, addr common.Address) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.balances[addr]
	return ok, s.failure
}
func (s *preparationRecords) SetBalance(_ context.Context, addr common.Address, _ uint64, value *big.Int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.balances[addr] = new(big.Int).Set(value)
	return nil
}
func (s *preparationRecords) UpdateBalance(_ context.Context, addr common.Address, _ uint64, delta *big.Int, _ common.Hash) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.balances[addr].Add(s.balances[addr], delta)
	return nil
}

type preparationAccounting []chains.BalanceDelta

func (a preparationAccounting) ConcurrentAccounting() bool { return true }
func (a preparationAccounting) NativeDeltas(context.Context, *model.Block, []*model.Receipt, chains.AccountingEnv) ([]chains.BalanceDelta, error) {
	return a, nil
}

// The first read represents an earlier block finishing initialization while
// later jobs wait for a permit. Only its committed records suppress reads;
// clearing those records before writing must restore the ordered RPC path.
func TestPreparedBalanceChecksRecordsAfterWaiting(t *testing.T) {
	for _, mode := range []struct {
		name        string
		size        int
		unsupported bool
	}{
		{"individual", 1, false}, {"batch", 2, false}, {"fallback", 2, true},
	} {
		for _, rollback := range []bool{false, true} {
			name := mode.name
			if rollback {
				name += "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				ctx := t.Context()
				records := &preparationRecords{balances: map[common.Address]*big.Int{}}
				var deltas preparationAccounting
				for i := int64(1); i <= 5; i++ {
					deltas = append(deltas, chains.BalanceDelta{Address: common.BigToAddress(big.NewInt(i)), Delta: big.NewInt(i)})
				}
				require.NoError(t, records.SetBalance(ctx, deltas[0].Address, 6, big.NewInt(30)))
				calls, batches := 0, 0
				parent := common.HexToHash("0x6")
				h := &handler{w: records, r: records, records: records, accounting: deltas, logger: zap.NewNop()}
				h.deps = feature.Deps{PrepareConcurrency: 1, StateBatchSize: mode.size}
				h.deps.BalanceAtHash = func(_ context.Context, addr common.Address, hash common.Hash) (*big.Int, error) {
					calls++
					require.Equal(t, parent, hash)
					require.Contains(t, []common.Address{deltas[1].Address, deltas[mode.size].Address}, addr, "later jobs should see committed initialization")
					for i := mode.size + 1; i < len(deltas); i++ {
						require.NoError(t, records.SetBalance(ctx, deltas[i].Address, 6, big.NewInt(int64(30*(i+1)))))
					}
					return new(big.Int).Mul(addr.Big(), big.NewInt(40)), nil
				}
				h.deps.BalanceBatchAtHash = func(ctx context.Context, addrs []common.Address, hash common.Hash) ([]*big.Int, []error, error) {
					batches++
					expected := make([]common.Address, mode.size)
					for i := range expected {
						expected[i] = deltas[i+1].Address
					}
					require.Equal(t, expected, addrs)
					if mode.unsupported {
						return nil, nil, rpcbatch.ErrUnsupported
					}
					values, failures := make([]*big.Int, len(addrs)), make([]error, len(addrs))
					for i, addr := range addrs {
						values[i], failures[i] = h.deps.BalanceAtHash(ctx, addr, hash)
					}
					return values, failures, nil
				}
				fallbackCalls := 0
				h.balanceAt = func(_ context.Context, addr common.Address, at *big.Int) (*big.Int, error) {
					fallbackCalls++
					require.True(t, rollback)
					require.Equal(t, big.NewInt(6), at)
					return new(big.Int).Mul(addr.Big(), big.NewInt(40)), nil
				}
				block := &feature.Block{Model: &model.Block{Number: 7, Hash: common.HexToHash("0x7"), ParentHash: parent}}
				prepared, err := h.PrepareBlock(ctx, block)
				require.NoError(t, err)
				require.True(t, prepared.ReadState)
				require.Equal(t, mode.size, calls, "waiting jobs no longer read all four initial balances")
				if mode.size > 1 {
					require.Equal(t, 1, batches)
				} else {
					require.Zero(t, batches)
				}
				if rollback {
					records.mu.Lock()
					for i := mode.size + 1; i < len(deltas); i++ {
						delete(records.balances, deltas[i].Address)
					}
					records.mu.Unlock()
				}
				require.NoError(t, prepared.Handler.HandleBlock(ctx, block))
				for i, delta := range deltas {
					initial := int64(30 * (i + 1))
					if i >= 1 && (i <= mode.size || rollback) {
						initial = int64(40 * (i + 1))
					}
					require.Equal(t, big.NewInt(initial+int64(i+1)), records.balances[delta.Address])
				}
				if rollback {
					require.Equal(t, 4-mode.size, fallbackCalls)
				} else {
					require.Zero(t, fallbackCalls)
				}
			})
		}
	}
}

func TestPreparedBalanceRecordedAccountsNeedNoState(t *testing.T) {
	for _, size := range []int{1, 16} {
		records := &preparationRecords{balances: map[common.Address]*big.Int{common.HexToAddress("0x1"): big.NewInt(10)}}
		h := &handler{records: records, accounting: preparationAccounting{{Address: common.HexToAddress("0x1"), Delta: big.NewInt(1)}}, deps: feature.Deps{PrepareConcurrency: 1, StateBatchSize: size}}
		h.deps.BalanceAtHash = func(context.Context, common.Address, common.Hash) (*big.Int, error) {
			t.Fatal("recorded accounts need no RPC")
			return nil, nil
		}
		h.deps.BalanceBatchAtHash = func(context.Context, []common.Address, common.Hash) ([]*big.Int, []error, error) {
			t.Fatal("empty batches must not reach RPC")
			return nil, nil, nil
		}
		block := &feature.Block{Model: &model.Block{Number: 7}}
		prepared, err := h.PrepareBlock(t.Context(), block)
		require.NoError(t, err)
		require.False(t, prepared.ReadState, "no unnecessary canonical state check")
		failure := errors.New("storage unavailable")
		records.failure = failure
		_, err = h.PrepareBlock(t.Context(), block)
		require.ErrorIs(t, err, failure, "storage failures cannot suppress required reads")
	}
}

type preparationChecker func(context.Context, common.Address) (bool, error)

func (c preparationChecker) HasBalanceRecord(ctx context.Context, addr common.Address) (bool, error) {
	return c(ctx, addr)
}

func TestPreparedBalanceCompactedBatchKeepsMemberIdentity(t *testing.T) {
	for _, memberFailure := range []error{nil, errors.New("historical state unavailable")} {
		records := &preparationRecords{balances: map[common.Address]*big.Int{}}
		a, b := common.HexToAddress("0x2"), common.HexToAddress("0x3")
		checks := 0
		h := &handler{w: records, r: records, logger: zap.NewNop(), accounting: preparationAccounting{{Address: a, Delta: big.NewInt(2)}, {Address: b, Delta: big.NewInt(3)}}, deps: feature.Deps{PrepareConcurrency: 1, StateBatchSize: 16}}
		h.records = preparationChecker(func(ctx context.Context, addr common.Address) (bool, error) {
			checks++
			if checks == 3 {
				require.NoError(t, records.SetBalance(ctx, a, 6, big.NewInt(60)))
			}
			return records.HasBalanceRecord(ctx, addr)
		})
		h.deps.BalanceAtHash = func(context.Context, common.Address, common.Hash) (*big.Int, error) {
			t.Fatal("single RPC unexpected")
			return nil, nil
		}
		h.deps.BalanceBatchAtHash = func(_ context.Context, addrs []common.Address, hash common.Hash) ([]*big.Int, []error, error) {
			require.Equal(t, []common.Address{b}, addrs)
			require.Equal(t, common.HexToHash("0x6"), hash)
			return []*big.Int{big.NewInt(120)}, []error{memberFailure}, nil
		}
		block := &feature.Block{Model: &model.Block{Number: 7, ParentHash: common.HexToHash("0x6")}}
		prepared, err := h.PrepareBlock(t.Context(), block)
		require.NoError(t, err)
		err = prepared.Handler.HandleBlock(t.Context(), block)
		if memberFailure != nil {
			require.ErrorIs(t, err, memberFailure, "member errors stay with the remaining account")
			require.NotContains(t, records.balances, b)
		} else {
			require.NoError(t, err)
			require.Equal(t, big.NewInt(62), records.balances[a])
			require.Equal(t, big.NewInt(123), records.balances[b])
		}
	}
}
