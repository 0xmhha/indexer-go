package flags

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/config"
)

func configGroups() []*ConfigGroup {
	var out []*ConfigGroup
	for _, g := range append(Configuration(), Log()) {
		if cg, ok := g.(*ConfigGroup); ok {
			out = append(out, cg)
		}
	}
	return out
}

// testValue returns a value for the field unlike its default.
func testValue(t *testing.T, p any) string {
	switch v := p.(type) {
	case *string:
		return "value-" + *v
	case *int:
		return fmt.Sprint(*v + 7)
	case *uint64:
		return fmt.Sprint(*v + 9)
	case *bool:
		return fmt.Sprint(!*v)
	}
	t.Fatalf("unsupported field type %T", p)
	return ""
}

// TestFlagsMatchEnvironment: each flag sets the same field as the
// environment variable its help names, with the same value.
func TestFlagsMatchEnvironment(t *testing.T) {
	for _, g := range configGroups() {
		for _, f := range g.Flags() {
			t.Run(f.Name, func(t *testing.T) {
				value := testValue(t, f.Field(config.NewConfig()))

				t.Setenv(f.Env, value)
				fromEnv := config.NewConfig()
				require.NoError(t, fromEnv.LoadFromEnv())

				fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
				group := NewConfigGroup(g.Name(), f)
				group.AddFlags(fs)
				require.NoError(t, fs.Parse([]string{"--" + f.Name + "=" + value}))
				fromFlag := config.NewConfig()
				group.ApplyTo(fromFlag, func(name string) bool { return fs.Changed(name) })

				want := reflect.ValueOf(f.Field(fromEnv)).Elem().Interface()
				require.NotEqual(t, reflect.ValueOf(f.Field(config.NewConfig())).Elem().Interface(), want, "%s did not change the field", f.Env)
				require.Equal(t, want, reflect.ValueOf(f.Field(fromFlag)).Elem().Interface())
			})
		}
	}
}

// TestFlagNamesAreUnique: a set silently drops a flag whose name an
// earlier group took.
func TestFlagNamesAreUnique(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	groups := append(append(Configuration(), Log()), &File{}, &Start{})
	NewSet(groups...).Add(fs)
	n := 0
	for _, g := range groups {
		gs := pflag.NewFlagSet(g.Name(), pflag.ContinueOnError)
		g.AddFlags(gs)
		n += countFlags(gs)
	}
	require.Equal(t, n, countFlags(fs))
}

func countFlags(fs *pflag.FlagSet) int {
	n := 0
	fs.VisitAll(func(*pflag.Flag) { n++ })
	return n
}

// TestUnchangedFlagsKeepConfiguration: flags not given leave every field
// as it was, including false booleans and zero numbers.
func TestUnchangedFlagsKeepConfiguration(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	set := NewSet(Configuration()...)
	set.Add(fs)
	require.NoError(t, fs.Parse(nil))
	cfg := config.NewConfig()
	cfg.API.Enabled = true
	cfg.Indexer.Workers = 3
	set.ApplyTo(cfg, fs.Changed)
	require.True(t, cfg.API.Enabled)
	require.Equal(t, 3, cfg.Indexer.Workers)
}

func TestStartRejectsClearDataWithReindex(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	var s Start
	s.AddFlags(fs)
	require.NoError(t, fs.Parse([]string{"--clear-data", "--reindex"}))
	_, err := s.Options()
	require.ErrorIs(t, err, ErrClearAndReindex)
}
