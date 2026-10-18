package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/stretchr/testify/require"
)

func TestQueryObservations(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.SetBlock(ctx, &model.Block{Number: 1}))
	acquire, query := 0, 0
	observed := ObserveQueries(ctx, func(o QueryObservation) {
		require.GreaterOrEqual(t, o.Elapsed, time.Duration(0))
		switch o.Stage {
		case "acquire":
			acquire++
		case "query":
			query++
		default:
			t.Errorf("unknown stage: %s", o.Stage)
		}
	})
	_, err := s.GetBlock(observed, 1)
	require.NoError(t, err)
	require.Equal(t, 1, acquire)
	require.Equal(t, 1, query)
	acquire, query = 0, 0
	_, err = s.GetBlocks(observed, 0, 2)
	require.NoError(t, err)
	require.Equal(t, 1, acquire)
	require.Equal(t, 1, query)
	bound, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	acquire, query = 0, 0
	bound = ObserveQueries(bound, func(o QueryObservation) {
		if o.Stage == "query" {
			query++
		} else {
			acquire++
		}
	})
	_, err = s.GetBlock(bound, 1)
	require.NoError(t, err)
	require.Zero(t, acquire)
	require.Equal(t, 1, query, "bound reads must not acquire another connection")
}
