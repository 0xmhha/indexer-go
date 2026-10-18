package window

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRetainedSegmentDoesNotHideOtherEligibleSegments(t *testing.T) {
	opts, clock := fixture(t)
	raw, err := Create(opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, raw.Close()) }()
	first, err := raw.Append(context.Background(), envelope(opts, 1))
	require.NoError(t, err)
	require.NoError(t, raw.Seal(context.Background()))
	second, err := raw.Append(context.Background(), envelope(opts, 2))
	require.NoError(t, err)
	require.NoError(t, raw.SealExpired(context.Background()))
	require.Equal(t, 1, raw.Stats().ActiveIndexRecords, "young active data stays active")
	clock.advance(25 * time.Hour)
	require.NoError(t, raw.SealExpired(context.Background()))
	require.Zero(t, raw.Stats().ActiveIndexRecords)
	n, err := raw.Collect(context.Background(), guardFunc(func(_ context.Context, refs []Reference, remove func() error) error {
		if refs[0].Segment == first.Segment {
			return ErrRetain
		}
		return remove()
	}))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, err = raw.Lookup(context.Background(), first.Block, first.Kind)
	require.NoError(t, err)
	_, err = raw.Lookup(context.Background(), second.Block, second.Kind)
	require.ErrorIs(t, err, ErrNotFound)
	want := errors.New("storage authority failed")
	_, err = raw.Collect(context.Background(), guardFunc(func(context.Context, []Reference, func() error) error { return errors.Join(ErrRetain, want) }))
	require.ErrorIs(t, err, want, "a joined independent error must not be treated as normal retention")
}
