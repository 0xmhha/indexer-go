package porttest

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Extra accounts and topics of the log fixture.
var (
	logsAddrD     = common.HexToAddress("0x00000000000000000000000000000000000000d5")
	logsApproval  = crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))
	logsTopicA    = common.BytesToHash(addrA.Bytes())
	logsTopicB    = common.BytesToHash(addrB.Bytes())
	logsLatestNum = uint64(4)
)

// logsFixture returns logs over blocks 1..4 from several contracts with
// varied topics, in chain order:
//
//	0: block 1 tx 0 log 0, addrC,     [Transfer, B, A]
//	1: block 1 tx 1 log 1, logsAddrD, [Transfer, A, B]
//	2: block 2 tx 0 log 0, logsAddrD, [Approval, A, B]
//	3: block 3 tx 0 log 0, addrC,     [Approval, B, A]
//	4: block 3 tx 0 log 1, addrC,     [] (anonymous)
//	5: block 4 tx 2 log 0, unknown,   [Transfer]
func logsFixture() []*model.Log {
	mk := func(block uint64, tx, idx uint, addr common.Address, topics ...common.Hash) *model.Log {
		if topics == nil {
			topics = []common.Hash{} // stores keep no distinction between nil and empty
		}
		return &model.Log{
			Address: addr, Topics: topics, Data: []byte{byte(block), byte(tx), byte(idx)},
			BlockNumber: block, BlockHash: fixtureHash("block", block),
			TxHash: fixtureHash("logtx", block*100+uint64(tx)), TxIndex: tx, Index: idx,
		}
	}
	return []*model.Log{
		mk(1, 0, 0, addrC, transferTopic, logsTopicB, logsTopicA),
		mk(1, 1, 1, logsAddrD, transferTopic, logsTopicA, logsTopicB),
		mk(2, 0, 0, logsAddrD, logsApproval, logsTopicA, logsTopicB),
		mk(3, 0, 0, addrC, logsApproval, logsTopicB, logsTopicA),
		mk(3, 0, 1, addrC),
		mk(4, 2, 0, unknown, transferTopic),
	}
}

// logsFixtureStore returns a store holding logsFixture with latest height 4.
func logsFixtureStore(t *testing.T, newStore NewStore) (logStore, []*model.Log) {
	t.Helper()
	s := open[logStore](t, newStore)
	logs := logsFixture()
	require.NoError(t, s.IndexLogs(context.Background(), logs))
	require.NoError(t, s.SetLatestHeight(context.Background(), logsLatestNum))
	return s, logs
}

// logsPick returns the fixture logs at the given positions.
func logsPick(logs []*model.Log, idx ...int) []*model.Log {
	out := make([]*model.Log, 0, len(idx))
	for _, i := range idx {
		out = append(out, logs[i])
	}
	return out
}

// assertLogsInOrder compares logs one by one.
func assertLogsInOrder(t *testing.T, want, got []*model.Log, msgAndArgs ...interface{}) {
	t.Helper()
	require.Len(t, got, len(want), msgAndArgs...)
	for i := range want {
		assertLog(t, want[i], got[i])
	}
}

// assertLogsAnyOrder compares logs ignoring their order.
func assertLogsAnyOrder(t *testing.T, want, got []*model.Log) {
	t.Helper()
	type pos struct {
		block   uint64
		tx, idx uint
	}
	byPos := make(map[pos]*model.Log, len(got))
	for _, l := range got {
		byPos[pos{l.BlockNumber, l.TxIndex, l.Index}] = l
	}
	require.Len(t, got, len(want))
	require.Len(t, byPos, len(want), "no log is returned twice")
	for _, w := range want {
		g, ok := byPos[pos{w.BlockNumber, w.TxIndex, w.Index}]
		require.True(t, ok, "log %d/%d/%d is missing", w.BlockNumber, w.TxIndex, w.Index)
		assertLog(t, w, g)
	}
}

