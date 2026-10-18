package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/indexingstatus"
	"github.com/0xmhha/indexer-go/pkg/projection"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func stateProofConfig() *evmstate.Config {
	return &evmstate.Config{Name: "watched-v1", StartHeight: 2, Accounts: []evmstate.AccountConfig{{Address: common.Address{19: 1}.Hex(), Code: true, Slots: []string{common.Hash{31: 3}.Hex()}}}}
}
func waitStateProofs(t *testing.T, a *App, n uint64) indexingstatus.Status {
	t.Helper()
	var out indexingstatus.Status
	require.Eventually(t, func() bool {
		var e error
		out, e = indexingstatus.Read(context.Background(), a.storage, "", 100)
		return e == nil && out.StateProofs != nil && out.StateProofs.Complete != nil && out.StateProofs.Complete.Height == n
	}, 25*time.Second, 20*time.Millisecond)
	return out
}
func TestManagedStateProofsRestartAndStatus(t *testing.T) {
	c := testchain.NewChain(5042002, nil)
	c.AddBlock() // no proof supply at height 1: configured scope starts at 2.
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.StateProofs = stateProofConfig()
	cfg.API.Enabled = true
	cfg.Source.Feed.Projections = []string{projection.Summary}
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	status := waitStateProofs(t, a, 3)
	waitFeedProjection(t, a, c, 3)
	require.Eventually(t, func() bool {
		s, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
		return e == nil && len(s.Workers) == 1 && s.Workers[0].Complete != nil && s.Workers[0].Complete.Height == 3
	}, 10*time.Second, 20*time.Millisecond)
	require.Equal(t, "caught_up", status.StateProofs.Status)
	require.Zero(t, status.StateProofs.Backlog)
	require.EqualValues(t, 2, status.StateProofs.Descriptor.FromHeight)
	require.EqualValues(t, 2, a.managedFeed.stateSupplier.Metrics().RPCBatch.Count)
	d := status.StateProofs.Descriptor
	var sel evmstate.Selection
	require.NoError(t, json.Unmarshal(d.Selection, &sel))
	src, e := facts.New(t.Context(), facts.Config{Store: a.managedFeed.store, Profile: a.managedFeed.program.Profile, Canonical: a.profileSrc, Consumer: "snapshot-test"})
	require.NoError(t, e)
	_, e = src.StateSnapshot(t.Context(), 1, sel)
	require.ErrorIs(t, e, facts.ErrCoverage)
	snapshot, e := src.StateSnapshot(t.Context(), 3, sel)
	require.NoError(t, e)
	require.False(t, snapshot.Accounts[0].Exists)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	before := rpc.Calls()
	w := httptest.NewRecorder()
	a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", "/indexing/status", nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var response indexingstatus.Status
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, status.StateProofs.Complete, response.StateProofs.Complete)
	require.Equal(t, before, rpc.Calls(), "status must not query proof or canonical RPC")
	if cfg.Database.Driver == "postgres" {
		readonly, e := openStore(t.Context(), &cfg.Database, true, nil, zap.NewNop())
		require.NoError(t, e)
		ro, e := indexingstatus.Read(t.Context(), readonly, "", 100)
		require.NoError(t, e)
		require.Equal(t, status.StateProofs.Complete, ro.StateProofs.Complete)
		require.NoError(t, readonly.Close())
	}
	a.Shutdown()
	rpc.PruneStateBefore(4)
	a, e = NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done = runFeedApp(t, a)
	beforeProof := rpc.Calls()["eth_getProof"]
	waitFeedProjection(t, a, c, 3)
	waitStateProofs(t, a, 3)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	waitStateProofs(t, a, 4)
	require.Equal(t, beforeProof+1, rpc.Calls()["eth_getProof"], "restart reuses stored earlier proofs even after pruning")
	require.EqualValues(t, 1, a.managedFeed.stateSupplier.Metrics().RPCBatch.Count)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	a.Shutdown()
	// A name cannot silently select a different scope on the next restart.
	cfg.Source.Feed.StateProofs.Accounts[0].Code = false
	_, e = NewApp(cfg, zap.NewNop(), false, "")
	require.Error(t, e)
}

