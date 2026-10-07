package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// startAppOutbox starts an app on dir with the outbox on or off.
func startAppOutbox(t *testing.T, srv *testchain.Server, dir string, outbox bool) *App {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Database.Path = dir
	cfg.API.Enabled = false
	cfg.Indexer.PollInterval = 10 * time.Millisecond
	cfg.EventBus.Outbox = outbox
	enableTestChainFeatures(cfg)
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	return app
}

// allEventTypes are the types the indexer publishes for blocks.
func allEventTypes() []events.EventType {
	return append([]events.EventType{
		events.EventTypeBlock, events.EventTypeTransaction, events.EventTypeLog, events.EventTypeReorg,
		events.EventTypeChainConfig, events.EventTypeValidatorSet,
	}, events.CodecTypes()...)
}

// drain reads sub until no event arrives for a while or the bus closed it.
func drain(t *testing.T, sub *events.Subscription) []events.Event {
	t.Helper()
	var out []events.Event
	for {
		select {
		case ev, ok := <-sub.Channel:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-time.After(500 * time.Millisecond):
			return out
		}
	}
}

// eventKey describes an event without the wall-clock times it was created
// at, so the events of two runs compare equal.
func eventKey(t *testing.T, ev events.Event) string {
	t.Helper()
	data, err := events.MarshalEvent(ev)
	require.NoError(t, err)
	var v map[string]any
	require.NoError(t, json.Unmarshal(data, &v))
	for k := range v {
		switch strings.ToLower(strings.ReplaceAll(k, "_", "")) {
		case "createdat", "detectedat":
			delete(v, k)
		}
	}
	out, err := json.Marshal(v)
	require.NoError(t, err)
	return string(ev.Type()) + " " + string(out)
}

// requireSequence requires the events to be numbered 1..len in order.
func requireSequence(t *testing.T, evs []events.Event, first uint64) {
	t.Helper()
	for i, ev := range evs {
		require.Equal(t, first+uint64(i), events.SequenceOf(ev), "event %d (%s)", i, ev.Type())
	}
}

// TestOutboxDeliversTheSameEvents indexes each scenario, the reference one
// with a reorganization, once with the outbox and once publishing directly
// (refactoring plan R3-1, defect D8): subscribers must receive the same
// events in the same order, and through the outbox numbered 1, 2, ...
func TestOutboxDeliversTheSameEvents(t *testing.T) {
	for _, name := range []string{"evm", "stablenet"} {
		t.Run(name, func(t *testing.T) {
			run := func(outbox bool) []events.Event {
				var sc *testchain.Scenario
				if name == "evm" {
					sc = testchain.BuildDefault()
				} else {
					sc = &testchain.BuildStableNet().Scenario
				}
				srv := testchain.NewServer(sc.Chain)
				defer srv.Close()
				app := startAppOutbox(t, srv, filepath.Join(t.TempDir(), "db"), outbox)
				defer app.Shutdown()
				sub := app.eventBus.Subscribe("all", allEventTypes(), nil, 1<<16)

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				head := sc.Chain.Head()
				require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
				if name == "evm" {
					reorgChain(sc, head-3, 5)
					newHead := sc.Chain.Head()
					loopCtx, stop := context.WithCancel(ctx)
					done := make(chan error, 1)
					go func() { done <- app.fetcher.Run(loopCtx) }()
					require.Eventually(t, func() bool {
						b, err := app.storage.GetBlock(ctx, newHead)
						return err == nil && b.Extra != nil
					}, time.Minute, 20*time.Millisecond)
					stop()
					<-done
				}
				return drain(t, sub)
			}

			direct := run(false)
			viaOutbox := run(true)
			require.NotEmpty(t, direct)
			for _, ev := range direct {
				require.Zero(t, events.SequenceOf(ev), "direct events are not numbered")
			}
			requireSequence(t, viaOutbox, 1)
			require.Equal(t, len(direct), len(viaOutbox))
			for i := range direct {
				require.Equal(t, eventKey(t, direct[i]), eventKey(t, viaOutbox[i]), "event %d", i)
			}
			if name == "evm" {
				var reorgs int
				for _, ev := range viaOutbox {
					if ev.Type() == events.EventTypeReorg {
						reorgs++
					}
				}
				require.Equal(t, 1, reorgs)
			}
		})
	}
}

