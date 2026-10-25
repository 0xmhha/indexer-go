package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/indexingstatus"
	"github.com/0xmhha/indexer-go/internal/progress"
	"github.com/0xmhha/indexer-go/internal/projection"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feed"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func waitStateChanges(t *testing.T, a *App, n uint64) indexingstatus.Status {
	t.Helper()
	var out indexingstatus.Status
	require.Eventually(t, func() bool {
		var e error
		out, e = indexingstatus.Read(context.Background(), a.storage, "", 100)
		return e == nil && out.StateChanges != nil && out.StateChanges.Complete != nil && out.StateChanges.Complete.Height == n
	}, 30*time.Second, 20*time.Millisecond)
	return out
}
func TestManagedStateProjectionRestartAPIAndReorg(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long: restarts a managed feed through a reorg")
	}
	c := testchain.NewChain(5042002, nil)
	for i := 0; i < 3; i++ {
		c.AddStateRootBlock(types.EmptyRootHash, nil)
	}
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.StateProofs = stateProofConfig()
	cfg.Source.Feed.StateProofs.Required = true
	cfg.Source.Feed.StateProofs.ProjectChanges = true
	cfg.Source.Feed.Projections = []string{projection.Summary}
	cfg.API.Enabled = true
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	status := waitStateChanges(t, a, 3)
	waitFeedProjection(t, a, c, 3)
	require.Equal(t, "caught_up", status.StateChanges.Status)
	require.Zero(t, status.StateChanges.Backlog)
	require.EqualValues(t, 2, status.StateChanges.Descriptor.Stream.FromHeight)
	events, e := indexingstatus.ReadStateEvents(t.Context(), a.storage, 0, 10)
	require.NoError(t, e)
	require.Len(t, events.Events, 2)
	require.EqualValues(t, 2, events.Next)
	old := events.Events[1].Input
	require.NotNil(t, old)
	current, e := indexingstatus.ReadState(t.Context(), a.storage)
	require.NoError(t, e)
	require.Equal(t, *old, *current.Head.Input)
	var values evmstate.Values
	require.NoError(t, json.Unmarshal(current.Head.Values, &values))
	require.Len(t, values.Accounts, 1)
	require.False(t, values.Accounts[0].Exists)
	block, e := indexingstatus.ReadStateChanges(t.Context(), a.storage, 2)
	require.NoError(t, e)
	var changes evmstate.Changes
	require.NoError(t, json.Unmarshal(block.Output.Changes, &changes))
	require.Empty(t, changes.Accounts, "empty transition must still advance durable completion")
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	// API reads perform no RPC, including historical inputs and events.
	before := rpc.Calls()
	for _, path := range []string{"/indexing/state/current", "/indexing/state/blocks/2", "/indexing/state/events?after=0&limit=1", fmt.Sprintf("/indexing/state/inputs/3/%s/%d", old.Block.Hash.Hex(), old.Generation)} {
		w := httptest.NewRecorder()
		a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 200, w.Code, path)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.True(t, json.Valid(w.Body.Bytes()))
	}
	require.Equal(t, before, rpc.Calls())
	// A valid-looking descriptor for another source plan must fail closed;
	// HTTP responses never expose the underlying DB/scope error details.
	kv := a.storage.(port.KV)
	saved, e := kv.Get(t.Context(), []byte(progress.DescriptorKey))
	require.NoError(t, e)
	var corrupt model.ProjectionDescriptor
	require.NoError(t, json.Unmarshal(saved, &corrupt))
	corrupt.Plan.Version++
	badDescriptor, e := json.Marshal(corrupt)
	require.NoError(t, e)
	require.NoError(t, kv.Put(t.Context(), []byte(progress.DescriptorKey), badDescriptor))
	faultResponse := httptest.NewRecorder()
	a.apiServer.Router().ServeHTTP(faultResponse, httptest.NewRequest("GET", "/indexing/state/current", nil))
	require.Equal(t, 500, faultResponse.Code)
	require.JSONEq(t, `{"error":"state_projection_error"}`, faultResponse.Body.String())
	require.NoError(t, kv.Put(t.Context(), []byte(progress.DescriptorKey), saved))
	for _, path := range []string{"/indexing/state/blocks/invalid", "/indexing/state/events?limit=257", "/indexing/state/events?after=18446744073709551616", "/indexing/state/inputs/3/invalid/1"} {
		w := httptest.NewRecorder()
		a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 400, w.Code, path)
	}
	if cfg.Database.Driver == "postgres" {
		ro, e := openStore(t.Context(), &cfg.Database, true, nil, zap.NewNop())
		require.NoError(t, e)
		result, e := indexingstatus.ReadState(t.Context(), ro)
		require.NoError(t, e)
		require.Equal(t, current.Head, result.Head)
		_, e = indexingstatus.ReadStateEvents(t.Context(), ro, 0, 10)
		require.NoError(t, e)
		require.NoError(t, ro.Close())
	}
	a.Shutdown()
	rpc.PruneStateBefore(4)
	a, e = NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done = runFeedApp(t, a)
	count := rpc.Calls()["eth_getProof"]
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	waitStateChanges(t, a, 4)
	require.Equal(t, count+1, rpc.Calls()["eth_getProof"])
	// Real P2P/source epoch change replaces both proof and state worker's fork.
	rpc.PruneStateBefore(0)
	c.Reorg(2)
	c.AddStateRootBlock(types.EmptyRootHash, []byte("state branch"))
	c.AddStateRootBlock(types.EmptyRootHash, []byte("state branch"))
	require.EventuallyWithT(t, func(tick *assert.CollectT) {
		s, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
		if !assert.NoError(tick, e) || !assert.NotNil(tick, s.StateChanges) {
			return
		}
		assert.Greater(tick, s.StateChanges.Epoch, status.StateChanges.Epoch)
		if assert.NotNil(tick, s.StateChanges.Complete, "state projection: %+v", s.StateChanges) {
			assert.Equal(tick, c.Block(4).Block.Hash(), s.StateChanges.Complete.Hash, "state projection: %+v", s.StateChanges)
		}
	}, 35*time.Second, 20*time.Millisecond)
	waitFeedProjection(t, a, c, 4)
	events, e = indexingstatus.ReadStateEvents(t.Context(), a.storage, 0, 30)
	require.NoError(t, e)
	kinds := []string{}
	for _, event := range events.Events {
		kinds = append(kinds, event.Kind)
	}
	require.Equal(t, []string{"apply", "apply", "apply", "revert", "revert", "reset", "apply", "apply"}, kinds)
	historical, e := indexingstatus.ReadStateInput(t.Context(), a.storage, old.Block.Number, old.Generation, old.Block.Hash)
	require.NoError(t, e)
	require.Equal(t, *old, historical.Identity)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
