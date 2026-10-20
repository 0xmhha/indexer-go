package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/feed"
	"github.com/0xmhha/indexer-go/pkg/feedtelemetry"
	"github.com/0xmhha/indexer-go/pkg/indexingstatus"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestManagedFeedTelemetryReadonlyRestart(t *testing.T) {
	c := testchain.NewChain(5042002, nil)
	for i := 0; i < 3; i++ {
		c.AddStateRootBlock(types.EmptyRootHash, nil)
	}
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.StateProofs = stateProofConfig()
	cfg.Source.Feed.StateProofs.ProjectChanges = true
	cfg.Source.Feed.Telemetry = &feed.TelemetryConfig{ReceiveIntervalTarget: 450 * time.Millisecond}
	cfg.API.Enabled = true
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	status := waitStateChanges(t, a, 3)
	waitFeedProjection(t, a, c, 3)
	require.Eventually(t, func() bool {
		status, e = indexingstatus.Read(t.Context(), a.storage, "", 100)
		return e == nil && status.Pipeline != nil && status.Pipeline.Runtime.Progress["projected"].Input.Block.Number == 3
	}, time.Second, time.Millisecond)
	p := status.Pipeline
	require.Nil(t, p.NetworkHead)
	require.Zero(t, *p.ProofBacklog)
	require.Zero(t, *p.StateBacklog)
	require.Zero(t, *p.ReadyStateBacklog)
	require.Equal(t, "available", p.Runtime.Availability)
	require.NotNil(t, p.Runtime.Observed)
	require.EqualValues(t, 3, p.Runtime.Observed.Block.Number)
	for _, name := range []string{"receiveNext", "rawAppend", "assemble", "captureCommit", "proofAcquire", "proofBatch", "proofVerify", "proofCommit", "stateApply", "rpcHTTP", "rpcLimitWait"} {
		require.Positive(t, p.Runtime.Metrics[name].Count, name)
		require.NotNil(t, p.Runtime.Metrics[name].P95MS, name)
	}
	require.EqualValues(t, 3, p.Runtime.Metrics["captureCommit"].Count)
	in := p.Runtime.RawIntake
	require.NotNil(t, in)
	require.EqualValues(t, 3, in.ReceivedThrough)
	require.Zero(t, in.PendingBlocks)
	require.Nil(t, in.OldestPending)
	require.Positive(t, in.AppendedFrames)
	require.Positive(t, in.AppendedBytes)
	// These independent run-time observations may race a downstream DB reader;
	// absent exact queue timing remains null rather than fabricating zero.
	require.Nil(t, p.ProofOldestPendingAgeMS)
	require.Nil(t, p.StateOldestReadyAgeMS)
	report, e := json.Marshal(p)
	require.NoError(t, e)
	t.Logf("local fixture pipeline: %s", report)
	savedID := p.Runtime.RunID
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	// The shared RPC dispatcher drains its already-reserved limiter slot
	// independently of cancelled callers; do not label that interval idle.
	require.Eventually(t, func() bool {
		s, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
		if e != nil {
			return false
		}
		for _, m := range s.Pipeline.Runtime.Metrics {
			if m.InFlight != 0 {
				return false
			}
		}
		return true
	}, 2*time.Second, time.Millisecond)
	before := rpc.Calls()
	source, e := a.managedFeed.store.State(t.Context())
	require.NoError(t, e)
	response := httptest.NewRecorder()
	a.apiServer.Router().ServeHTTP(response, httptest.NewRequest("GET", "/indexing/status", nil))
	require.Equal(t, 200, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var out indexingstatus.Status
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &out))
	require.Equal(t, "stopped", out.Pipeline.Runtime.Availability)
	require.Equal(t, before, rpc.Calls())
	after, e := a.managedFeed.store.State(t.Context())
	require.NoError(t, e)
	require.Equal(t, source, after)
	for name, m := range out.Pipeline.Runtime.Metrics {
		require.Zero(t, m.InFlight, name)
	}
	if cfg.Database.Driver == "postgres" {
		ro, e := openStore(t.Context(), &cfg.Database, true, nil, zap.NewNop())
		require.NoError(t, e)
		readonly, e := indexingstatus.Read(t.Context(), ro, "", 100)
		require.NoError(t, e)
		require.Equal(t, "stopped", readonly.Pipeline.Runtime.Availability)
		require.Equal(t, source.Generation, readonly.Source.Generation)
		require.NoError(t, ro.Close())
	}
	a.Shutdown()
	require.Equal(t, "runtime_unavailable", feedtelemetry.Read(&model.FeedTelemetryDescriptor{RunID: savedID}, source.Anchor, out.Source.PlanDigest).Availability)
	a, e = NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	t.Cleanup(a.Shutdown)
	out, e = indexingstatus.Read(t.Context(), a.storage, "", 100)
	require.NoError(t, e)
	require.NotEqual(t, savedID, out.Pipeline.Runtime.RunID)
	require.Empty(t, out.Pipeline.Runtime.Progress)
	require.Nil(t, out.Pipeline.Runtime.Metrics["captureCommit"].P95MS)
	cancel, done = runFeedApp(t, a)
	defer cancel()
	waitStateChanges(t, a, 3)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
func TestManagedFeedTelemetryValidation(t *testing.T) {
	cfg, _, _ := feedAppConfig(t, testchain.NewChain(5042002, nil))
	for _, d := range []time.Duration{-1, time.Minute + 1} {
		cfg.Source.Feed.Telemetry = &feed.TelemetryConfig{ReceiveIntervalTarget: d}
		require.Error(t, cfg.Source.Feed.Validate())
	}
	cfg.Source.Feed.Telemetry = &feed.TelemetryConfig{}
	require.NoError(t, cfg.Source.Feed.Validate())
	var v feed.Config
	require.Error(t, yaml.Unmarshal([]byte("telemetry:\n  unknown: true\n"), &v))
	for _, d := range []*model.FeedTelemetryDescriptor{{RunID: "wrong", ReceiveIntervalTargetMS: 450}, {RunID: "00112233445566778899aabbccddeeff", ReceiveIntervalTargetMS: -1}} {
		require.Error(t, (model.ProjectionDescriptor{Telemetry: d}).ValidateTelemetry())
	}
}