// TestOutboxSequenceSurvivesCrashAndRestart: a block whose commit fails
// publishes nothing; the app is stopped while the relay may still be behind
// and started again on the database. A consumer that remembers the last
// sequence it saw receives every event exactly once, numbered without gaps,
// and the numbering matches a run without crash.
func TestOutboxSequenceSurvivesCrashAndRestart(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The events of a run without crash.
	clean := startAppOutbox(t, srv, filepath.Join(t.TempDir(), "clean"), true)
	cleanSub := clean.eventBus.Subscribe("all", allEventTypes(), nil, 1<<16)
	require.NoError(t, clean.fetcher.FetchRange(ctx, 0, head))
	want := drain(t, cleanSub)
	clean.Shutdown()
	requireSequence(t, want, 1)

	dir := filepath.Join(t.TempDir(), "db")
	var got []events.Event
	var last uint64 // the consumer's position, kept across restarts
	consume := func(evs []events.Event) {
		for _, ev := range evs {
			if s := events.SequenceOf(ev); s > last {
				require.Equal(t, last+1, s, "no gap")
				last = s
				got = append(got, ev)
			}
		}
	}

	// Session 1 fails to commit block 6.
	crash := errors.New("crash before commit")
	app := startAppOutbox(t, srv, dir, true)
	sub := app.eventBus.Subscribe("all", allEventTypes(), nil, 1<<16)
	app.fetcher.SetBeforeCommitHook(func(h uint64) error {
		if h == 6 {
			return crash
		}
		return nil
	})
	require.ErrorIs(t, app.fetcher.FetchRange(ctx, 0, head), crash)
	consume(drain(t, sub))
	for _, ev := range got {
		require.Less(t, eventBlock(ev), uint64(6), "no event of the failed block")
	}

	// Session 2 indexes a few more blocks and stops at once, before the
	// relay may have caught up.
	app.Shutdown()
	app = startAppOutbox(t, srv, dir, true)
	sub = app.eventBus.Subscribe("all", allEventTypes(), nil, 1<<16)
	app.fetcher.StartRelay()
	require.NoError(t, app.fetcher.FetchRange(ctx, 6, 9))
	app.Shutdown()
	consume(drain(t, sub))

	// Session 3 delivers what is left and indexes the rest.
	app = startAppOutbox(t, srv, dir, true)
	defer app.Shutdown()
	sub = app.eventBus.Subscribe("all", allEventTypes(), nil, 1<<16)
	app.fetcher.StartRelay()
	require.NoError(t, app.fetcher.FetchRange(ctx, 10, head))
	consume(drain(t, sub))

	require.Equal(t, len(want), len(got))
	for i := range want {
		require.Equal(t, eventKey(t, want[i]), eventKey(t, got[i]), "event %d", i)
	}
}

// eventBlock returns the block an event belongs to.
func eventBlock(ev events.Event) uint64 {
	switch e := ev.(type) {
	case *events.BlockEvent:
		return e.Number
	case *events.TransactionEvent:
		return e.BlockNumber
	case *events.LogEvent:
		return e.Log.BlockNumber
	}
	panic(fmt.Sprintf("event %T", ev))
}

// TestReindexContinuesOutboxSequence: a reindex deletes the outbox entries
// but the events of the new indexing continue the numbering, so consumers
// that drop sequences they have seen do not drop them.
func TestReindexContinuesOutboxSequence(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	ctx := context.Background()

	app := startAppOutbox(t, srv, dir, true)
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, 5))
	first, err := app.storage.(port.Outbox).LastOutboxSeq(ctx)
	require.NoError(t, err)
	require.NotZero(t, first)
	app.Shutdown()

	require.NoError(t, reindexData(dir, zap.NewNop()))

	app = startAppOutbox(t, srv, dir, true)
	defer app.Shutdown()
	sub := app.eventBus.Subscribe("all", allEventTypes(), nil, 1<<16)
	app.fetcher.StartRelay()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, 5))
	evs := drain(t, sub)
	require.NotEmpty(t, evs)
	requireSequence(t, evs, first+1)
	entries, err := app.storage.(port.Outbox).ReadOutbox(ctx, 0, 1)
	require.NoError(t, err)
	require.Equal(t, first+1, entries[0].Seq, "the old entries were deleted")
}

// waitRelayed waits until the relay delivered every committed outbox entry
// to the app's bus. Delivery follows the commit asynchronously.
func waitRelayed(t *testing.T, app *App) {
	t.Helper()
	ob := app.storage.(port.Outbox)
	require.Eventually(t, func() bool {
		last, err := ob.LastOutboxSeq(context.Background())
		return err == nil && app.eventBus.LastSequence() == last
	}, 10*time.Second, 5*time.Millisecond)
}
