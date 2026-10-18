package progress_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/progress"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestCompletionRangesPreserveHolesAndDefinitions(t *testing.T) {
	db, e := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, e)
	defer func() { require.NoError(t, db.Close()) }()
	ctx := context.Background()
	ch := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	anchor := model.FeedBlock{Chain: ch, Hash: ch.GenesisHash}
	record := func(n uint64, def string, applied bool) {
		t.Helper()
		in := model.ProjectionInput{Epoch: 1, Anchor: anchor, Identity: model.CaptureIdentity{Block: model.FeedBlock{Chain: ch, Number: n, Hash: common.Hash{byte(n + 1)}}, Generation: 1, PlanVersion: 1, PlanDigest: common.Hash{9}}}
		require.NoError(t, progress.Record(ctx, db, "a'; DROP TABLE kv; --", def, in, applied))
	}
	read := func() model.UnitCheckpoint {
		b, e := db.Get(ctx, progress.UnitKey("a'; DROP TABLE kv; --"))
		require.NoError(t, e)
		var c model.UnitCheckpoint
		require.NoError(t, json.Unmarshal(b, &c))
		return c
	}
	record(3, "v1", true)
	record(1, "v1", true)
	require.Len(t, read().Ranges, 2)
	record(2, "v1", true)
	c := read()
	require.Len(t, c.Ranges, 1)
	require.EqualValues(t, 1, c.Ranges[0].From)
	require.EqualValues(t, 3, c.Ranges[0].To)
	record(3, "v2", false)
	require.Equal(t, "v1", read().Definition, "duplicate cannot mark a new definition processed")
	record(2, "v2", true)
	c = read()
	require.Equal(t, "v2", c.Definition)
	require.EqualValues(t, 2, c.Ranges[0].From)
	record(1, "v2", true)
	require.EqualValues(t, 1, read().Ranges[0].From)
}
func TestReadPageIsBoundedAndNeverInfersLegacyCompletion(t *testing.T) {
	db, e := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, e)
	defer func() { require.NoError(t, db.Close()) }()
	ctx := context.Background()
	for _, n := range []string{"a", "b';--", "c"} {
		require.NoError(t, db.SetFeatureState(ctx, n, port.FeatureState{Active: true, Through: 999}))
	}
	p, e := db.ReadIndexingPage(ctx, "", 2)
	require.NoError(t, e)
	require.Len(t, p.Units, 2)
	require.Equal(t, "b';--", p.Next)
	require.Nil(t, p.Units[0].Checkpoint)
	p, e = db.ReadIndexingPage(ctx, p.Next, 2)
	require.NoError(t, e)
	require.Len(t, p.Units, 1)
	require.Equal(t, "c", p.Units[0].Name)
	require.Empty(t, p.Next)
	_, e = db.ReadIndexingPage(ctx, "", 101)
	require.ErrorIs(t, e, port.ErrInvalidData)
}
