package capture_test

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func stateDescriptor() model.StateProjectionDescriptor {
	return model.StateProjectionDescriptor{Version: 1, Name: "tracked." + requiredDescriptor().Name, Stream: requiredDescriptor()}
}
func stateDerive(_ model.StateProjectionDescriptor, before, after capture.SupplementCheckpoint) (capture.StateProjectionValues, error) {
	a, _ := json.Marshal(map[string]string{"value": before.Block.Hash.Hex()})
	b, _ := json.Marshal(map[string]string{"value": after.Block.Hash.Hex()})
	changes, _ := json.Marshal(map[string]model.FeedBlock{"before": before.Block, "after": after.Block})
	return capture.StateProjectionValues{Before: a, After: b, Changes: changes}, nil
}
func prepareStateProjection(t *testing.T, db port.CaptureDatabase) (*capture.Store, *oracle, model.StateProjectionDescriptor, []capture.Input) {
	t.Helper()
	o := newOracle()
	s := service(t, db, o).WithSupplementVerifier(fixtureVerifier)
	initialize(t, s)
	d := stateDescriptor()
	require.NoError(t, s.BindRequiredSupplement(ctx, &d.Stream))
	_, e := s.InitializeSupplementStream(ctx, d.Stream)
	require.NoError(t, e)
	_, e = s.InitializeStateProjection(ctx, d)
	require.NoError(t, e)
	blocks := []capture.Input{input(1, 1, common.Hash{2}, chain.GenesisHash), input(2, 1, common.Hash{3}, common.Hash{2}), input(3, 1, common.Hash{4}, common.Hash{3})}
	for _, b := range blocks {
		require.NoError(t, s.Commit(ctx, b))
	}
	require.NoError(t, s.CommitSupplementCheckpoint(ctx, 1, d.Stream, checkpointFixture(blocks[0].Manifest.Identity.Block)))
	for _, b := range blocks[1:] {
		require.NoError(t, s.CommitStreamSupplement(ctx, 1, d.Stream, streamSupplement(d.Stream, b.Manifest.Identity)))
	}
	return s, o, d, blocks
}
func TestStateProjectionAtomicRetryAndUndo(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			open := openFactory(t, backend)
			db := open()
			s, o, d, blocks := prepareStateProjection(t, db)
			b, c := blocks[1].Manifest.Identity, blocks[2].Manifest.Identity
			require.ErrorIs(t, s.ApplyStateProjection(ctx, 1, d, c, stateDerive), capture.ErrGap)
			for n := 1; n <= 5; n++ {
				broken := service(t, failDB{CaptureDatabase: db, at: n}, o)
				require.ErrorIs(t, broken.ApplyStateProjection(ctx, 1, d, b, stateDerive), errInjected)
				status, e := s.StateProjectionStatus(ctx, d)
				require.NoError(t, e)
				require.Nil(t, status.Complete)
				require.Zero(t, status.Sequence)
				events, e := capture.ReadStateProjectionEvents(ctx, db, chain, d, 0, 10)
				require.NoError(t, e)
				require.Empty(t, events)
				_, _, e = capture.ReadStateProjectionHead(ctx, db, chain, d)
				require.ErrorIs(t, e, port.ErrNotFound)
			}
			o.fork(2, common.Hash{99})
			require.ErrorIs(t, s.ApplyStateProjection(ctx, 1, d, b, stateDerive), capture.ErrCanonical)
			o.fork(2, b.Block.Hash)
			require.ErrorIs(t, service(t, uncertainDB{db}, o).ApplyStateProjection(ctx, 1, d, b, stateDerive), errInjected)
			require.NoError(t, s.ApplyStateProjection(ctx, 1, d, b, stateDerive))
			require.NoError(t, s.ApplyStateProjection(ctx, 1, d, c, stateDerive))
			head, epoch, e := capture.ReadStateProjectionHead(ctx, db, chain, d)
			require.NoError(t, e)
			require.EqualValues(t, 1, epoch)
			require.Equal(t, c, *head.Input)
			for _, missing := range []string{"head", "output/", "events/"} {
				bad := stateReadFaultDB{CaptureDatabase: db, part: missing}
				switch missing {
				case "events/":
					_, e = capture.ReadStateProjectionEvents(ctx, bad, chain, d, 0, 10)
				case "head":
					_, _, e = capture.ReadStateProjectionHead(ctx, bad, chain, d)
				default:
					_, _, e = capture.ReadStateProjectionOutput(ctx, bad, chain, d, 2)
				}
				require.ErrorIs(t, e, capture.ErrConflict, "claimed completion with missing bytes is corruption")
			}

			source, e := s.State(ctx)
			require.NoError(t, e)
			require.EqualValues(t, 3, source.Sequence, "state worker owns no base facts events")
			require.NoError(t, db.Close())
			db = open()
			defer func() { require.NoError(t, db.Close()) }()
			s = service(t, db, o)
			cursor, e := s.InitializeStateProjection(ctx, d)
			require.NoError(t, e)
			require.Equal(t, c, *cursor.Complete)
			require.NoError(t, s.Reset(ctx, 1, blocks[0].Manifest.Identity.Block))
			_, _, e = capture.ReadStateProjectionHead(ctx, db, chain, d)
			require.ErrorIs(t, e, capture.ErrStale)
			for n := 1; n <= 5; n++ {
				broken := service(t, failDB{CaptureDatabase: db, at: n}, o)
				_, e = broken.InitializeStateProjection(ctx, d)
				require.ErrorIs(t, e, errInjected)
				events, e := capture.ReadStateProjectionEvents(ctx, db, chain, d, 0, 20)
				require.NoError(t, e)
				require.Len(t, events, 2)
			}
			cursor, e = s.InitializeStateProjection(ctx, d)
			require.NoError(t, e)
			require.Nil(t, cursor.Complete)
			require.EqualValues(t, 2, cursor.Epoch)
			events, e := capture.ReadStateProjectionEvents(ctx, db, chain, d, 0, 20)
			require.NoError(t, e)
			require.Len(t, events, 5)
			require.Equal(t, []string{"apply", "apply", "revert", "revert", "reset"}, []string{events[0].Kind, events[1].Kind, events[2].Kind, events[3].Kind, events[4].Kind})
			require.Equal(t, c, *events[2].Input)
			require.Equal(t, b, *events[3].Input)
			old, e := capture.ReadStateProjectionInput(ctx, db, chain, d, c)
			require.NoError(t, e)
			require.Equal(t, c, old.Identity)
			_, e = s.InitializeSupplementStream(ctx, d.Stream)
			require.NoError(t, e)
			o.fork(2, common.Hash{33})
			branch := input(2, 2, common.Hash{33}, blocks[0].Manifest.Identity.Block.Hash)
			require.NoError(t, s.Commit(ctx, branch))
			require.NoError(t, s.WithSupplementVerifier(fixtureVerifier).CommitStreamSupplement(ctx, 2, d.Stream, streamSupplement(d.Stream, branch.Manifest.Identity)))
			require.NoError(t, s.ApplyStateProjection(ctx, 2, d, branch.Manifest.Identity, stateDerive))
			head, epoch, e = capture.ReadStateProjectionHead(ctx, db, chain, d)
			require.NoError(t, e)
			require.EqualValues(t, 2, epoch)
			require.Equal(t, branch.Manifest.Identity, *head.Input)
			out, _, e := capture.ReadStateProjectionOutput(ctx, db, chain, d, 2)
			require.NoError(t, e)
			require.Equal(t, branch.Manifest.Identity, out.Identity)
			changed := d
			changed.Stream.FromHeight = 3
			_, e = s.InitializeStateProjection(ctx, changed)
			require.ErrorIs(t, e, capture.ErrConflict)
		})
	}
}
func TestStateProjectionResetRace(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s, _, d, blocks := prepareStateProjection(t, db)
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-start
		done <- s.ApplyStateProjection(context.Background(), 1, d, blocks[1].Manifest.Identity, stateDerive)
	}()
	close(start)
	require.NoError(t, s.Reset(ctx, 1, blocks[0].Manifest.Identity.Block))
	e := <-done
	if e != nil {
		require.ErrorIs(t, e, capture.ErrStale)
	}
	_, _, e = capture.ReadStateProjectionHead(ctx, db, chain, d)
	if e != port.ErrNotFound {
		require.ErrorIs(t, e, capture.ErrStale)
	}
	c, e := s.InitializeStateProjection(ctx, d)
	require.NoError(t, e)
	require.Nil(t, c.Complete)
}

