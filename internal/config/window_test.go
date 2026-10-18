package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowStorageConfigLoadsStrictlyWithoutIO(t *testing.T) {
	root := filepath.Join(t.TempDir(), "unmounted")
	text := fmt.Sprintf(`rpc:
  endpoint: http://127.0.0.1:1
  rate_limit: 5
window_storage:
  volumes:
    external:
      root: %q
      id: external-fixture
      max_bytes: 5242880
  windows:
    arc:
      volume: external
      path: %q
      max_bytes: 2097152
      segment_bytes: 131072
      max_payload_bytes: 32768
      max_segment_records: 8
      retention: 24h
`, root, filepath.Join(root, "arc"))
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(text), 0600))
	c := NewConfig()
	c.Database.Path = filepath.Join(t.TempDir(), "db")
	require.NoError(t, c.LoadFromFile(p))
	require.NoError(t, c.Validate())
	require.True(t, c.WindowStorage.Configured())
	require.EqualValues(t, 5, c.RPC.RateLimit)
	require.False(t, c.WindowStorage.Volumes["external"].AllowDirectory)
	_, err := os.Stat(root)
	require.ErrorIs(t, err, os.ErrNotExist)
	for _, typo := range []string{"max_bytez", "retension"} {
		bad := strings.Replace(text, "max_bytes:", typo+":", 1)
		if typo == "retension" {
			bad = strings.Replace(text, "retention:", typo+":", 1)
		}
		require.NoError(t, os.WriteFile(p, []byte(bad), 0600))
		require.Error(t, NewConfig().LoadFromFile(p))
	}
}
