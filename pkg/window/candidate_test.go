package window

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestCandidateRequiresOneCompleteHashAcrossSegments(t *testing.T) {
	opts, _ := fixture(t)
	opts.MaxSegmentRecords = 1
	s := created(t, opts)
	ctx := context.Background()
	kinds := []model.DatasetKind{model.DatasetReceipts, model.DatasetBodies}
	_, err := s.Candidate(ctx, 1, kinds)
	require.ErrorIs(t, err, ErrNotFound)
	e := envelope(opts, 1)
	body, err := s.Append(ctx, e)
	require.NoError(t, err)
	_, err = s.Candidate(ctx, 1, kinds)
	require.ErrorIs(t, err, ErrNotFound)
	e.Kind = model.DatasetReceipts
	receipts, err := s.Append(ctx, e)
	require.NoError(t, err)
	got, err := s.Candidate(ctx, 1, kinds)
	require.NoError(t, err)
	require.Equal(t, []Reference{receipts, body}, got, "required order, across sealed/active indexes")
	_, err = s.Append(ctx, e)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	reopened, err := Open(opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	got, err = reopened.Candidate(ctx, 1, kinds)
	require.NoError(t, err)
	require.Equal(t, []Reference{receipts, body}, got)
	e.Block.Hash = common.Hash{99}
	e.Kind = model.DatasetHeaders // even an incomplete fork is ambiguous
	_, err = reopened.Append(ctx, e)
	require.NoError(t, err)
	_, err = reopened.Candidate(ctx, 1, kinds)
	require.ErrorIs(t, err, ErrAmbiguous)
	_, err = reopened.Candidate(ctx, 2, kinds)
	require.ErrorIs(t, err, ErrNotFound)
	for _, invalid := range [][]model.DatasetKind{nil, {model.DatasetBodies, model.DatasetBodies}, {"unknown"}} {
		_, err = reopened.Candidate(ctx, 1, invalid)
		require.ErrorIs(t, err, ErrConflict)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = reopened.Candidate(cancelled, 1, kinds)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, reopened.Close())
	_, err = reopened.Candidate(ctx, 1, kinds)
	require.ErrorIs(t, err, ErrClosed)
}

func TestCandidateNeverHidesCorruptIndex(t *testing.T) {
	opts, _ := fixture(t)
	s := created(t, opts)
	_, err := s.Append(context.Background(), envelope(opts, 1))
	require.NoError(t, err)
	require.NoError(t, s.Seal(context.Background()))
	index := filepath.Join(opts.Path, segmentName(s.segments[0].ID, "idx"))
	require.NoError(t, os.WriteFile(index, []byte("corrupt"), 0600))
	_, err = s.Candidate(context.Background(), 1, []model.DatasetKind{model.DatasetBodies})
	require.ErrorIs(t, err, ErrUnavailable, "a corrupt index poisons the store; it is never an empty candidate")
}
