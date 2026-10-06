package jsonrpc

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// tipSwapStore replaces the tip block with another block right after the
// first log read, as a reorganization committing during a poll would, and
// records the tip hash each log read saw.
type tipSwapStore struct {
	*storage.PebbleStorage
	t       *testing.T
	swapped bool
	seen    []common.Hash
}

func (s *tipSwapStore) GetLogs(ctx context.Context, f *port.LogFilter) ([]*types.Log, error) {
	logs, err := s.PebbleStorage.GetLogs(ctx, f)
	h, _ := hashAt(ctx, s.PebbleStorage, 2)
	s.seen = append(s.seen, h)
	if !s.swapped {
		s.swapped = true
		require.NoError(s.t, s.SetModelBlock(ctx, testModelBlock(2, 0xbb)))
	}
	return logs, err
}

func testModelBlock(n uint64, salt byte) *model.Block {
	return &model.Block{
		Number:     n,
		Hash:       common.BytesToHash([]byte{byte(n), salt}),
		ParentHash: common.BytesToHash([]byte{byte(n - 1), 0xaa}),
		Time:       1700000000 + n,
		GasLimit:   30_000_000,
		BaseFee:    big.NewInt(1),
	}
}

// TestFilterPollRecordsTheTipItRead: the hash a poll records must be the
// one of the chain its logs were read from. Recording the tip after the
// read could pair old logs with a replacement block, and the next poll
// would then miss both the removed logs and the replacement's logs.
func TestFilterPollRecordsTheTipItRead(t *testing.T) {
	db, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	for n := uint64(0); n <= 2; n++ {
		require.NoError(t, db.SetModelBlock(ctx, testModelBlock(n, 0xaa)))
	}
	require.NoError(t, db.SetLatestHeight(ctx, 2))

	store := &tipSwapStore{PebbleStorage: db, t: t}
	fm := NewFilterManager(ctx, time.Minute)
	defer fm.Close()
	id := fm.NewFilter(LogFilterType, &port.LogFilter{}, 0, false)

	_, height, hash, err := fm.GetLogsSinceLastPoll(ctx, store, id)
	require.NoError(t, err)
	require.Equal(t, uint64(2), height)
	require.Equal(t, store.seen[len(store.seen)-1], hash, "recorded tip differs from the tip the logs were read at")
	require.Equal(t, testModelBlock(2, 0xbb).Hash, hash)
}
