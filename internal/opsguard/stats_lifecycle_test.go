package opsguard

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type joiningStats struct {
	started, cancelled, release chan struct{}
	wal                         atomic.Int32
}

func (s *joiningStats) DatabaseBytes(ctx context.Context) (int64, error) {
	close(s.started)
	<-ctx.Done()
	close(s.cancelled)
	<-s.release
	return 123, nil // An uncooperative provider returns a late apparent success.
}
func (s *joiningStats) WALBytes(context.Context) (int64, error) { s.wal.Add(1); return 1, nil }

func TestStatsDetachCancelsJoinsAndDiscardsLateResult(t *testing.T) {
	m, _, _, cfg := newTestMonitor(t)
	s := &joiningStats{started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	detach := m.AttachPostgresStats(s)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() { defer close(finished); m.sampleStats(ctx) }()
	var once sync.Once
	release := func() { once.Do(func() { close(s.release) }) }
	defer func() { cancel(); release(); <-finished }()
	<-s.started
	detached := make(chan struct{})
	go func() { detach(); close(detached) }()
	select {
	case <-detached:
		t.Fatal("detach returned without cancelling/joining its active Store read")
	case <-s.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("detach did not cancel active read")
	}
	select {
	case <-detached:
		t.Fatal("detach returned before provider joined")
	default:
	}
	release()
	<-detached
	<-finished
	cancel()
	require.Empty(t, journalRecords(t, cfg.JournalFile))
	require.Zero(t, s.wal.Load())
}
