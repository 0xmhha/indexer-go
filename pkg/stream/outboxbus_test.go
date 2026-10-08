package stream_test

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/stream"
	"github.com/0xmhha/indexer-go/pkg/stream/streamtest"
)

// outboxHarness is a stream in a Pebble outbox. Every bus it returns reads
// the same outbox, as the nodes of one database would.
func outboxHarness(wrap func(*stream.OutboxBus) stream.Bus) streamtest.NewHarness {
	return outboxHarnessWith(stream.OutboxBusConfig{Batch: 3, Poll: time.Hour}, false, wrap)
}

// outboxHarnessWith is outboxHarness with buses of cfg; with shared, every
// NewBus returns the same bus.
func outboxHarnessWith(cfg stream.OutboxBusConfig, shared bool, wrap func(*stream.OutboxBus) stream.Bus) streamtest.NewHarness {
	return func(t *testing.T) streamtest.Harness {
		s, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })
		var mu sync.Mutex
		var buses []*stream.OutboxBus
		return streamtest.Harness{
			Append: func(t *testing.T, values ...string) {
				txCtx, tx, err := s.BeginBlock(context.Background())
				require.NoError(t, err)
				var entries []port.OutboxEntry
				for _, v := range values {
					entries = append(entries, port.OutboxEntry{Type: "t", Data: []byte(v)})
				}
				require.NoError(t, s.AppendOutbox(txCtx, entries))
				require.NoError(t, tx.Commit())
				mu.Lock()
				defer mu.Unlock()
				for _, b := range buses {
					b.Notify()
				}
			},
			NewBus: func(t *testing.T) stream.Bus {
				mu.Lock()
				if shared && len(buses) > 0 {
					b := buses[0]
					mu.Unlock()
					return b
				}
				b := stream.NewOutboxBus(s, cfg, nil)
				buses = append(buses, b)
				mu.Unlock()
				if wrap != nil {
					return wrap(b)
				}
				return b
			},
		}
	}
}

// TestOutboxBusContract runs the bus contract against OutboxBus.
func TestOutboxBusContract(t *testing.T) {
	streamtest.Run(t, outboxHarness(nil))
}

// brokenBuses violate the contract in ways it must catch.
var brokenBuses = map[string]func(*stream.OutboxBus) stream.Bus{
	// Every group consumes one shared position, as a single consumer
	// group shared by all nodes would: each entry reaches one node.
	"SharedGroup": func(b *stream.OutboxBus) stream.Bus { return sharedGroup{b} },
	// Positions are not kept: a consumer starts where its group joined.
	"ForgetsPosition": func(b *stream.OutboxBus) stream.Bus { return &forgetful{OutboxBus: b} },
}

type sharedGroup struct{ *stream.OutboxBus }

func (s sharedGroup) Join(ctx context.Context, _ string, start stream.Start) (uint64, error) {
	return s.OutboxBus.Join(ctx, "shared", start)
}

func (s sharedGroup) Consume(ctx context.Context, _ string, h stream.Handler) error {
	return s.OutboxBus.Consume(ctx, "shared", h)
}

type forgetful struct{ *stream.OutboxBus }

func (f *forgetful) Consume(ctx context.Context, group string, h stream.Handler) error {
	tmp := group + "/" + time.Now().Format(time.RFC3339Nano)
	if _, err := f.Join(ctx, tmp, stream.StartEarliest); err != nil {
		return err
	}
	return f.OutboxBus.Consume(ctx, tmp, h)
}

// TestBusContractCatchesBrokenBuses runs the contract against each broken
// bus in a child process and requires it to fail.
func TestBusContractCatchesBrokenBuses(t *testing.T) {
	if name := os.Getenv("STREAMTEST_BROKEN"); name != "" {
		streamtest.Run(t, outboxHarness(brokenBuses[name]))
		return
	}
	if testing.Short() {
		t.Skip("runs the contract once per broken bus")
	}
	for name := range brokenBuses {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestBusContractCatchesBrokenBuses$", "-test.count=1")
			cmd.Env = append(os.Environ(), "STREAMTEST_BROKEN="+name)
			out, err := cmd.CombinedOutput()
			require.Error(t, err, "the contract passed a broken bus:\n%s", out)
		})
	}
}

