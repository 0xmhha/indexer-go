package opsguard

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/window"
)

const gib = int64(1) << 30

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		reason Reason
		ok     bool
	}{
		{"nil", nil, "", false},
		{"plain", errors.New("rpc timeout"), "", false},
		{"canceled", context.Canceled, "", false},
		{"halt", fmt.Errorf("wrapped: %w", &HaltError{Reason: ReasonCapacity}), ReasonCapacity, true},
		{"window capacity", window.ErrCapacity, ReasonCapacity, true},
		{"window unavailable", fmt.Errorf("feed: %w", window.ErrUnavailable), ReasonStorage, true},
		{"window corrupt", window.ErrCorrupt, ReasonStorage, true},
		{"objects", capture.ErrObjectsUnavailable, ReasonStorage, true},
		{"window conflict", window.ErrConflict, ReasonIntegrity, true},
		{"capture conflict", errors.Join(errors.New("x"), capture.ErrConflict), ReasonIntegrity, true},
		{"finality", capture.ErrFinalityConflict, ReasonIntegrity, true},
		{"rebuild", capture.ErrRebuildRequired, ReasonIntegrity, true},
		{"canonical is retried", capture.ErrCanonical, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, ok := Classify(tc.err)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.reason, r)
		})
	}
	require.True(t, ReasonCapacity.ProcessWide())
	require.True(t, ReasonStorage.ProcessWide())
	require.False(t, ReasonIntegrity.ProcessWide())
}

func TestLatchKeepsFirstCauseAndBlocksStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "halt.json")
	require.NoError(t, CheckStartup(path))
	l, err := ReadLatch(path)
	require.NoError(t, err)
	require.Nil(t, l)
	at := time.Date(2026, 10, 20, 1, 2, 3, 0, time.UTC)
	long := make([]byte, 2*maxDetail)
	for i := range long {
		long[i] = 'x'
	}
	require.NoError(t, WriteLatch(path, Latch{Reason: ReasonCapacity, Source: SourceFilesystem, Detail: string(long), At: at}))
	require.NoError(t, WriteLatch(path, Latch{Reason: ReasonStorage, Detail: "later", At: at.Add(time.Hour)}))
	l, err = ReadLatch(path)
	require.NoError(t, err)
	require.Equal(t, ReasonCapacity, l.Reason)
	require.Equal(t, SourceFilesystem, l.Source)
	require.Len(t, l.Detail, maxDetail)
	require.True(t, at.Equal(l.At))
	err = CheckStartup(path)
	require.ErrorIs(t, err, ErrHalted)
	require.Contains(t, err.Error(), "--resume-check")
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary file is left")

	require.NoError(t, os.WriteFile(path, []byte("{garbage"), 0o600))
	l, err = ReadLatch(path)
	require.NoError(t, err)
	require.Equal(t, ReasonIntegrity, l.Reason, "an unreadable latch still blocks")
	require.ErrorIs(t, CheckStartup(path), ErrHalted)
	require.Error(t, WriteLatch("", Latch{}))
}

func TestJournalRotatesWithinBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capacity.jsonl")
	j, err := OpenJournal(path, 200)
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		require.NoError(t, j.Write(Record{Kind: "filesystem", Name: fmt.Sprint(i)}))
	}
	require.NoError(t, j.Close())
	require.ErrorIs(t, j.Write(Record{}), os.ErrClosed)
	for _, p := range []string{path, path + ".1"} {
		st, err := os.Stat(p)
		require.NoError(t, err)
		require.LessOrEqual(t, st.Size(), int64(200))
	}
	var nilJournal *Journal
	require.NoError(t, nilJournal.Write(Record{}))
	require.NoError(t, nilJournal.Close())
}

func TestTreeBytesBoundsAndFollowsNoLinks(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "base", "1"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "base", "1", "a"), make([]byte, 64<<10), 0o600))
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "big"), make([]byte, 1<<20), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))
	n, err := TreeBytes(context.Background(), root, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(64<<10))
	require.Less(t, n, int64(1<<20), "the symbolic link is not followed")
	_, err = TreeBytes(context.Background(), root, 2)
	require.ErrorContains(t, err, "more than 2 entries")
	_, err = TreeBytes(context.Background(), filepath.Join(root, "missing"), 0)
	require.Error(t, err)
}

func validConfig(dir string) Config {
	return Config{HaltFile: filepath.Join(dir, "halt.json"), Capacity: &CapacityConfig{
		FilesystemPath: dir, FloorFreeBytes: 100 * gib, StopFreeBytes: 120 * gib, WarnFreeBytes: 150 * gib, ResumeFreeBytes: 150 * gib,
		PostgresDataDir: filepath.Join(dir, "pg"), PostgresBudgetBytes: 160 * gib,
	}}
}