func TestManagedStateProjectionProofFailureDoesNotAdvance(t *testing.T) {
	c := testchain.NewChain(5042002, nil)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.StateProofs = stateProofConfig()
	cfg.Source.Feed.StateProofs.Required = true
	cfg.Source.Feed.StateProofs.ProjectChanges = true
	cfg.Source.Feed.Projections = []string{projection.Summary}
	rpc.PruneStateBefore(3)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	_, done := runFeedApp(t, a)
	select {
	case e = <-done:
		require.ErrorIs(t, e, evmstate.ErrUnavailable)
	case <-time.After(25 * time.Second):
		t.Fatal("proof failure did not join five tasks")
	}
	s, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
	require.NoError(t, e)
	require.Nil(t, s.StateChanges.Complete)
	events, e := indexingstatus.ReadStateEvents(t.Context(), a.storage, 0, 10)
	require.NoError(t, e)
	require.Empty(t, events.Events)
	_, e = indexingstatus.ReadState(t.Context(), a.storage)
	require.Error(t, e)
}
func TestStateProjectionConfig(t *testing.T) {
	cfg, _, _ := feedAppConfig(t)
	cfg.Source.Feed.StateProofs = stateProofConfig()
	cfg.Source.Feed.StateProofs.ProjectChanges = true
	d, e := cfg.Source.Feed.StateProofs.Descriptor(mustFeedAnchor(t, cfg.Source.Feed))
	require.NoError(t, e)
	require.True(t, d.ParentCheckpoint)
	raw, e := yaml.Marshal(cfg.Source.Feed)
	require.NoError(t, e)
	decoded := *cfg.Source.Feed
	require.NoError(t, yaml.Unmarshal(raw, &decoded))
	require.True(t, decoded.StateProofs.ProjectChanges)
	require.Error(t, yaml.Unmarshal([]byte(strings.Replace(string(raw), "project_changes:", "project_changez:", 1)), &decoded))

}

func mustFeedAnchor(t *testing.T, c *feed.Config) model.FeedBlock {
	t.Helper()
	b, e := c.Identity()
	require.NoError(t, e)
	return b
}
