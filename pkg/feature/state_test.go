package feature_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

func TestReconcile(t *testing.T) {
	active := port.FeatureState{Active: true}
	through := func(h uint64) port.FeatureState { return port.FeatureState{Through: h} }

	t.Run("empty database", func(t *testing.T) {
		w, jobs := feature.Reconcile([]string{"a", "b"}, nil, 0, false)
		require.Equal(t, map[string]port.FeatureState{"a": active, "b": active}, w)
		require.Empty(t, jobs)
	})
	t.Run("database from before feature states", func(t *testing.T) {
		w, jobs := feature.Reconcile([]string{"a"}, map[string]port.FeatureState{}, 50, true)
		require.Equal(t, map[string]port.FeatureState{"a": active}, w)
		require.Empty(t, jobs)
	})
	t.Run("newly enabled, re-enabled, disabled, finished", func(t *testing.T) {
		w, jobs := feature.Reconcile(
			[]string{"up", "new", "back", "done"},
			map[string]port.FeatureState{"up": active, "back": through(19), "done": through(50), "off": active},
			50, true)
		require.Equal(t, []feature.BackfillJob{{Feature: "new", From: 0, To: 50}, {Feature: "back", From: 20, To: 50}}, jobs)
		require.Equal(t, map[string]port.FeatureState{"done": active, "off": through(50)}, w)
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
	gap := func(from, to uint64) port.FeatureState {
		return port.FeatureState{Active: true, Gap: &port.BlockRange{From: from, To: to}}
	}

	t.Run("newly enabled runs online with a gap", func(t *testing.T) {
		w, jobs := feature.Reconcile([]string{"rc.online"}, map[string]port.FeatureState{"x": {Active: true}}, 50, true)
		require.Equal(t, []feature.BackfillJob{{Feature: "rc.online", From: 0, To: 50, Online: true}}, jobs)
		require.Equal(t, gap(0, 50), w["rc.online"])
	})
	t.Run("re-enabled fills from its last complete block", func(t *testing.T) {
		w, jobs := feature.Reconcile([]string{"rc.online"}, map[string]port.FeatureState{"rc.online": {Through: 19}}, 50, true)
		require.Equal(t, []feature.BackfillJob{{Feature: "rc.online", From: 20, To: 50, Online: true}}, jobs)
		require.Equal(t, gap(20, 50), w["rc.online"])
	})
	t.Run("interrupted gap resumes", func(t *testing.T) {
		w, jobs := feature.Reconcile([]string{"rc.online"}, map[string]port.FeatureState{"rc.online": gap(31, 50)}, 70, true)
		require.Equal(t, []feature.BackfillJob{{Feature: "rc.online", From: 31, To: 50, Online: true}}, jobs)
		require.Empty(t, w)
	})
	t.Run("disabled with an open gap is complete below it", func(t *testing.T) {
		w, _ := feature.Reconcile(nil, map[string]port.FeatureState{"rc.online": gap(31, 50)}, 70, true)
		require.Equal(t, port.FeatureState{Through: 30}, w["rc.online"])
		w, _ = feature.Reconcile(nil, map[string]port.FeatureState{"rc.online": gap(0, 50)}, 70, true)
		require.Equal(t, port.FeatureState{Through: 0}, w["rc.online"])
	})
}

func TestReconcileParts(t *testing.T) {
	feature.Register(independentFeature{name: "rc.parts"})
	def := func(d string) port.FeatureState { return port.FeatureState{Active: true, Definition: d} }
	units := func(defs ...string) []feature.Unit {
		out := make([]feature.Unit, len(defs))
		for i, d := range defs {
			out[i] = feature.Unit{Name: "rc.parts/" + string(rune('a'+i)), Definition: d}
		}
		return out
	}

	t.Run("empty database records the definitions", func(t *testing.T) {
		w, jobs, err := feature.ReconcileUnits(units("1", "2"), nil, 0, false)
		require.NoError(t, err)
		require.Empty(t, jobs)
		require.Equal(t, map[string]port.FeatureState{"rc.parts/a": def("1"), "rc.parts/b": def("2")}, w)
	})
	t.Run("an added part is backfilled alone", func(t *testing.T) {
		w, jobs, err := feature.ReconcileUnits(units("1", "2"), map[string]port.FeatureState{"rc.parts/a": def("1")}, 50, true)
		require.NoError(t, err)
		require.Equal(t, []feature.BackfillJob{{Feature: "rc.parts/b", From: 0, To: 50, Online: true, Definition: "2"}}, jobs)
		require.Equal(t, map[string]port.FeatureState{
			"rc.parts/b": {Active: true, Gap: &port.BlockRange{From: 0, To: 50}, Definition: "2"},
		}, w)
	})
	t.Run("a changed definition is refused", func(t *testing.T) {
		_, _, err := feature.ReconcileUnits(units("1", "3"), map[string]port.FeatureState{"rc.parts/a": def("1"), "rc.parts/b": def("2")}, 50, true)
		require.ErrorContains(t, err, "rc.parts/b changed since it was indexed")
	})
	t.Run("a removed part stops, keeping its definition", func(t *testing.T) {
		w, jobs, err := feature.ReconcileUnits(units("1"), map[string]port.FeatureState{"rc.parts/a": def("1"), "rc.parts/b": def("2")}, 50, true)
		require.NoError(t, err)
		require.Empty(t, jobs)
		require.Equal(t, map[string]port.FeatureState{"rc.parts/b": {Through: 50, Definition: "2"}}, w)

		w, jobs, err = feature.ReconcileUnits(units("1", "2"), map[string]port.FeatureState{"rc.parts/a": def("1"), "rc.parts/b": w["rc.parts/b"]}, 70, true)
		require.NoError(t, err)
		require.Equal(t, []feature.BackfillJob{{Feature: "rc.parts/b", From: 51, To: 70, Online: true, Definition: "2"}}, jobs,
			"added back with the same definition, it continues where it stopped")
		require.Len(t, w, 1)
	})
	t.Run("parts take over a feature recorded before it had parts", func(t *testing.T) {
		w, jobs, err := feature.ReconcileUnits(units("1", "2"), map[string]port.FeatureState{"rc.parts": {Active: true}}, 50, true)
		require.NoError(t, err)
		require.Empty(t, jobs)
		require.Equal(t, map[string]port.FeatureState{"rc.parts/a": def("1"), "rc.parts/b": def("2")}, w,
			"the feature's own state is not recorded as stopped")
	})
}

func TestPipelineParts(t *testing.T) {
	feature.Register(partsFeature{})
	p, err := feature.Build([]string{"pp.parts"}, feature.Deps{})
	require.NoError(t, err)
	require.Equal(t, []feature.Unit{{Name: "pp.parts/x", Definition: "dx"}, {Name: "pp.parts/y", Definition: "dy"}}, p.Units())
	require.Equal(t, []string{"pp.parts"}, p.Features())

	var ran []string
	b := &feature.Block{Model: &model.Block{}}
	require.NoError(t, p.Only("pp.parts/y").HandleBlock(context.WithValue(context.Background(), ranKey{}, &ran), b))
	require.Equal(t, []string{"y"}, ran, "one part")
	ran = nil
	require.NoError(t, p.Only("pp.parts").HandleBlock(context.WithValue(context.Background(), ranKey{}, &ran), b))
	require.Equal(t, []string{"x", "y"}, ran, "the whole feature")
}

type ranKey struct{}

type partsFeature struct{}

func (partsFeature) Name() string       { return "pp.parts" }
func (partsFeature) Requires() []string { return nil }
func (partsFeature) Register(r feature.Registrar) error {
	for _, part := range []string{"x", "y"} {
		r.(feature.PartRegistrar).OnPart(part, "d"+part, feature.BlockHandlerFunc(func(ctx context.Context, _ *feature.Block) error {
			ran := ctx.Value(ranKey{}).(*[]string)
			*ran = append(*ran, part)
			return nil
		}))
	}
	return nil
}
