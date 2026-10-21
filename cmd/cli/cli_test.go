package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/app"
)

var update = flag.Bool("update", false, "rewrite testdata/help.txt")

const baseConfig = `rpc:
  endpoint: "http://127.0.0.1:8545"
database:
  path: "/tmp/indexer-flags-test"
indexer:
  workers: 7
  chunk_size: 5
api:
  enabled: true
`

func configFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

// result is what one command line ran.
type result struct {
	code    int
	stdout  string
	stderr  string
	started bool
	resumed bool
	cfg     *config.Config
	opts    app.StartOptions
}

// run executes args with the engine replaced by recorders; startErr is
// what the indexer returns.
func run(t *testing.T, startErr error, args ...string) result {
	t.Helper()
	var r result
	c := newCLI()
	c.start = func(_ context.Context, cfg *config.Config, _ *zap.Logger, opts app.StartOptions) error {
		r.started, r.cfg, r.opts = true, cfg, opts
		return startErr
	}
	c.resume = func(_ context.Context, cfg *config.Config, _ *zap.Logger) error {
		r.resumed, r.cfg = true, cfg
		return nil
	}
	root := c.command()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	r.code = execute(context.Background(), root, args, &errOut)
	r.stdout, r.stderr = out.String(), errOut.String()
	return r
}

func TestFlagsOnlyOverrideWhenGiven(t *testing.T) {
	path := configFile(t, baseConfig)

	r := run(t, nil, "--config", path, "--log-level", "error")
	require.Zero(t, r.code, r.stderr)
	require.Equal(t, 7, r.cfg.Indexer.Workers, "the --workers default must not replace the config value")
	require.Equal(t, 5, r.cfg.Indexer.ChunkSize)
	require.True(t, r.cfg.API.Enabled)

	r = run(t, nil, "--config", path, "--log-level", "error", "--workers", "3", "--chunk-size", "9", "--api=false")
	require.Zero(t, r.code, r.stderr)
	require.Equal(t, 3, r.cfg.Indexer.Workers)
	require.Equal(t, 9, r.cfg.Indexer.ChunkSize)
	require.False(t, r.cfg.API.Enabled, "boolean flags must be able to switch features off")
}

func TestFlagsCanSupplyRequiredValues(t *testing.T) {
	// The file lacks the RPC endpoint; --rpc provides it. Validation must
	// run after flags are applied.
	path := configFile(t, "database:\n  path: \"/tmp/indexer-flags-test\"\n")
	r := run(t, nil, "--config", path, "--log-level", "error", "--rpc", "http://127.0.0.1:8545")
	require.Zero(t, r.code, r.stderr)
	require.Equal(t, "http://127.0.0.1:8545", r.cfg.RPC.Endpoint)
}

func TestDefaultConfigFileIsOptional(t *testing.T) {
	// No --config and no ./config.yaml (tests run in cmd/cli).
	r := run(t, nil, "--log-level", "error", "--rpc", "http://127.0.0.1:8545", "--db", t.TempDir())
	require.Zero(t, r.code, r.stderr)

	// An explicitly given config file must exist.
	r = run(t, nil, "--config", filepath.Join(t.TempDir(), "missing.yaml"))
	require.Equal(t, exitFailure, r.code)
	require.False(t, r.started)
	require.Contains(t, r.stderr, "no such file")
}

func TestStartOptions(t *testing.T) {
	path := configFile(t, baseConfig)
	r := run(t, nil, "--config", path, "--log-level", "error", "--gap-recovery", "--reindex", "--adapter", "evm")
	require.Zero(t, r.code, r.stderr)
	require.Equal(t, app.StartOptions{GapRecovery: true, Reindex: true, Adapter: "evm",
		Build: app.BuildInfo{Name: defaultName, Version: version, Commit: commit, Time: buildTime}}, r.opts)
}

