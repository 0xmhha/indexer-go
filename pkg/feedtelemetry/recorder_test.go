package feedtelemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T) (*Recorder, model.CaptureIdentity) {
	t.Helper()
	p := model.CapturePlan{Version: 1, Required: []model.DatasetRequirement{{Kind: model.DatasetHeaders, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}}}}
	b := model.FeedBlock{Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, Hash: common.Hash{1}}
	r, e := New(b, p, 0)
	require.NoError(t, e)
	t.Cleanup(r.Close)
	d, _ := p.Digest()
	return r, model.CaptureIdentity{Block: b, Generation: 1, PlanVersion: 1, PlanDigest: d}
}
func TestTelemetryBoundsMissingAndScope(t *testing.T) {
	r, id := fixture(t)
	d := r.Descriptor()
	initial := Read(d, r.anchor, r.plan)
	require.Equal(t, "available", initial.Availability)
	require.Nil(t, initial.Metrics["captureCommit"].P95MS)
	require.Equal(t, "unmeasured", initial.ReceiveAssessment)
	for n := 1; n <= Samples+20; n++ {
		r.Record(CaptureCommit, time.Duration(n)*time.Millisecond)
	}
	m := Read(d, r.anchor, r.plan).Metrics["captureCommit"]
	require.EqualValues(t, Samples+20, m.Count)
	require.Equal(t, Samples, m.RecentSamples)
	require.Equal(t, float64(264), *m.P95MS)
	id.Block.Number = 1
	id.Block.Hash = common.Hash{2}
	r.Complete(Captured, id)
	r.Complete(Captured, id)
	require.NotNil(t, PendingAge(d, r.anchor, r.plan, Captured, 1, 1))
	require.Nil(t, PendingAge(d, r.anchor, r.plan, Captured, 2, 1))
	r.QueueAge(Captured, ProofQueue, id, time.Now())
	require.EqualValues(t, 1, Read(d, r.anchor, r.plan).Metrics["proofQueue"].Count)
	other := id
	other.Generation++
	r.QueueAge(Captured, ProofQueue, other, time.Now())
	require.EqualValues(t, 1, Read(d, r.anchor, r.plan).Metrics["proofQueue"].Count)
	for n := 2; n <= Points+2; n++ {
		id.Block.Number = uint64(n)
		id.Block.Hash = common.HexToHash(fmt.Sprintf("0x%x", n))
		r.Complete(Captured, id)
	}
	require.Len(t, r.progress[Captured].entries, Points)
	require.Nil(t, PendingAge(d, r.anchor, r.plan, Captured, 1, 1))
	require.Equal(t, "runtime_unavailable", Read(d, model.FeedBlock{}, r.plan).Availability)
	require.Equal(t, "runtime_unavailable", Read(d, r.anchor, common.Hash{}).Availability)
	require.Equal(t, "disabled", Read(nil, r.anchor, r.plan).Availability)
	r.Stop()
	require.Equal(t, "stopped", Read(d, r.anchor, r.plan).Availability)
	r.Close()
	v := Read(d, r.anchor, r.plan)
	require.Equal(t, "runtime_unavailable", v.Availability)
	require.Empty(t, v.Metrics)
	require.Empty(t, v.Progress)
}
func TestTelemetryConcurrentLifecycle(t *testing.T) {
	r, id := fixture(t)
	ctx := WithRecorder(context.Background(), r)
	require.Same(t, r, From(ctx))
	require.Nil(t, From(context.Background()))
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				s := From(ctx).Start(RPCHTTP)
				_ = Read(r.Descriptor(), r.anchor, r.plan)
				s.End(errors.New("fixture"))
				r.Complete(Proved, id)
			}
		}()
	}
	wg.Wait()
	v := Read(r.Descriptor(), r.anchor, r.plan)
	require.EqualValues(t, 200, v.Metrics["rpcHTTP"].Count)
	require.EqualValues(t, 200, v.Metrics["rpcHTTP"].Failures)
	require.Zero(t, v.Metrics["rpcHTTP"].InFlight)
	span := r.Start(StateApply)
	require.Equal(t, 1, Read(r.Descriptor(), r.anchor, r.plan).Metrics["stateApply"].InFlight)
	span.Discard()
	require.Zero(t, Read(r.Descriptor(), r.anchor, r.plan).Metrics["stateApply"].Count)
	s := (*Recorder)(nil).Start(CaptureCommit)
	s.End(nil)
	s.Discard()
	(*Recorder)(nil).Stop()
	(*Recorder)(nil).Close()
}
