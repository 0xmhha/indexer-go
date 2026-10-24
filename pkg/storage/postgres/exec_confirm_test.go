package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
)

func confirmationFixture(t *testing.T) (*Store, execwire.Result, []byte) {
	t.Helper()
	s, _, p := publicationFixture(t, 7)
	r := execwire.Result{Ref: p.Ref, Parent: p.Parent, StateRoot: p.Root, ReceiptsRoot: execwire.Hash{7}, Time: 1, BlockRLP: []byte{0xc0}, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Cause: execwire.Finalize, TxIndex: -1}}}
	payload, err := execwire.MarshalResult(r)
	require.NoError(t, err)
	p.Digest = execwire.Hash(sha256.Sum256(payload))
	_, err = s.pool.Exec(t.Context(), "UPDATE exec_parts SET digest=$1", p.Digest[:])
	require.NoError(t, err)
	_, err = s.CommitExecution(t.Context(), p, func(ctx context.Context) error { return s.Put(ctx, []byte("confirmed-feature"), []byte("once")) }, func(context.Context) error { return nil })
	require.NoError(t, err)
	return s, r, payload
}

func TestExecutionConfirmationLostACKAndCanonicalOlderGeneration(t *testing.T) {
	s, r, payload := confirmationFixture(t)
	fresh := 0
	check := func(_ context.Context, ref execwire.Ref) error { fresh++; require.Equal(t, r.Ref, ref); return nil }
	ok, err := s.ConfirmExecution(t.Context(), payload, check)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = s.BeginExecutionRewind(t.Context(), r.Ref, r.StateRoot)
	require.NoError(t, err)
	_, err = s.ResumeExecutionRewind(t.Context(), nil, func(context.Context, execwire.Ref) error { return nil })
	require.NoError(t, err)
	r.Ref.Generation++
	payload, err = execwire.MarshalResult(r)
	require.NoError(t, err)
	ok, err = s.ConfirmExecution(t.Context(), payload, check)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 2, fresh)
	value, err := s.Get(t.Context(), []byte("confirmed-feature"))
	require.NoError(t, err)
	require.Equal(t, []byte("once"), value)
	var count int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM exec_publications").Scan(&count))
	require.Equal(t, 1, count)
	altered := r
	altered.Time++
	bad, err := execwire.MarshalResult(altered)
	require.NoError(t, err)
	_, err = s.ConfirmExecution(t.Context(), bad, check)
	require.ErrorIs(t, err, port.ErrExecutionConflict)
	require.Equal(t, 2, fresh)
}

func TestExecutionConfirmationFreshFailureFenceAndUnpublished(t *testing.T) {
	s, r, payload := confirmationFixture(t)
	failure := errors.New("canonical unavailable")
	ok, err := s.ConfirmExecution(t.Context(), payload, func(context.Context, execwire.Ref) error { return failure })
	require.ErrorIs(t, err, failure)
	require.False(t, ok)
	ok, err = s.ConfirmExecution(t.Context(), payload, func(ctx context.Context, _ execwire.Ref) error {
		_, e := s.q(ctx).Exec(ctx, "UPDATE exec_control SET generation=generation+1 WHERE id")
		return e
	})
	require.ErrorIs(t, err, port.ErrExecutionStale)
	require.False(t, ok)
	c, err := s.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, uint64(1), c.Generation)
	r.Ref.Height++
	r.Parent = r.Ref.Hash
	r.Ref.Hash[0]++
	payload, err = execwire.MarshalResult(r)
	require.NoError(t, err)
	ok, err = s.ConfirmExecution(t.Context(), payload, func(context.Context, execwire.Ref) error {
		t.Fatal("unpublished payload invoked fresh/ACK")
		return nil
	})
	require.NoError(t, err)
	require.False(t, ok)
	_, err = s.BeginExecutionRewind(t.Context(), c.Visible, c.Root)
	require.NoError(t, err)
	_, err = s.ConfirmExecution(t.Context(), payload, func(context.Context, execwire.Ref) error { return nil })
	require.ErrorIs(t, err, port.ErrExecutionRewinding)
}
