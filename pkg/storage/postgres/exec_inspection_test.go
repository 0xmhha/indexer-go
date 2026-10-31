package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/stretchr/testify/require"
)

func TestExecutionIngressInspectionUsesReservedReaderAndReturnsItsLoan(t *testing.T) {
	s, policy := ingressOwnerFixture(t)
	require.NoError(t, s.ConfigureExecutionIngress(t.Context(), &policy, false))
	lease, err := s.ReserveQueryReader(t.Context())
	require.NoError(t, err)
	defer lease.Close()
	for range 3 {
		conn, err := s.pool.Acquire(t.Context())
		require.NoError(t, err)
		defer conn.Release()
	}
	ctx, cancel := context.WithTimeout(lease.Context(t.Context()), 5*time.Second)
	defer cancel()
	_, err = s.InspectExecutionIngress(ctx)
	require.NoError(t, err, "metadata export must borrow the existing reserved fourth slot")
	cancelled, stop := context.WithCancel(lease.Context(t.Context()))
	stop()
	_, err = s.InspectExecutionIngress(cancelled)
	require.ErrorIs(t, err, context.Canceled)
	_, err = s.InspectExecutionIngress(ctx)
	require.NoError(t, err, "the reserved connection loan must have been returned")
	require.EqualValues(t, 4, s.pool.Stat().AcquiredConns())
}

func TestExecutionIngressInspectionUsesExistingReadOnlySnapshot(t *testing.T) {
	s, policy := ingressOwnerFixture(t)
	require.NoError(t, s.ConfigureExecutionIngress(t.Context(), &policy, false))
	before, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	owner, err := s.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	for _, store := range []*Store{s, openTestStore(t, s.schema, true)} {
		view, err := store.InspectExecutionIngress(t.Context())
		require.NoError(t, err, "metadata inspection must work on the existing read-only Store")
		require.Equal(t, before, view.Control)
		require.Equal(t, owner, *view.Owner)
		require.Nil(t, view.Retention)
		require.Nil(t, view.Raw)
		require.NoError(t, view.Validate())
		data, err := json.Marshal(view)
		require.NoError(t, err)
		decoded, err := ingress.DecodeInspection(data)
		require.NoError(t, err)
		view.Control.IngressOwned = false // intentionally internal, json:"-"
		require.Equal(t, view, decoded)
		require.EqualValues(t, 4, store.pool.Config().MaxConns)
		require.Zero(t, store.pool.Stat().AcquiredConns())
	}
	after, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestExecutionIngressInspectionKeepsPendingRawEvidenceAndRejectsIncompleteArtifact(t *testing.T) {
	s, p, evidence := rawDeletionFixture(t)
	interrupted := errors.New("owned fixture interruption before raw deletion")
	require.ErrorIs(t, s.WithExecutionRawDeletion(t.Context(), p, evidence, func(context.Context) error { return interrupted }), interrupted)
	view, err := s.InspectExecutionIngress(t.Context())
	require.NoError(t, err)
	require.True(t, view.RawPending)
	require.NotNil(t, view.Raw)
	require.False(t, view.Raw.Completed)
	require.NoError(t, view.Validate())
	data, err := json.Marshal(view)
	require.NoError(t, err)
	require.Less(t, len(data), 64<<10, "the export must not include payload/reference arrays")
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))
	delete(fields, "RawPending")
	incomplete, err := json.Marshal(fields)
	require.NoError(t, err)
	_, err = ingress.DecodeInspection(incomplete)
	require.Error(t, err, "missing false/null fields are not negative observations")
	view.Raw.StoreID[0] ^= 1
	require.Error(t, view.Validate())
	ticket, err := s.ExecutionRawDeletion(t.Context(), execwire.Hash(evidence.Record.StoreID))
	require.NoError(t, err)
	require.False(t, ticket.Completed, "inspection must not finish the pending ticket")
}

func TestExecutionIngressInspectionObservesPendingAndCancellation(t *testing.T) {
	s, request := retentionFixture(t)
	permit, err := s.BeginExecutionRetention(t.Context(), request, func(context.Context) error { return nil })
	require.NoError(t, err)
	view, err := s.InspectExecutionIngress(t.Context())
	require.NoError(t, err, "pending maintenance is observable, not recovered")
	require.True(t, view.Retention.Pending)
	require.Equal(t, permit, view.Retention.Permit)
	require.Nil(t, view.Retention.Receipt)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.InspectExecutionIngress(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, s.pool.Stat().AcquiredConns())
	current, pending, _, err := s.ExecutionRetentionStatus(t.Context())
	require.NoError(t, err)
	require.True(t, pending)
	require.Equal(t, permit, current)
}
