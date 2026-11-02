package feedtelemetry

import (
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"github.com/0xmhha/indexer-go/internal/nativeobserve/observetest"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestIntakeObservationPreservesOriginalUnverifiedReceive(t *testing.T) {
	r, _ := intakeFixture(t)
	i, tap := observetest.Bind(t, nativeobserve.CaptureReceivedHeight)
	r.AttachObservation(tap)
	r.StartIntake(intakeBlock(r, 0, 1))
	original := time.Now().Add(-time.Hour)
	r.Received(intakeBlock(r, 1, 2), model.DatasetHeaders, original)
	o, ok := i.Poll()
	require.True(t, ok)
	require.Equal(t, original, o.EventAt)
	require.Equal(t, nativeobserve.Unverified, o.Coverage)
	require.Zero(t, o.Position.Capture)
	r.Stop()
	r.Received(intakeBlock(r, 1, 2), model.DatasetBodies, original)
	_, ok = i.Poll()
	require.False(t, ok)
}