func TestStateProjectionUndoBoundDoesNotPartiallyReset(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			s, o, d, blocks := prepareStateProjection(t, db)
			for _, in := range blocks[1:] {
				require.NoError(t, s.ApplyStateProjection(ctx, 1, d, in.Manifest.Identity, stateDerive))
			}
			previous := blocks[2].Manifest.Identity.Block.Hash
			for n := uint64(4); n <= capture.MaxStateUndo+2; n++ {
				hash := common.BigToHash(new(big.Int).SetUint64(n + 1000))
				o.fork(n, hash)
				in := input(n, 1, hash, previous)
				require.NoError(t, s.Commit(ctx, in))
				require.NoError(t, s.CommitStreamSupplement(ctx, 1, d.Stream, streamSupplement(d.Stream, in.Manifest.Identity)))
				require.NoError(t, s.ApplyStateProjection(ctx, 1, d, in.Manifest.Identity, stateDerive))
				previous = hash
			}
			old, e := s.StateProjectionStatus(ctx, d)
			require.NoError(t, e)
			require.EqualValues(t, capture.MaxStateUndo+1, old.Sequence)
			require.NoError(t, s.Reset(ctx, 1, blocks[0].Manifest.Identity.Block))
			_, e = s.InitializeStateProjection(ctx, d)
			require.ErrorIs(t, e, capture.ErrRebuildRequired)
			events, e := capture.ReadStateProjectionEvents(ctx, db, chain, d, 0, 256)
			require.NoError(t, e)
			require.Len(t, events, capture.MaxStateUndo+1)
			for _, ev := range events {
				require.Equal(t, "apply", ev.Kind)
			}
			status, e := s.StateProjectionStatus(ctx, d)
			require.NoError(t, e)
			require.Equal(t, "stale", status.Status)
			require.Nil(t, status.Complete)
			require.Equal(t, old.Sequence, status.Sequence)
		})
	}
}

type stateReadFaultDB struct {
	port.CaptureDatabase
	part string
}
type stateReadFaultReader struct {
	port.CaptureReader
	part string
}

func (r stateReadFaultReader) Get(k []byte) ([]byte, error) {
	if strings.HasPrefix(string(k), "state-projection/") && strings.Contains(string(k), "/"+r.part) {
		return nil, port.ErrNotFound
	}
	return r.CaptureReader.Get(k)
}
func (d stateReadFaultDB) CaptureView(ctx context.Context, ch model.FeedChain, fn func(port.CaptureReader) error) error {
	return d.CaptureDatabase.CaptureView(ctx, ch, func(r port.CaptureReader) error { return fn(stateReadFaultReader{r, d.part}) })
}
