package opsguard

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeStats struct {
	db, wal       int64
	dbErr, walErr error
	block         chan struct{} // DatabaseBytes waits for ctx when set
}

func (f *fakeStats) DatabaseBytes(ctx context.Context) (int64, error) {
	if f.block != nil {
		close(f.block)
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return f.db, f.dbErr
}

func (f *fakeStats) WALBytes(context.Context) (int64, error) { return f.wal, f.walErr }

func journalRecords(t *testing.T, path string) []Record {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []Record
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var r Record
		require.NoError(t, json.Unmarshal([]byte(line), &r))
		out = append(out, r)
	}
	return out
}

// TestPostgresStatsAreJournaledAndNeverHalt: the database and WAL sizes are
// journaled; an unavailable statistic is unknown and does not halt.
func TestPostgresStatsAreJournaledAndNeverHalt(t *testing.T) {
	m, _, halts, cfg := newTestMonitor(t)
	m.sampleStats(context.Background())
	require.Empty(t, journalRecords(t, cfg.JournalFile), "nothing is sampled without a source")

	first := &fakeStats{db: 7 * gib, wal: gib}
	detachFirst := m.AttachPostgresStats(first)
	second := &fakeStats{db: 9 * gib, walErr: errors.New("permission denied for function pg_ls_waldir")}
	detachSecond := m.AttachPostgresStats(second)
	m.sampleStats(context.Background())
	detachSecond()
	detachSecond()
	m.sampleStats(context.Background())
	detachFirst()
	m.sampleStats(context.Background())
	require.Empty(t, *halts)
	require.Nil(t, m.Halted())

	recs := journalRecords(t, cfg.JournalFile)
	require.Len(t, recs, 4)
	require.Equal(t, KindPostgresDatabase, recs[0].Kind)
	require.Equal(t, 9*gib, *recs[0].Bytes, "the latest attached source is sampled")
	require.Equal(t, KindPostgresWAL, recs[1].Kind)
	require.Equal(t, LevelUnknown, recs[1].Level)
	require.Nil(t, recs[1].Bytes)
	require.Contains(t, recs[1].Error, "pg_ls_waldir")
	require.Equal(t, 7*gib, *recs[2].Bytes, "detach falls back to the earlier source")
	require.Equal(t, LevelNormal, recs[3].Level)
	require.Equal(t, gib, *recs[3].Bytes)
	require.Equal(t, LevelNormal, m.levels[KindPostgresWAL], "the WAL statistic recovered")
}

// TestMonitorRunWaitsForStatsQuery: Run cancels a statistics query in
// progress and returns only after it ended.
func TestMonitorRunWaitsForStatsQuery(t *testing.T) {
	cfg := validConfig(t.TempDir()).WithDefaults()
	cfg.Capacity.PostgresDataDir = ""
	m, err := NewMonitor(cfg, nil, WithProbes(func(string) (uint64, error) { return uint64(500 * gib), nil }, nil))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	stats := &fakeStats{block: make(chan struct{})}
	m.AttachPostgresStats(stats)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	<-stats.block
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancelling the statistics query")
	}
}

// TestMonitorRunSamplesStatsPeriodically: statistics are sampled at their
// own interval, independent of the data directory sampling.
func TestMonitorRunSamplesStatsPeriodically(t *testing.T) {
	dir := t.TempDir()
	cfg := validConfig(dir).WithDefaults()
	cfg.JournalFile = filepath.Join(dir, "capacity.jsonl")
	cfg.Capacity.PostgresDataDir, cfg.Capacity.PostgresBudgetBytes = "", 0
	cfg.Capacity.PostgresStatsInterval = 5 * time.Millisecond
	require.Equal(t, DefaultPostgresStatsInterval, validConfig(dir).WithDefaults().Capacity.PostgresStatsInterval)
	m, err := NewMonitor(cfg, nil, WithProbes(func(string) (uint64, error) { return uint64(500 * gib), nil }, nil))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	sampled := make(chan struct{}, 16)
	m.AttachPostgresStats(&countingStats{sampled: sampled})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	for i := 0; i < 3; i++ {
		select {
		case <-sampled:
		case <-time.After(10 * time.Second):
			t.Fatalf("statistics sampled %d times", i)
		}
	}
	cancel()
	require.NoError(t, <-done)
}

type countingStats struct{ sampled chan struct{} }

func (c *countingStats) DatabaseBytes(context.Context) (int64, error) {
	select {
	case c.sampled <- struct{}{}:
	default:
	}
	return gib, nil
}

func (c *countingStats) WALBytes(context.Context) (int64, error) { return gib, nil }