// TestUsageErrors: a wrong command line starts nothing, exits 2 and says
// what to do instead.
func TestUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"positional before flags", []string{"start", "--rpc", "http://127.0.0.1:8545"}, []string{`unknown command "start" for "indexer"`, "takes flags, not arguments"}},
		{"misspelled command", []string{"resum-check"}, []string{"Did you mean 'indexer resume-check'?"}},
		{"misspelled flag", []string{"--rpcc", "http://127.0.0.1:8545"}, []string{"unknown flag: --rpcc", "Did you mean --rpc?"}},
		{"single dash", []string{"-rpc", "http://127.0.0.1:8545"}, []string{"Long flags take two dashes: --rpc."}},
		{"clear and reindex", []string{"--clear-data", "--reindex"}, []string{"give only one of them"}},
		{"indexing flag on resume-check", []string{"resume-check", "--rpc", "x"}, []string{"unknown flag: --rpc", "Run 'indexer resume-check --help'"}},
		{"bad value", []string{"--workers", "many"}, []string{`invalid argument "many" for "--workers"`}},
		{"unknown adapter", []string{"--adapter", "stablenett"}, []string{`--adapter "stablenett" is not a chain profile of this binary`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := run(t, nil, tc.args...)
			require.Equal(t, exitUsage, r.code)
			require.False(t, r.started)
			require.False(t, r.resumed)
			for _, w := range tc.want {
				require.Contains(t, r.stderr, w)
			}
			require.Contains(t, r.stderr, "--help' for usage.")
		})
	}
}

func TestInvalidConfigurationSaysWhereSettingsComeFrom(t *testing.T) {
	r := run(t, nil, "--rpc", "http://127.0.0.1:8545", "--db", t.TempDir(), "--chunk-size", "0")
	require.Equal(t, exitFailure, r.code)
	require.False(t, r.started)
	require.Contains(t, r.stderr, "invalid configuration: chunk size must be positive")
	require.Contains(t, r.stderr, "INDEXER_* environment variables")
}

func TestIndexerFailureExitsOne(t *testing.T) {
	path := configFile(t, baseConfig)
	r := run(t, errors.New("node unreachable"), "--config", path, "--log-level", "error")
	require.True(t, r.started)
	require.Equal(t, exitFailure, r.code)
	require.Contains(t, r.stderr, "Error: node unreachable")
}

// TestResumeCheck: the command runs only the resume check, also with the
// global flags before it, as the container entrypoint gives them.
func TestResumeCheck(t *testing.T) {
	path := configFile(t, baseConfig)
	for _, args := range [][]string{
		{"resume-check", "--config", path, "--log-level", "error"},
		{"--config", path, "--log-level", "error", "resume-check"},
	} {
		r := run(t, nil, args...)
		require.Zero(t, r.code, r.stderr)
		require.True(t, r.resumed)
		require.False(t, r.started)
		require.Equal(t, 7, r.cfg.Indexer.Workers)
	}
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		r := run(t, nil, args...)
		require.Zero(t, r.code, r.stderr)
		require.Equal(t, versionText(defaultName), r.stdout)
		require.False(t, r.started)
	}
}

// TestHelp pins the root help (refresh with -update).
func TestHelp(t *testing.T) {
	r := run(t, nil, "--help")
	require.Zero(t, r.code, r.stderr)
	golden := filepath.Join("testdata", "help.txt")
	if *update {
		require.NoError(t, os.WriteFile(golden, []byte(r.stdout), 0o644))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	require.Equal(t, string(want), r.stdout)
	require.False(t, r.started)
}

// TestBuildInjectsVersion: the Makefile and the Dockerfile set the version
// variables of this package.
func TestBuildInjectsVersion(t *testing.T) {
	x := regexp.MustCompile(`-X ([^ =]+)\.(\w+)=`)
	for _, file := range []string{"../../Makefile", "../../Dockerfile"} {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		got := map[string]bool{}
		for _, m := range x.FindAllStringSubmatch(string(data), -1) {
			require.Equal(t, "github.com/0xmhha/indexer-go/cmd/cli", m[1], file)
			got[m[2]] = true
		}
		require.Equal(t, map[string]bool{"version": true, "commit": true, "buildTime": true}, got, file)
	}
}