func TestManagedStateProofsReorg(t *testing.T) {
	c := testchain.NewChain(5042002, nil)
	c.AddBlock()
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	cfg, _, _ := feedAppConfig(t, c)
	cfg.Source.Feed.StateProofs = stateProofConfig()
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	old := waitStateProofs(t, a, 3)
	c.Reorg(1)
	c.AddStateRootBlock(types.EmptyRootHash, []byte("branch-b"))
	c.AddStateRootBlock(types.EmptyRootHash, []byte("branch-b"))
	require.Eventually(t, func() bool {
		got, e := indexingstatus.Read(context.Background(), a.storage, "", 100)
		return e == nil && got.StateProofs != nil && got.StateProofs.Epoch > old.StateProofs.Epoch && got.StateProofs.Complete != nil && got.StateProofs.Complete.Hash == c.Block(3).Block.Hash()
	}, 30*time.Second, 20*time.Millisecond)
	waitFeedProjection(t, a, c, 3)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	a.Shutdown()
}

func TestManagedStateProofFailureDoesNotComplete(t *testing.T) {
	c := testchain.NewChain(5042002, nil)
	c.AddBlock()
	c.AddStateRootBlock(types.EmptyRootHash, nil)
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.StateProofs = stateProofConfig()
	rpc.PruneStateBefore(3)
	cfg.Source.Feed.Projections = []string{projection.Summary}
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	_, done := runFeedApp(t, a)
	select {
	case e = <-done:
		require.ErrorIs(t, e, evmstate.ErrUnavailable)
	case <-time.After(20 * time.Second):
		t.Fatal("failed proof task did not cancel/join managed tasks")
	}
	status, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
	require.NoError(t, e)
	require.Nil(t, status.StateProofs.Complete)
	require.EqualValues(t, 1, status.StateProofs.Backlog)
	id, e := a.managedFeed.store.CanonicalAt(t.Context(), status.Source.Generation, 2)
	require.NoError(t, e)
	_, e = a.managedFeed.store.ReadSupplement(t.Context(), status.Source.Generation, id, evmstate.Codec, status.StateProofs.Descriptor.SelectionHash)
	require.ErrorIs(t, e, port.ErrNotFound)
}

func TestStateProofConfigStrictAndPreflight(t *testing.T) {
	cfg, _, _ := feedAppConfig(t)
	cfg.Source.Feed.StateProofs = stateProofConfig()
	raw, e := yaml.Marshal(cfg.Source.Feed)
	require.NoError(t, e)
	var decoded = *cfg.Source.Feed
	require.NoError(t, yaml.Unmarshal(raw, &decoded))
	require.NoError(t, decoded.Validate())
	for _, typo := range []string{"start_heigth", "codez", "slotz"} {
		bad := string(raw)
		switch typo {
		case "start_heigth":
			bad = strings.Replace(bad, "start_height:", typo+":", 1)
		case "codez":
			bad = strings.Replace(bad, "code:", typo+":", 1)
		case "slotz":
			bad = strings.Replace(bad, "slots:", typo+":", 1)
		}
		require.Error(t, yaml.Unmarshal([]byte(bad), &decoded))
	}
	for _, bad := range []evmstate.Config{{Name: "valid", Accounts: stateProofConfig().Accounts}, {Name: "injected';--", StartHeight: 2, Accounts: stateProofConfig().Accounts}, {Name: "valid", StartHeight: 2, Accounts: []evmstate.AccountConfig{{Address: "0x123"}}}} {
		cfg.Source.Feed.StateProofs = &bad
		require.Error(t, preflightFeeds(cfg, false))
	}
}