func TestConfigValidation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Config{}.WithDefaults().Validate())
	require.False(t, Config{}.Enabled())
	ok := validConfig(dir).WithDefaults()
	require.NoError(t, ok.Validate())
	require.True(t, ok.Enabled())
	require.Equal(t, DefaultFilesystemInterval, ok.Capacity.ResumeInterval)
	require.Equal(t, DefaultResumeSamples, ok.Capacity.ResumeSamples)
	for _, tc := range []struct {
		name   string
		change func(*Config)
		want   string
	}{
		{"no latch", func(c *Config) { c.HaltFile = "" }, "halt_file is required"},
		{"relative latch", func(c *Config) { c.HaltFile = "halt.json" }, "halt_file must be absolute"},
		{"relative journal", func(c *Config) { c.JournalFile = "j" }, "journal_file must be absolute"},
		{"relative path", func(c *Config) { c.Capacity.FilesystemPath = "data" }, "filesystem_path"},
		{"threshold order", func(c *Config) { c.Capacity.StopFreeBytes = c.Capacity.WarnFreeBytes }, "floor_free_bytes < stop_free_bytes"},
		{"short interval", func(c *Config) { c.Capacity.FilesystemInterval = time.Millisecond }, "filesystem_interval"},
		{"missing shorter", func(c *Config) { c.Capacity.FilesystemMissingStop = 10 * time.Second }, "filesystem_missing_stop"},
		{"no budget", func(c *Config) { c.Capacity.PostgresBudgetBytes = 0 }, "postgres_budget_bytes must be positive"},
		{"budget without dir", func(c *Config) { c.Capacity.PostgresDataDir = "" }, "needs postgres_data_dir"},
		{"samples", func(c *Config) { c.Capacity.ResumeSamples = 101 }, "resume_samples"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig(dir)
			v := *c.Capacity
			c.Capacity = &v
			tc.change(&c)
			require.ErrorContains(t, c.WithDefaults().Validate(), tc.want)
		})
	}
	require.ErrorContains(t, Config{JournalFile: "/j"}.Validate(), "needs operations.capacity")
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) get() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) add(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

func newTestMonitor(t *testing.T) (*Monitor, *clock, *[]*HaltError, Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := validConfig(dir).WithDefaults()
	cfg.JournalFile = filepath.Join(dir, "capacity.jsonl")
	c := &clock{now: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	var halts []*HaltError
	m, err := NewMonitor(cfg, func(h *HaltError) { halts = append(halts, h) }, WithClock(c.get))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	return m, c, &halts, cfg
}

func journalKinds(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var kinds []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		kinds = append(kinds, s.Text())
	}
	return kinds
}

func TestMonitorFilesystemThresholds(t *testing.T) {
	m, c, halts, cfg := newTestMonitor(t)
	m.ObserveFilesystem(c.add(30*time.Second), uint64(149*gib), nil)
	require.Equal(t, LevelNormal, m.levels["filesystem"], "one low sample is not a warning")
	m.ObserveFilesystem(c.add(30*time.Second), uint64(149*gib), nil)
	require.Equal(t, LevelWarning, m.levels["filesystem"])
	require.Empty(t, *halts)
	m.ObserveFilesystem(c.add(30*time.Second), uint64(120*gib), nil)
	require.Len(t, *halts, 1)
	require.Equal(t, SourceFilesystem, (*halts)[0].Source)
	m.ObserveFilesystem(c.add(30*time.Second), uint64(10*gib), nil)
	require.Len(t, *halts, 1, "a monitor halts once")
	select {
	case <-m.Done():
	default:
		t.Fatal("Done is not closed")
	}
	l, err := ReadLatch(cfg.HaltFile)
	require.NoError(t, err)
	require.Equal(t, ReasonCapacity, l.Reason)
	require.Equal(t, SourceFilesystem, l.Source)
	require.Len(t, journalKinds(t, cfg.JournalFile), 5)
}

func TestMonitorFilesystemForecast(t *testing.T) {
	m, c, halts, _ := newTestMonitor(t)
	// 1 GiB per minute toward a floor 140 GiB away: about 2h20m, no stop.
	free := 240 * gib
	for i := 0; i < 4; i++ {
		m.ObserveFilesystem(c.add(time.Minute), uint64(free), nil)
		free -= gib
	}
	require.Empty(t, *halts)
	// A burst of 40 GiB in a minute forecasts the floor within the limit.
	m.ObserveFilesystem(c.add(time.Minute), uint64(free-40*gib), nil)
	require.Len(t, *halts, 1)
	require.Contains(t, (*halts)[0].Detail, "reaches floor_free_bytes")
}

func TestMonitorForecastResetsWhenClockGoesBack(t *testing.T) {
	m, c, halts, _ := newTestMonitor(t)
	m.ObserveFilesystem(c.add(time.Minute), uint64(300*gib), nil)
	m.ObserveFilesystem(c.get().Add(-time.Hour), uint64(200*gib), nil)
	m.ObserveFilesystem(c.add(time.Minute), uint64(199*gib), nil)
	require.Empty(t, *halts, "a decline measured across a clock step is not used")
}

func TestMonitorPostgresThresholds(t *testing.T) {
	m2, c2, halts2, _ := newTestMonitor(t)
	m2.ObservePostgres(c2.add(5*time.Minute), 129*gib, nil)
	m2.ObservePostgres(c2.add(5*time.Minute), 129*gib, nil)
	require.Equal(t, LevelWarning, m2.levels["postgres"])
	m2.ObservePostgres(c2.add(5*time.Minute), 160*gib, nil)
	require.Len(t, *halts2, 1)
	require.Equal(t, SourcePostgres, (*halts2)[0].Source)
}

func TestMonitorPostgresForecastStops(t *testing.T) {
	m, c, halts, _ := newTestMonitor(t)
	m.ObservePostgres(c.add(5*time.Minute), 100*gib, nil)
	m.ObservePostgres(c.add(5*time.Minute), 110*gib, nil)
	require.Len(t, *halts, 1, "10 GiB per 5 minutes reaches the budget in 30 minutes")
	require.Contains(t, (*halts)[0].Detail, "reaches postgres_budget_bytes")
}

func TestMonitorMissingObservationsStop(t *testing.T) {
	m, c, halts, _ := newTestMonitor(t)
	m.ObservePostgres(c.add(time.Second), 10*gib, nil)
	m.ObserveFilesystem(c.add(time.Second), uint64(500*gib), nil)
	m.ObserveFilesystem(c.add(30*time.Second), 0, errors.New("statfs failed"))
	require.Empty(t, *halts)
	m.ObserveFilesystem(c.add(31*time.Second), 0, errors.New("statfs failed"))
	require.Len(t, *halts, 1)
	require.Contains(t, (*halts)[0].Detail, "free space unobserved")

	m2, c2, halts2, _ := newTestMonitor(t)
	for i := 0; i < 21; i++ {
		m2.ObserveFilesystem(c2.add(30*time.Second), uint64(500*gib), nil)
	}
	m2.ObservePostgres(c2.get(), 0, errors.New("permission denied"))
	require.Len(t, *halts2, 1)
	require.Contains(t, (*halts2)[0].Detail, "data directory size unobserved")
}

func TestMonitorRawUsage(t *testing.T) {
	m, _, halts, cfg := newTestMonitor(t)
	m.ObserveRaw(window.Usage{Reservation: "arc", FileBytes: 70, IndexReserveBytes: 5, LimitBytes: 100})
	m.ObserveRaw(window.Usage{Reservation: "arc", FileBytes: 80, LimitBytes: 100})
	m.ObserveRaw(window.Usage{Reservation: "arc", FileBytes: 80, LimitBytes: 100})
	require.Equal(t, LevelWarning, m.levels["raw/arc"])
	m.ObserveRaw(window.Usage{Reservation: "arc", LimitBytes: 0})
	require.Empty(t, *halts)
	m.ObserveRaw(window.Usage{Reservation: "arc", FileBytes: 90, IndexReserveBytes: 5, LimitBytes: 100})
	require.Len(t, *halts, 1)
	require.Equal(t, SourceRaw, (*halts)[0].Source)
	l, err := ReadLatch(cfg.HaltFile)
	require.NoError(t, err)
	require.Equal(t, SourceRaw, l.Source)
}

func TestMonitorRawForecast(t *testing.T) {
	m, c, halts, _ := newTestMonitor(t)
	u := window.Usage{Reservation: "arc", FileBytes: 10 * gib, LimitBytes: 100 * gib}
	for i := 0; i < 3; i++ {
		c.add(time.Minute)
		m.ObserveRaw(u)
		u.FileBytes += gib / 100
		u.AppendedBytes += gib / 100
	}
	require.Empty(t, *halts, "slow growth forecasts days")
	// A reopened window restarts its counter; that is not a negative rate.
	u.AppendedBytes = 0
	c.add(time.Minute)
	m.ObserveRaw(u)
	require.Empty(t, *halts)
	c.add(time.Minute)
	u.AppendedBytes += 2 * gib
	u.FileBytes += 2 * gib
	m.ObserveRaw(u)
	require.Len(t, *halts, 1, "2 GiB per minute reaches 95% in about 41 minutes")
	require.Contains(t, (*halts)[0].Detail, "reaches 95%")
}

func TestMonitorRunHaltsAndStops(t *testing.T) {
	dir := t.TempDir()
	cfg := validConfig(dir).WithDefaults()
	cfg.Capacity.FilesystemInterval = 5 * time.Millisecond
	cfg.Capacity.PostgresInterval = 5 * time.Millisecond
	free := make(chan uint64, 4)
	free <- uint64(500 * gib)
	free <- uint64(10 * gib)
	m, err := NewMonitor(cfg, nil, WithProbes(func(string) (uint64, error) { return <-free, nil }, func(context.Context, string, int) (int64, error) { return gib, nil }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	err = m.Run(context.Background())
	var h *HaltError
	require.ErrorAs(t, err, &h)
	require.Equal(t, SourceFilesystem, h.Source)

	cfg = validConfig(t.TempDir()).WithDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	m, err = NewMonitor(cfg, nil, WithProbes(func(string) (uint64, error) { cancel(); return uint64(500 * gib), nil }, func(context.Context, string, int) (int64, error) { return gib, nil }))
	require.NoError(t, err)
	require.NoError(t, m.Run(ctx))
	require.NoError(t, m.Close())
	require.Nil(t, m.Halted())
}

func TestMonitorRunHaltsOnBlockedProbe(t *testing.T) {
	cfg := validConfig(t.TempDir()).WithDefaults()
	cfg.Capacity.FilesystemInterval = 5 * time.Millisecond
	cfg.Capacity.FilesystemMissingStop = 20 * time.Millisecond
	cfg.Capacity.PostgresDataDir = ""
	release := make(chan struct{})
	defer close(release)
	m, err := NewMonitor(cfg, nil, WithProbes(func(string) (uint64, error) { <-release; return 0, nil }, nil))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	err = m.Run(context.Background())
	require.ErrorContains(t, err, "free space unobserved")
}

func TestResume(t *testing.T) {
	noSleep := func(context.Context, time.Duration) error { return nil }
	tree := func(context.Context, string, int) (int64, error) { return 100 * gib, nil }
	values := func(v ...int64) func(string) (uint64, error) {
		i := 0
		return func(string) (uint64, error) { x := v[i]; i++; return uint64(x), nil }
	}
	dir := t.TempDir()
	cfg := validConfig(dir)
	r, err := Resume(context.Background(), cfg, ResumeOptions{Sleep: noSleep})
	require.NoError(t, err)
	require.Nil(t, r.Latch)

	require.NoError(t, WriteLatch(cfg.HaltFile, Latch{Reason: ReasonCapacity, Source: SourceFilesystem}))
	_, err = Resume(context.Background(), cfg, ResumeOptions{Free: values(200*gib, 150*gib, 200*gib), Tree: tree, Sleep: noSleep})
	require.ErrorIs(t, err, ErrResumeRefused, "free space equal to the resume threshold is not enough")
	require.ErrorIs(t, CheckStartup(cfg.HaltFile), ErrHalted)
	_, err = Resume(context.Background(), cfg, ResumeOptions{Free: values(200*gib, 200*gib, 200*gib), Tree: func(context.Context, string, int) (int64, error) { return 128 * gib, nil }, Sleep: noSleep})
	require.ErrorIs(t, err, ErrResumeRefused)
	r, err = Resume(context.Background(), cfg, ResumeOptions{Free: values(200*gib, 200*gib, 200*gib), Tree: tree, Sleep: noSleep})
	require.NoError(t, err)
	require.True(t, r.Cleared)
	require.Len(t, r.FreeBytes, 3)
	require.NoError(t, CheckStartup(cfg.HaltFile))

	for _, l := range []Latch{{Reason: ReasonCapacity, Source: SourceRaw}, {Reason: ReasonIntegrity}} {
		require.NoError(t, WriteLatch(cfg.HaltFile, l))
		_, err = Resume(context.Background(), cfg, ResumeOptions{Free: values(500*gib, 500*gib, 500*gib), Tree: tree, Sleep: noSleep})
		require.ErrorIs(t, err, ErrResumeRefused)
		require.NoError(t, os.Remove(cfg.HaltFile))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, WriteLatch(cfg.HaltFile, Latch{Reason: ReasonStorage}))
	_, err = Resume(ctx, cfg, ResumeOptions{Free: values(500*gib, 500*gib, 500*gib), Tree: tree})
	require.ErrorIs(t, err, context.Canceled)
}
