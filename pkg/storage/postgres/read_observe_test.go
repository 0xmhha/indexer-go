package postgres

import (
	"context"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
)

// These failure paths need no live DB and must not create a connection or
// change cancellation/close behavior merely because diagnostics are enabled.
func TestReadDiagnosticsAdmissionFailures(t *testing.T) {
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		var rows []ReadObservation
		ctx = ObserveReads(ctx, func(o ReadObservation) { rows = append(rows, o) })
		r := &reservedQueryReader{}
		_, _, err := r.acquire(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Zero(t, r.active)
		require.Len(t, rows, 1)
		require.Equal(t, ReadObservation{Stage: "admission", Elapsed: rows[0].Elapsed, Path: "unavailable", Failed: true}, rows[0])
	})
	t.Run("closed", func(t *testing.T) {
		stopped := make(chan struct{})
		close(stopped)
		r := &reservedQueryReader{stopped: stopped, gate: make(chan struct{}, 1)}
		var rows []ReadObservation
		ctx := ObserveReads(t.Context(), func(o ReadObservation) { rows = append(rows, o) })
		_, _, err := r.acquire(ctx)
		require.ErrorIs(t, err, port.ErrClosed)
		require.Zero(t, r.active)
		require.Len(t, rows, 1)
		require.True(t, rows[0].Failed)
		require.Zero(t, rows[0].BorrowProbes)
	})
}
