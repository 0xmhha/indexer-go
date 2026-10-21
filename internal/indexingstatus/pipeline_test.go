package indexingstatus

import (
	"github.com/0xmhha/indexer-go/internal/feedtelemetry"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPipelineLagEpochAndUnknown(t *testing.T) {
	plan := model.CapturePlan{Version: 1, Required: []model.DatasetRequirement{{Kind: model.DatasetHeaders, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}}}}
	digest, _ := plan.Digest()
	anchor := model.FeedBlock{Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, Hash: common.Hash{1}}
	r, e := feedtelemetry.New(anchor, plan, 0)
	require.NoError(t, e)
	defer r.Close()
	proof := model.SupplementStreamDescriptor{FromHeight: 2}
	changes := model.StateProjectionDescriptor{Stream: proof}
	d := model.ProjectionDescriptor{Anchor: anchor, Plan: plan, Telemetry: r.Descriptor(), StateProofs: &proof, StateChanges: &changes}
	cap := capture.StatusSnapshot{State: capture.State{Anchor: anchor, Plan: plan, Generation: 1}, StateProofs: &capture.SupplementStatus{Epoch: 1, Status: "lagging", Backlog: 3, Complete: &model.BlockPoint{Height: 2}}, StateChanges: &capture.StateProjectionStatus{Epoch: 1, Status: "lagging", Backlog: 4, Complete: &model.BlockPoint{Height: 1}}}
	id := model.CaptureIdentity{Block: model.FeedBlock{Chain: anchor.Chain, Number: 3, Hash: common.Hash{3}}, Generation: 1, PlanVersion: 1, PlanDigest: digest}
	r.Complete(feedtelemetry.Captured, id)
	id.Block.Number = 2
	id.Block.Hash = common.Hash{2}
	r.Complete(feedtelemetry.Proved, id)
	snap := feedtelemetry.Read(d.Telemetry, anchor, digest)
	p := readPipeline(cap, d, snap)
	require.EqualValues(t, 3, *p.ProofBacklog)
	require.EqualValues(t, 4, *p.StateBacklog)
	require.EqualValues(t, 1, *p.ReadyStateBacklog)
	require.NotNil(t, p.ProofOldestPendingAgeMS)
	require.NotNil(t, p.StateOldestReadyAgeMS)
	require.Nil(t, p.NetworkHead)
	cap.State.Generation = 2
	cap.StateProofs.Status = "stale"
	cap.StateChanges.Status = "stale"
	p = readPipeline(cap, d, feedtelemetry.Read(d.Telemetry, anchor, digest))
	require.Nil(t, p.ProofBacklog)
	require.Nil(t, p.StateBacklog)
	require.Nil(t, p.ReadyStateBacklog)
	require.Nil(t, p.ProofOldestPendingAgeMS)
	require.Nil(t, p.StateOldestReadyAgeMS)
	require.Empty(t, p.Runtime.Progress)
	r.Close()
	cap.State.Generation = 1
	cap.StateProofs.Status = "lagging"
	cap.StateChanges.Status = "lagging"
	p = readPipeline(cap, d, feedtelemetry.Read(d.Telemetry, anchor, digest))
	require.Equal(t, "runtime_unavailable", p.Runtime.Availability)
	require.EqualValues(t, 3, *p.ProofBacklog)
	require.Nil(t, p.ProofOldestPendingAgeMS)
}
