package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/opsguard"
)

func TestOperationsConfigLoadsStrictly(t *testing.T) {
	dir := t.TempDir()
	load := func(t *testing.T, operations string) (*Config, error) {
		t.Helper()
		text := "rpc:\n  endpoint: http://127.0.0.1:1\noperations:\n" + operations
		p := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(p, []byte(text), 0600))
		c := NewConfig()
		c.Database.Path = filepath.Join(dir, "db")
		if err := c.LoadFromFile(p); err != nil {
			return nil, err
		}
		return c, c.Validate()
	}
	c, err := load(t, fmt.Sprintf(`  halt_file: %q
  journal_file: %q
  capacity:
    filesystem_path: %q
    floor_free_bytes: 107374182400
    stop_free_bytes: 128849018880
    warn_free_bytes: 161061273600
    resume_free_bytes: 161061273600
    postgres_data_dir: %q
    postgres_budget_bytes: 171798691840
`, filepath.Join(dir, "halt.json"), filepath.Join(dir, "capacity.jsonl"), dir, filepath.Join(dir, "pg")))
	require.NoError(t, err)
	require.True(t, c.Operations.Enabled())
	ops := c.Operations.WithDefaults()
	require.Equal(t, opsguard.DefaultFilesystemInterval, ops.Capacity.FilesystemInterval)
	require.Equal(t, 5*time.Minute, ops.Capacity.PostgresInterval)
	require.EqualValues(t, opsguard.DefaultJournalMaxBytes, ops.JournalMaxBytes)

	_, err = load(t, "  halt_file: /tmp/halt.json\n  unknown: 1\n")
	require.ErrorContains(t, err, "unknown")
	_, err = load(t, fmt.Sprintf("  capacity:\n    filesystem_path: %q\n", dir))
	require.ErrorContains(t, err, "halt_file is required")
	_, err = load(t, fmt.Sprintf("  halt_file: %q\n  capacity:\n    filesystem_path: %q\n    stop_free_bytes: 10\n    warn_free_bytes: 5\n    resume_free_bytes: 5\n", filepath.Join(dir, "h"), dir))
	require.ErrorContains(t, err, "stop_free_bytes < warn_free_bytes")

	c, err = load(t, "  halt_file: /var/lib/indexer-ops/halt.json\n")
	require.NoError(t, err, "a latch alone is valid: process-wide failures still latch")
	require.False(t, c.Operations.Enabled())
}
