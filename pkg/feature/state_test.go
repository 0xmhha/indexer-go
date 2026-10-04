package feature_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

func TestReconcile(t *testing.T) {
	active := storage.FeatureState{Active: true}
	through := func(h uint64) storage.FeatureState { return storage.FeatureState{Through: h} }

	t.Run("empty database", func(t *testing.T) {
		w, jobs := feature.Reconcile([]string{"a", "b"}, nil, 0, false)
		require.Equal(t, map[string]storage.FeatureState{"a": active, "b": active}, w)
		require.Empty(t, jobs)
	})
	t.Run("database from before feature states", func(t *testing.T) {
		w, jobs := feature.Reconcile([]string{"a"}, map[string]storage.FeatureState{}, 50, true)
		require.Equal(t, map[string]storage.FeatureState{"a": active}, w)
		require.Empty(t, jobs)
	})
	t.Run("newly enabled, re-enabled, disabled, finished", func(t *testing.T) {
		w, jobs := feature.Reconcile(
			[]string{"up", "new", "back", "done"},
			map[string]storage.FeatureState{"up": active, "back": through(19), "done": through(50), "off": active},
			50, true)
		require.Equal(t, []feature.BackfillJob{{Feature: "new", From: 0, To: 50}, {Feature: "back", From: 20, To: 50}}, jobs)
		require.Equal(t, map[string]storage.FeatureState{"done": active, "off": through(50)}, w)
	})
}
