package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/multichain"
	"github.com/0xmhha/indexer-go/internal/opsguard"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/0xmhha/indexer-go/pkg/window"
)

var testChainConfigForOps = multichain.ChainConfig{ID: "arc", Name: "arc", RPCEndpoint: "http://127.0.0.1:1", ChainID: 1}

// impossibleFree is more free space than any test filesystem has.
const impossibleFree = int64(1) << 60

func operationsConfig(dir string) opsguard.Config {
	return opsguard.Config{HaltFile: filepath.Join(dir, "halt.json"), JournalFile: filepath.Join(dir, "capacity.jsonl"), Capacity: &opsguard.CapacityConfig{
		FilesystemPath: dir, FloorFreeBytes: impossibleFree / 8, StopFreeBytes: impossibleFree / 4, WarnFreeBytes: impossibleFree / 2, ResumeFreeBytes: impossibleFree,
	}}
}

// TestCapacityHaltEndsRunAndLatches: a stop threshold ends Run with the
// halt, leaves the latch for the next start, and the resume check refuses
// while the condition holds.
func TestCapacityHaltEndsRunAndLatches(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.API.Enabled = false
	enableTestChainFeatures(cfg)
	ops := t.TempDir()
	cfg.Operations = operationsConfig(ops)
	require.NoError(t, cfg.Validate())
	require.NoError(t, opsguard.CheckStartup(cfg.Operations.HaltFile))

	a, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	t.Cleanup(a.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err = a.Run(ctx)
	var h *opsguard.HaltError
	require.ErrorAs(t, err, &h)
	require.Equal(t, opsguard.ReasonCapacity, h.Reason)
	require.Equal(t, opsguard.SourceFilesystem, h.Source)
	require.NoError(t, ctx.Err(), "the halt, not the deadline, ended Run")
	require.ErrorIs(t, opsguard.CheckStartup(cfg.Operations.HaltFile), opsguard.ErrHalted)
	_, err = opsguard.Resume(ctx, cfg.Operations, opsguard.ResumeOptions{Sleep: func(context.Context, time.Duration) error { return nil }})
	require.ErrorIs(t, err, opsguard.ErrResumeRefused)
	require.ErrorContains(t, ResumeCheck(ctx, cfg, zap.NewNop()), "resume refused")
}

// TestPostgresStatsFollowTheStore: the monitor samples a PostgreSQL store,
// and only a PostgreSQL store.
func TestPostgresStatsFollowTheStore(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.API.Enabled = false
	enableTestChainFeatures(cfg)
	cfg.Operations = operationsConfig(t.TempDir())
	require.NoError(t, cfg.Validate())

	a, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	t.Cleanup(a.Shutdown)
	require.Equal(t, testOnPostgres(), a.statsDetach != nil, "only a PostgreSQL store has statistics")
	if s, ok := postgresStats(a.storage); ok {
		n, err := s.DatabaseBytes(context.Background())
		require.NoError(t, err)
		require.Positive(t, n)
	}
}

// TestChainFailurePolicy: storage and capacity failures latch and stop the
// process, integrity failures only stop their chain's restarts, others retry.
func TestChainFailurePolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		suppress bool
		latch    opsguard.Reason
		stopDB   bool
	}{
		{"storage", fmt.Errorf("feed: %w", window.ErrUnavailable), true, opsguard.ReasonStorage, true},
		{"capacity", window.ErrCapacity, true, opsguard.ReasonCapacity, false},
		{"database disk full", fmt.Errorf("commit block: %w", &pgconn.PgError{Code: "53100"}), true, opsguard.ReasonCapacity, true},
		{"database I/O error", fmt.Errorf("commit block: %w", &pgconn.PgError{Code: "58030"}), true, opsguard.ReasonStorage, true},
		{"integrity", capture.ErrConflict, true, "", false},
		{"retryable", errors.New("rpc timeout"), false, "", false},
		{"other database error", &pgconn.PgError{Code: "40001"}, false, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.Operations.HaltFile = filepath.Join(t.TempDir(), "halt.json")
			a := &App{config: cfg, logger: zap.NewNop()}
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			a.haltRun = cancel
			require.Equal(t, tc.suppress, a.chainFailure("arc", tc.err))
			l, err := opsguard.ReadLatch(cfg.Operations.HaltFile)
			require.NoError(t, err)
			if tc.latch == "" {
				require.Nil(t, l)
				require.NoError(t, ctx.Err(), "the process keeps running")
				return
			}
			require.Equal(t, tc.latch, l.Reason)
			require.Equal(t, tc.stopDB, l.StopDatabase)
			require.Equal(t, "arc", l.Chain)
			var h *opsguard.HaltError
			require.ErrorAs(t, context.Cause(ctx), &h)
			require.Equal(t, tc.latch, h.Reason)
		})
	}
}

// TestSingleChainStorageFailureLatches: Run's own storage failure latches,
// so the process cannot simply be started again.
func TestSingleChainStorageFailureLatches(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Operations.HaltFile = filepath.Join(t.TempDir(), "halt.json")
	a := &App{config: cfg, logger: zap.NewNop()}
	a.latchFailure("", errors.New("rpc timeout"))
	require.NoError(t, opsguard.CheckStartup(cfg.Operations.HaltFile))
	a.latchFailure("", fmt.Errorf("session: %w", window.ErrCorrupt))
	require.ErrorIs(t, opsguard.CheckStartup(cfg.Operations.HaltFile), opsguard.ErrHalted)
	a.config.Operations.HaltFile = ""
	a.latchFailure("", window.ErrCorrupt) // no latch configured: nothing to write
}

// TestChainAppsHaveNoOwnMonitor: multichain chain Apps leave capacity
// monitoring and the latch to the root App.
func TestChainAppsHaveNoOwnMonitor(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Operations = operationsConfig(t.TempDir())
	a := &App{config: cfg, logger: zap.NewNop()}
	chain := a.chainAppConfig(&testChainConfigForOps)
	require.False(t, chain.Operations.Enabled())
	require.Empty(t, chain.Operations.HaltFile)
	require.True(t, a.config.Operations.Enabled(), "the root keeps its section")
}

// TestManagedFeedReportsRawUsage: the feed session's raw window usage
// reaches the capacity monitor's journal.
func TestManagedFeedReportsRawUsage(t *testing.T) {
	cfg, c, _ := feedAppConfig(t)
	dir := t.TempDir()
	cfg.Operations = opsguard.Config{HaltFile: filepath.Join(dir, "halt.json"), JournalFile: filepath.Join(dir, "capacity.jsonl"), Capacity: &opsguard.CapacityConfig{
		FilesystemPath: dir, FloorFreeBytes: 0, StopFreeBytes: 1, WarnFreeBytes: 2, ResumeFreeBytes: 2,
	}}
	a, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	cancel, done := runFeedApp(t, a)
	waitFeedProjection(t, a, c, 2)
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(cfg.Operations.JournalFile)
		return err == nil && strings.Contains(string(data), `"kind":"raw","level":"normal"`) && strings.Contains(string(data), `"name":"arc"`)
	}, 10*time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.NoError(t, opsguard.CheckStartup(cfg.Operations.HaltFile))
}
