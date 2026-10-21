package feedtelemetry

import (
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func intakeFixture(t *testing.T) (*Recorder, model.CaptureIdentity) {
	t.Helper()
	complete := model.DatasetCoverage{Scope: model.CoverageComplete}
	p := model.CapturePlan{Version: 1, Required: []model.DatasetRequirement{{Kind: model.DatasetHeaders, Coverage: complete}, {Kind: model.DatasetBodies, Coverage: complete}}}
	anchor := model.FeedBlock{Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, Hash: common.Hash{1}}
	r, err := New(anchor, p, 0)
	require.NoError(t, err)
	t.Cleanup(r.Close)
	d, _ := p.Digest()
	return r, model.CaptureIdentity{Block: anchor, Generation: 1, PlanVersion: 1, PlanDigest: d}
}

func intakeBlock(r *Recorder, n uint64, hash byte) model.FeedBlock {
	return model.FeedBlock{Chain: r.anchor.Chain, Number: n, Hash: common.Hash{hash}}
}

func TestRawIntakeUnstartedIsNull(t *testing.T) {
	r, _ := intakeFixture(t)
	r.Received(intakeBlock(r, 1, 1), model.DatasetHeaders, time.Now())
	r.Appended(1, 10)
	require.Nil(t, Read(r.Descriptor(), r.anchor, r.plan).RawIntake)
}

func TestRawIntakeCursorAgeAndCapture(t *testing.T) {
	r, id := intakeFixture(t)
	r.StartIntake(intakeBlock(r, 10, 10))
	old := time.Now().Add(-time.Minute)
	b11, b12, b13 := intakeBlock(r, 11, 11), intakeBlock(r, 12, 12), intakeBlock(r, 13, 13)

	// Height 12 completes before 11: the cursor must wait for 11.
	r.Received(b12, model.DatasetHeaders, time.Now())
	r.Received(b12, model.DatasetBodies, time.Now())
	r.Received(b11, model.DatasetHeaders, old)
	r.Appended(3, 300)
	got := r.snapshot().RawIntake
	require.Equal(t, uint64(10), got.StartHeight)
	require.Equal(t, uint64(10), got.ReceivedThrough)
	require.Equal(t, 2, got.PendingBlocks)
	require.Equal(t, b11, got.OldestPending.Block)
	require.InDelta(t, float64(time.Minute/time.Millisecond), got.OldestPending.AgeMS, 5000)
	require.Equal(t, uint64(3), got.AppendedFrames)
	require.Equal(t, int64(300), got.AppendedBytes)

	// A re-delivered frame keeps the earliest receive time and adds no bytes.
	r.Received(b11, model.DatasetHeaders, time.Now())
	r.Received(b11, model.DatasetBodies, time.Now())
	r.Received(b13, model.DatasetHeaders, time.Now())
	got = r.snapshot().RawIntake
	require.Equal(t, uint64(12), got.ReceivedThrough)
	require.Equal(t, b11, got.OldestPending.Block)
	require.Equal(t, uint64(3), got.AppendedFrames)

	// A fork at a captured height is no longer pending.
	r.Received(intakeBlock(r, 11, 99), model.DatasetHeaders, old.Add(-time.Hour))
	id.Block = b11
	r.Complete(Captured, id)
	got = r.snapshot().RawIntake
	require.Equal(t, 2, got.PendingBlocks)
	require.Equal(t, b12, got.OldestPending.Block)

	id.Block = b12
	r.Complete(Captured, id)
	id.Block = b13
	r.Complete(Captured, id)
	got = r.snapshot().RawIntake
	require.Equal(t, uint64(13), got.ReceivedThrough)
	require.Zero(t, got.PendingBlocks)
	require.Nil(t, got.OldestPending)
}

func TestRawIntakeIgnoresOtherScope(t *testing.T) {
	r, _ := intakeFixture(t)
	r.StartIntake(intakeBlock(r, 0, 1))
	other := intakeBlock(r, 1, 1)
	other.Chain.ID = "2"
	r.Received(other, model.DatasetHeaders, time.Now())
	r.Received(intakeBlock(r, 1, 1), model.DatasetReceipts, time.Now())
	r.Received(intakeBlock(r, 0, 1), model.DatasetHeaders, time.Now())
	require.Zero(t, r.snapshot().RawIntake.PendingBlocks)
}

func TestRawIntakeBoundTruncatesNewest(t *testing.T) {
	r, _ := intakeFixture(t)
	r.StartIntake(intakeBlock(r, 0, 1))
	first := time.Now().Add(-time.Hour)
	for n := uint64(2); n < PendingBlocks+10; n++ {
		r.Received(intakeBlock(r, n, byte(n)), model.DatasetHeaders, first.Add(time.Duration(n)))
	}
	got := r.snapshot().RawIntake
	require.Equal(t, PendingBlocks, got.PendingBlocks)
	require.True(t, got.PendingTruncated)
	require.Equal(t, uint64(2), got.OldestPending.Block.Number)
	require.Zero(t, got.ReceivedThrough)
}

func TestRawIntakeRestartDiscardsSession(t *testing.T) {
	r, _ := intakeFixture(t)
	r.StartIntake(intakeBlock(r, 0, 1))
	r.Received(intakeBlock(r, 1, 1), model.DatasetHeaders, time.Now())
	r.Appended(1, 7)
	r.StartIntake(intakeBlock(r, 5, 5))
	got := r.snapshot().RawIntake
	require.Equal(t, uint64(5), got.StartHeight)
	require.Zero(t, got.PendingBlocks)
	require.Equal(t, int64(7), got.AppendedBytes)
}

func TestRawIntakeClockBehindHasNoAge(t *testing.T) {
	r, _ := intakeFixture(t)
	r.StartIntake(intakeBlock(r, 0, 1))
	r.Received(intakeBlock(r, 1, 1), model.DatasetHeaders, time.Now().Add(time.Hour))
	got := r.snapshot().RawIntake
	require.Equal(t, 1, got.PendingBlocks)
	require.Nil(t, got.OldestPending)
}