// testLogs checks LogReader and LogWriter: indexed logs come back from
// every reader in chain order, block ranges are inclusive, and GetLogs
// applies LogFilter (addresses OR, topic positions AND with OR inside a
// position, an empty position matches anything, ToBlock 0 means the latest
// height).
func testLogs(t *testing.T, newStore NewStore) {
	ctx := context.Background()

	t.Run("EmptyStore", func(t *testing.T) {
		s := open[logStore](t, newStore)
		logs, err := s.GetLogsByBlock(ctx, 1)
		require.NoError(t, err)
		assert.Empty(t, logs)
		logs, err = s.GetLogsByAddress(ctx, addrC, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, logs)
		logs, err = s.GetLogsByTopic(ctx, transferTopic, 0, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, logs)
		logs, err = s.GetLogs(ctx, &port.LogFilter{})
		require.NoError(t, err)
		assert.Empty(t, logs)
	})

	t.Run("ChainLogsByBlock", func(t *testing.T) {
		s := open[logStore](t, newStore)
		c := newChain(5)
		c.write(t, s)
		for h := uint64(0); h <= c.head(); h++ {
			got, err := s.GetLogsByBlock(ctx, h)
			require.NoError(t, err, "block %d", h)
			assertLogsInOrder(t, c.logs(h, h), got)
		}
		got, err := s.GetLogsByBlock(ctx, 2)
		require.NoError(t, err)
		assert.Empty(t, got, "the failed call in block 2 emits no log")
	})

	t.Run("GetLogsByBlockOrder", func(t *testing.T) {
		s := open[logStore](t, newStore)
		logs := logsFixture()
		// Written out of order, one at a time.
		for _, i := range []int{4, 3} {
			require.NoError(t, s.IndexLog(ctx, logs[i]))
		}
		got, err := s.GetLogsByBlock(ctx, 3)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 3, 4), got)

		for _, i := range []int{1, 0} {
			require.NoError(t, s.IndexLog(ctx, logs[i]))
		}
		got, err = s.GetLogsByBlock(ctx, 1)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 0, 1), got)
	})

	t.Run("GetLogsByAddress", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		got, err := s.GetLogsByAddress(ctx, addrC, 0, 10)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 0, 3, 4), got)

		got, err = s.GetLogsByAddress(ctx, addrC, 1, 1)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 0), got)

		got, err = s.GetLogsByAddress(ctx, logsAddrD, 2, 3)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 2), got, "both ends of the range are inclusive")

		got, err = s.GetLogsByAddress(ctx, addrA, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, got, "no log emitted by addrA")
	})

	t.Run("GetLogsByTopic", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		got, err := s.GetLogsByTopic(ctx, transferTopic, 0, 0, 10)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 0, 1, 5), got)

		got, err = s.GetLogsByTopic(ctx, transferTopic, 0, 2, 4)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 5), got, "the range is inclusive")

		got, err = s.GetLogsByTopic(ctx, logsTopicA, 1, 0, 10)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 1, 2), got, "topic A at position 1")

		got, err = s.GetLogsByTopic(ctx, logsTopicA, 2, 0, 10)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 0, 3), got, "topic A at position 2")

		got, err = s.GetLogsByTopic(ctx, transferTopic, 1, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, got, "the position is part of the match")

		got, err = s.GetLogsByTopic(ctx, transferTopic, 3, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, got)

		_, err = s.GetLogsByTopic(ctx, transferTopic, -1, 0, 10)
		assert.Error(t, err, "topic index below 0")
		_, err = s.GetLogsByTopic(ctx, transferTopic, 4, 0, 10)
		assert.Error(t, err, "topic index above 3")
	})

	t.Run("GetLogsRange", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		got, err := s.GetLogs(ctx, &port.LogFilter{FromBlock: 2, ToBlock: 3})
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 2, 3, 4), got)

		got, err = s.GetLogs(ctx, &port.LogFilter{FromBlock: 3, ToBlock: 3})
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 3, 4), got)

		got, err = s.GetLogs(ctx, &port.LogFilter{FromBlock: 5, ToBlock: 9})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("GetLogsToBlockZeroIsLatest", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		got, err := s.GetLogs(ctx, &port.LogFilter{})
		require.NoError(t, err)
		assertLogsInOrder(t, logs, got, "an empty filter returns every log up to the latest height")

		got, err = s.GetLogs(ctx, &port.LogFilter{FromBlock: 3})
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 3, 4, 5), got)

		require.NoError(t, s.SetLatestHeight(ctx, 3))
		got, err = s.GetLogs(ctx, &port.LogFilter{FromBlock: 3})
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 3, 4), got, "logs above the latest height are not returned")
	})

	t.Run("GetLogsFromAboveLatestIsEmpty", func(t *testing.T) {
		s, _ := logsFixtureStore(t, newStore)
		got, err := s.GetLogs(ctx, &port.LogFilter{FromBlock: logsLatestNum + 1})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("GetLogsRejectsInvertedRange", func(t *testing.T) {
		s, _ := logsFixtureStore(t, newStore)
		_, err := s.GetLogs(ctx, &port.LogFilter{FromBlock: 3, ToBlock: 2})
		assert.Error(t, err)
	})

	t.Run("GetLogsRejectsNilFilter", func(t *testing.T) {
		s, _ := logsFixtureStore(t, newStore)
		_, err := s.GetLogs(ctx, nil)
		assert.Error(t, err)
	})

	t.Run("GetLogsAddresses", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		got, err := s.GetLogs(ctx, &port.LogFilter{Addresses: []common.Address{logsAddrD}})
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 1, 2), got)

		got, err = s.GetLogs(ctx, &port.LogFilter{Addresses: []common.Address{logsAddrD, addrC}})
		require.NoError(t, err)
		assertLogsAnyOrder(t, logsPick(logs, 0, 1, 2, 3, 4), got)

		got, err = s.GetLogs(ctx, &port.LogFilter{Addresses: []common.Address{addrC}, FromBlock: 2, ToBlock: 3})
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 3, 4), got, "the range applies to the address filter")

		got, err = s.GetLogs(ctx, &port.LogFilter{Addresses: []common.Address{addrA}})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("GetLogsTopics", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		cases := []struct {
			name   string
			topics [][]common.Hash
			want   []int
		}{
			{"Topic0", [][]common.Hash{{logsApproval}}, []int{2, 3}},
			{"OrInsidePosition", [][]common.Hash{{transferTopic, logsApproval}}, []int{0, 1, 2, 3, 5}},
			{"AndAcrossPositions", [][]common.Hash{{transferTopic}, {logsTopicB}}, []int{0}},
			{"NilPositionIsAny", [][]common.Hash{nil, {logsTopicA}}, []int{1, 2}},
			{"EmptyPositionIsAny", [][]common.Hash{{}, {}, {logsTopicA}}, []int{0, 3}},
			{"PositionBeyondTopics", [][]common.Hash{{transferTopic}, nil, nil, {logsTopicA}}, nil},
			{"NoMatch", [][]common.Hash{{logsTopicA}}, nil},
		}
		for _, tc := range cases {
			got, err := s.GetLogs(ctx, &port.LogFilter{Topics: tc.topics})
			require.NoError(t, err, tc.name)
			assertLogsAnyOrder(t, logsPick(logs, tc.want...), got)
		}
	})

	t.Run("GetLogsAddressesAndTopics", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		got, err := s.GetLogs(ctx, &port.LogFilter{
			Addresses: []common.Address{addrC},
			Topics:    [][]common.Hash{{logsApproval}},
		})
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 3), got)

		got, err = s.GetLogs(ctx, &port.LogFilter{
			Addresses: []common.Address{addrC, logsAddrD},
			Topics:    [][]common.Hash{{transferTopic}, {logsTopicA}},
		})
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 1), got)
	})

	t.Run("GetLogsChainOrder", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		got, err := s.GetLogs(ctx, &port.LogFilter{Addresses: []common.Address{logsAddrD, addrC}})
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 0, 1, 2, 3, 4), got)

		got, err = s.GetLogs(ctx, &port.LogFilter{Topics: [][]common.Hash{{logsApproval, transferTopic}}})
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 0, 1, 2, 3, 5), got)
	})

	t.Run("GetLogsRepeatedOptionsReturnOnce", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		got, err := s.GetLogs(ctx, &port.LogFilter{Addresses: []common.Address{logsAddrD, logsAddrD}})
		require.NoError(t, err)
		assertLogsAnyOrder(t, logsPick(logs, 1, 2), got)

		got, err = s.GetLogs(ctx, &port.LogFilter{Topics: [][]common.Hash{{logsApproval, logsApproval}}})
		require.NoError(t, err)
		assertLogsAnyOrder(t, logsPick(logs, 2, 3), got)
	})

	t.Run("IndexIsIdempotent", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		require.NoError(t, s.IndexLogs(ctx, logs))
		require.NoError(t, s.IndexLog(ctx, logs[0]))

		got, err := s.GetLogs(ctx, &port.LogFilter{})
		require.NoError(t, err)
		assertLogsInOrder(t, logs, got)
		got, err = s.GetLogsByAddress(ctx, addrC, 0, 10)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 0, 3, 4), got)
		got, err = s.GetLogsByTopic(ctx, transferTopic, 0, 0, 10)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 0, 1, 5), got)
	})

	t.Run("ReindexReplacesLog", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		replaced := *logs[0]
		replaced.Data = []byte("replacement")
		require.NoError(t, s.IndexLog(ctx, &replaced))
		got, err := s.GetLogsByBlock(ctx, 1)
		require.NoError(t, err)
		assertLogsInOrder(t, []*model.Log{&replaced, logs[1]}, got)
	})

	t.Run("ReindexDropsStaleIndexes", func(t *testing.T) {
		s, logs := logsFixtureStore(t, newStore)
		moved := *logs[0]
		moved.Address = addrB
		moved.Topics = []common.Hash{logsApproval}
		require.NoError(t, s.IndexLog(ctx, &moved))

		got, err := s.GetLogsByAddress(ctx, addrC, 0, 10)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 3, 4), got)
		got, err = s.GetLogsByTopic(ctx, transferTopic, 0, 0, 10)
		require.NoError(t, err)
		assertLogsInOrder(t, logsPick(logs, 1, 5), got)
	})

	t.Run("IndexLogsEmpty", func(t *testing.T) {
		s := open[logStore](t, newStore)
		assert.NoError(t, s.IndexLogs(ctx, nil))
		assert.NoError(t, s.IndexLogs(ctx, []*model.Log{}))
	})

	t.Run("RejectsNil", func(t *testing.T) {
		s := open[logStore](t, newStore)
		assert.Error(t, s.IndexLog(ctx, nil))
		assert.Error(t, s.IndexLogs(ctx, []*model.Log{logsFixture()[0], nil}))
	})
}
