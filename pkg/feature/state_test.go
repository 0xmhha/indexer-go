package feature_test

import (
	"context"
	"strings"
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

// evolvingFeature compares definitions as "base" and "base+more": the
// longer one extends the shorter, "same:x" equals "x".
type evolvingFeature struct{ independentFeature }

func (evolvingFeature) EvolvePart(_, stored, current string) feature.PartChange {
	switch {
	case stored == "same:"+current:
		return feature.PartUnchanged
	case strings.HasPrefix(current, stored+"+"):
		return feature.PartExtended
	}
	return feature.PartIncompatible
}

func TestReconcileEvolvedParts(t *testing.T) {
	feature.Register(evolvingFeature{independentFeature{name: "rc.evolve"}})
	unit := func(def string) []feature.Unit { return []feature.Unit{{Name: "rc.evolve/t", Definition: def}} }
	stored := map[string]port.FeatureState{"rc.evolve/t": {Active: true, Definition: "base"}}

	t.Run("extended: backfilled again from the start", func(t *testing.T) {
		w, jobs, err := feature.ReconcileUnits(unit("base+more"), stored, 50, true)
		require.NoError(t, err)
		require.Equal(t, []feature.BackfillJob{{Feature: "rc.evolve/t", From: 0, To: 50, Online: true, Definition: "base+more"}}, jobs)
		require.Equal(t, port.FeatureState{Active: true, Gap: &port.BlockRange{From: 0, To: 50}, Definition: "base+more"}, w["rc.evolve/t"])
	})
	t.Run("unchanged in another form: the new form is recorded", func(t *testing.T) {
		w, jobs, err := feature.ReconcileUnits(unit("x"), map[string]port.FeatureState{"rc.evolve/t": {Active: true, Definition: "same:x"}}, 50, true)
		require.NoError(t, err)
		require.Empty(t, jobs)
		require.Equal(t, port.FeatureState{Active: true, Definition: "x"}, w["rc.evolve/t"])
	})
	t.Run("incompatible: refused", func(t *testing.T) {
		_, _, err := feature.ReconcileUnits(unit("other"), stored, 50, true)
		require.ErrorContains(t, err, "changed since it was indexed")
	})
	t.Run("incompatible with rebuild allowed: reset and backfilled from the start", func(t *testing.T) {
		u := []feature.Unit{{Name: "rc.evolve/t", Definition: "other", Rebuild: true}}
		w, jobs, err := feature.ReconcileUnits(u, stored, 50, true)
		require.NoError(t, err)
		require.Equal(t, []feature.BackfillJob{{Feature: "rc.evolve/t", From: 0, To: 50, Online: true, Definition: "other", Reset: true}}, jobs)
		require.Equal(t, port.FeatureState{Active: true, Gap: &port.BlockRange{From: 0, To: 50}, Definition: "other"}, w["rc.evolve/t"])
	})
	t.Run("rebuild allowed but nothing changed: no reset", func(t *testing.T) {
		u := []feature.Unit{{Name: "rc.evolve/t", Definition: "base", Rebuild: true}}
		w, jobs, err := feature.ReconcileUnits(u, stored, 50, true)
		require.NoError(t, err)
		require.Empty(t, jobs)
		require.Empty(t, w)
	})
}

// rebuildingHandler is a part handler that may be rebuilt.
type rebuildingHandler struct {
	feature.BlockHandlerFunc
	allowed bool
	resets  *int
}

func (h rebuildingHandler) RebuildAllowed() bool { return h.allowed }
func (h rebuildingHandler) ResetPart(context.Context) error {
	*h.resets++
	return nil
}

type rebuildFeature struct{ resets *int }

func (rebuildFeature) Name() string       { return "pp.rebuild" }
func (rebuildFeature) Requires() []string { return nil }
func (f rebuildFeature) Register(r feature.Registrar) error {
	noop := feature.BlockHandlerFunc(func(context.Context, *feature.Block) error { return nil })
	r.(feature.PartRegistrar).OnPart("on", "d", rebuildingHandler{noop, true, f.resets})
	r.(feature.PartRegistrar).OnPart("off", "d", rebuildingHandler{noop, false, f.resets})
	r.(feature.PartRegistrar).OnPart("plain", "d", noop)
	return nil
}

func TestPipelinePartRebuild(t *testing.T) {
	var resets int
	feature.Register(rebuildFeature{&resets})
	p, err := feature.Build([]string{"pp.rebuild"}, feature.Deps{})
	require.NoError(t, err)
	require.Equal(t, []feature.Unit{
		{Name: "pp.rebuild/on", Definition: "d", Rebuild: true},
		{Name: "pp.rebuild/off", Definition: "d"},
		{Name: "pp.rebuild/plain", Definition: "d"},
	}, p.Units(), "only a rebuilder that allows it")

	require.NoError(t, p.ResetPart(context.Background(), "pp.rebuild/on"))
	require.Equal(t, 1, resets)
	require.ErrorContains(t, p.ResetPart(context.Background(), "pp.rebuild/plain"), "cannot be rebuilt")
	require.ErrorContains(t, p.ResetPart(context.Background(), "pp.rebuild/none"), "has no handler")
}
