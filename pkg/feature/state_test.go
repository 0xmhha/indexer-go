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

type independentFeature struct{ name string }

func (f independentFeature) Name() string                   { return f.name }
func (independentFeature) Requires() []string               { return nil }
func (independentFeature) Register(feature.Registrar) error { return nil }
func (independentFeature) OrderIndependent() bool           { return true }

func TestReconcileOnline(t *testing.T) {
	feature.Register(independentFeature{name: "rc.online"})
	require.True(t, feature.IsOrderIndependent("rc.online"))
	require.False(t, feature.IsOrderIndependent("rc.unknown"))
	gap := func(from, to uint64) storage.FeatureState {
		return storage.FeatureState{Active: true, Gap: &storage.BlockRange{From: from, To: to}}
	}

	t.Run("newly enabled runs online with a gap", func(t *testing.T) {
		w, jobs := feature.Reconcile([]string{"rc.online"}, map[string]storage.FeatureState{"x": {Active: true}}, 50, true)
		require.Equal(t, []feature.BackfillJob{{Feature: "rc.online", From: 0, To: 50, Online: true}}, jobs)
		require.Equal(t, gap(0, 50), w["rc.online"])
	})
	t.Run("re-enabled fills from its last complete block", func(t *testing.T) {
		w, jobs := feature.Reconcile([]string{"rc.online"}, map[string]storage.FeatureState{"rc.online": {Through: 19}}, 50, true)
		require.Equal(t, []feature.BackfillJob{{Feature: "rc.online", From: 20, To: 50, Online: true}}, jobs)
		require.Equal(t, gap(20, 50), w["rc.online"])
	})
	t.Run("interrupted gap resumes", func(t *testing.T) {
		w, jobs := feature.Reconcile([]string{"rc.online"}, map[string]storage.FeatureState{"rc.online": gap(31, 50)}, 70, true)
		require.Equal(t, []feature.BackfillJob{{Feature: "rc.online", From: 31, To: 50, Online: true}}, jobs)
		require.Empty(t, w)
	})
	t.Run("disabled with an open gap is complete below it", func(t *testing.T) {
		w, _ := feature.Reconcile(nil, map[string]storage.FeatureState{"rc.online": gap(31, 50)}, 70, true)
		require.Equal(t, storage.FeatureState{Through: 30}, w["rc.online"])
		w, _ = feature.Reconcile(nil, map[string]storage.FeatureState{"rc.online": gap(0, 50)}, 70, true)
		require.Equal(t, storage.FeatureState{Through: 0}, w["rc.online"])
	})
}
