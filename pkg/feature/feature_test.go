package feature_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/feature"
)

type testFeature struct {
	name     string
	requires []string
	calls    *[]string
	fail     error
}

func (f testFeature) Name() string       { return f.name }
func (f testFeature) Requires() []string { return f.requires }
func (f testFeature) Register(r feature.Registrar) error {
	r.OnBlock(feature.BlockHandlerFunc(func(context.Context, *feature.Block) error {
		*f.calls = append(*f.calls, f.name)
		return f.fail
	}))
	return nil
}

func names(fs []feature.Feature) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Name()
	}
	return out
}

func TestResolveAndRun(t *testing.T) {
	var calls []string
	errBoom := errors.New("boom")
	for _, f := range []testFeature{
		{name: "rr.c", requires: []string{"rr.b"}},
		{name: "rr.b", requires: []string{"rr.z"}},
		{name: "rr.z"},
		{name: "rr.a"},
		{name: "rr.cycle1", requires: []string{"rr.cycle2"}},
		{name: "rr.cycle2", requires: []string{"rr.cycle1"}},
		{name: "rr.fail", fail: errBoom},
	} {
		f.calls = &calls
		feature.Register(f)
	}

	t.Run("dependencies first, then by name", func(t *testing.T) {
		fs, err := feature.Resolve([]string{"rr.c", "rr.a", "rr.b", "rr.z"})
		require.NoError(t, err)
		require.Equal(t, []string{"rr.a", "rr.z", "rr.b", "rr.c"}, names(fs))
		again, err := feature.Resolve([]string{"rr.z", "rr.b", "rr.a", "rr.c"})
		require.NoError(t, err)
		require.Equal(t, names(fs), names(again), "order does not depend on input order")
	})
	t.Run("disabled requirement", func(t *testing.T) {
		_, err := feature.Resolve([]string{"rr.c", "rr.b"})
		require.ErrorContains(t, err, `"rr.b" requires "rr.z"`)
	})
	t.Run("unknown feature", func(t *testing.T) {
		_, err := feature.Resolve([]string{"rr.typo"})
		require.ErrorContains(t, err, "not registered")
	})
	t.Run("cycle", func(t *testing.T) {
		_, err := feature.Resolve([]string{"rr.cycle1", "rr.cycle2"})
		require.ErrorContains(t, err, "cycle")
	})
	t.Run("pipeline runs handlers in order and stops on error", func(t *testing.T) {
		p, err := feature.Build([]string{"rr.c", "rr.b", "rr.z", "rr.fail"}, feature.Deps{})
		require.NoError(t, err)
		require.Equal(t, []string{"rr.fail", "rr.z", "rr.b", "rr.c"}, p.Features())
		calls = nil
		err = p.HandleBlock(context.Background(), &feature.Block{})
		require.ErrorIs(t, err, errBoom)
		require.Equal(t, []string{"rr.fail"}, calls)

		p, err = feature.Build([]string{"rr.c", "rr.b", "rr.z"}, feature.Deps{})
		require.NoError(t, err)
		calls = nil
		require.NoError(t, p.HandleBlock(context.Background(), &feature.Block{}))
		require.Equal(t, []string{"rr.z", "rr.b", "rr.c"}, calls)
	})
	t.Run("duplicate registration", func(t *testing.T) {
		require.Panics(t, func() { feature.Register(testFeature{name: "rr.a", calls: &calls}) })
	})
}

func TestEnabled(t *testing.T) {
	var calls []string
	feature.Register(testFeature{name: "en.default", calls: &calls})
	feature.Register(testFeature{name: "en.optional", calls: &calls})

	got, err := feature.Enabled([]string{"en.default", "en.not_migrated"}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"en.default"}, got, "unregistered defaults are skipped")

	got, err = feature.Enabled([]string{"en.default"}, map[string]bool{"en.default": false, "en.optional": true})
	require.NoError(t, err)
	require.Equal(t, []string{"en.optional"}, got)

	_, err = feature.Enabled(nil, map[string]bool{"en.typo": true})
	require.ErrorContains(t, err, "not registered")
}
