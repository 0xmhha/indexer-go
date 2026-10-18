package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/window"
)

func TestLegacyAppRejectsUnwiredWindowBeforeIO(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer srv.Close()
	for _, multi := range []bool{false, true} {
		cfg := config.NewConfig()
		cfg.RPC.Endpoint = srv.URL
		cfg.MultiChain.Enabled = multi
		cfg.Database.Path = filepath.Join(t.TempDir(), "absent-db")
		cfg.WindowStorage = window.Layout{Volumes: map[string]window.VolumeSpec{"external": {Root: "/absent-external"}}}
		_, err := NewApp(cfg, zap.NewNop(), false, "")
		require.ErrorContains(t, err, "capture.RunConfiguredSession")
		_, err = os.Stat(cfg.Database.Path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	require.Zero(t, calls.Load(), "explicit rejection precedes RPC/DB setup")
}