// TestOutboxBusPrunesBehindSlowestGroup: entries are pruned only Retain
// behind the slowest group consuming from the bus.
func TestOutboxBusPrunesBehindSlowestGroup(t *testing.T) {
	ctx := context.Background()
	s, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	for b := 0; b < 30; b++ {
		txCtx, tx, err := s.BeginBlock(ctx)
		require.NoError(t, err)
		entries := make([]port.OutboxEntry, 100)
		for i := range entries {
			entries[i] = port.OutboxEntry{Type: "t"}
		}
		require.NoError(t, s.AppendOutbox(txCtx, entries))
		require.NoError(t, tx.Commit())
	}
	bus := stream.NewOutboxBus(s, stream.OutboxBusConfig{Retain: 100, Poll: time.Hour}, nil)
	for _, g := range []string{"fast", "slow"} {
		_, err := bus.Join(ctx, g, stream.StartEarliest)
		require.NoError(t, err)
	}

	// The slow group stops after 1000 entries and stays consuming.
	slowCtx, stopSlow := context.WithCancel(ctx)
	defer stopSlow()
	release := make(chan struct{})
	slowDone := make(chan error, 1)
	var slowAt uint64
	go func() {
		slowDone <- bus.Consume(slowCtx, "slow", func(ctx context.Context, batch []port.OutboxEntry) error {
			if slowAt >= 1000 {
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			slowAt = batch[len(batch)-1].Seq
			return nil
		})
	}()
	require.Eventually(t, func() bool {
		pos, _, err := s.OutboxCursor(ctx, "slow")
		return err == nil && pos >= 1000
	}, 10*time.Second, 5*time.Millisecond)

	fastCtx, stopFast := context.WithCancel(ctx)
	fastDone := make(chan error, 1)
	go func() {
		fastDone <- bus.Consume(fastCtx, "fast", func(context.Context, []port.OutboxEntry) error { return nil })
	}()
	require.Eventually(t, func() bool {
		pos, _, err := s.OutboxCursor(ctx, "fast")
		return err == nil && pos == 3000
	}, 10*time.Second, 5*time.Millisecond)
	stopFast()
	require.ErrorIs(t, <-fastDone, context.Canceled)

	slowPos, _, err := s.OutboxCursor(ctx, "slow")
	require.NoError(t, err)
	first, err := s.ReadOutbox(ctx, 0, 1)
	require.NoError(t, err)
	require.LessOrEqual(t, first[0].Seq, slowPos-100+1, "the slow group's retained entries stay")

	// With the slow group caught up, older entries go.
	close(release)
	require.Eventually(t, func() bool {
		pos, _, err := s.OutboxCursor(ctx, "slow")
		return err == nil && pos == 3000
	}, 10*time.Second, 5*time.Millisecond)
	first, err = s.ReadOutbox(ctx, 0, 1)
	require.NoError(t, err)
	require.Greater(t, first[0].Seq, slowPos, "entries behind every group are pruned")
	stopSlow()
	require.ErrorIs(t, <-slowDone, context.Canceled)
}

// TestEphemeralOutboxBusContract runs the bus contract against an Ephemeral
// OutboxBus within one process: every "other bus" is the same instance, as
// the restarts and nodes of the contract are, for an API process, the
// consumers of its one bus.
func TestEphemeralOutboxBusContract(t *testing.T) {
	streamtest.Run(t, outboxHarnessWith(stream.OutboxBusConfig{Batch: 3, Poll: time.Hour, Ephemeral: true}, true, nil))
}

// TestEphemeralOutboxBusWritesNothing: an Ephemeral bus records no
// positions and prunes nothing, so a read-only store can serve it.
func TestEphemeralOutboxBusWritesNothing(t *testing.T) {
	ctx := context.Background()
	s, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	for i := 0; i < 5; i++ {
		txCtx, tx, err := s.BeginBlock(ctx)
		require.NoError(t, err)
		require.NoError(t, s.AppendOutbox(txCtx, []port.OutboxEntry{{Type: "t", Data: []byte("x")}}))
		require.NoError(t, tx.Commit())
	}
	bus := stream.NewOutboxBus(s, stream.OutboxBusConfig{Ephemeral: true, Retain: 1, Poll: 10 * time.Millisecond}, nil)
	_, err = bus.Join(ctx, "api", stream.StartEarliest)
	require.NoError(t, err)

	cctx, cancel := context.WithCancel(ctx)
	var got []uint64
	done := make(chan error, 1)
	go func() {
		done <- bus.Consume(cctx, "api", func(_ context.Context, batch []port.OutboxEntry) error {
			for _, e := range batch {
				got = append(got, e.Seq)
			}
			if len(got) == 5 {
				cancel()
			}
			return nil
		})
	}()
	<-done
	require.Equal(t, []uint64{1, 2, 3, 4, 5}, got)
	_, ok, err := s.OutboxCursor(ctx, "api")
	require.NoError(t, err)
	require.False(t, ok, "no position written")
	entries, err := s.ReadOutbox(ctx, 0, 0)
	require.NoError(t, err)
	require.Len(t, entries, 5, "nothing pruned")
}
