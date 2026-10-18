package capture_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/window"
)

func sessionLayout(root string) window.Layout {
	return window.Layout{
		Volumes: map[string]window.VolumeSpec{"external": {Root: root, ID: "session-fixture", AllowDirectory: true, MaxBytes: 5 << 20}},
		Windows: map[string]window.WindowConfig{"arc": {Volume: "external", Path: filepath.Join(root, "arc"), MaxBytes: 2 << 20, SegmentBytes: 32 << 10, MaxPayloadBytes: 2048, MaxSegmentRecords: 2}},
	}
}

func sessionConfig(t *testing.T) capture.SessionConfig {
	t.Helper()
	l := sessionLayout(t.TempDir())
	require.NoError(t, window.InitializeVolume(l.Volumes["external"]))
	return capture.SessionConfig{Storage: l, Instance: "arc", Plan: plan, Anchor: model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}, Create: true, UsageInterval: time.Millisecond}
}

func TestConfiguredSessionRoundTrip(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			cfg := sessionConfig(t)
			refs := make(chan window.Reference, 2)
			err := capture.RunConfiguredSession(ctx, db, newOracle(), cfg,
				func(c context.Context, w *window.Store, _ *capture.Store) error {
					a := input(1, 1, common.Hash{2}, chain.GenesisHash)
					for _, p := range a.Payloads {
						r, err := w.AppendWait(c, chainfeed.Envelope{Block: a.Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Payload: p.Bytes})
						if err != nil {
							return err
						}
						refs <- r
					}
					<-c.Done()
					return c.Err()
				},
				func(c context.Context, w *window.Store, s *capture.Store) error {
					a := input(1, 1, common.Hash{2}, chain.GenesisHash)
					a.Payloads = nil
					for i := 0; i < 2; i++ {
						var ref window.Reference
						select {
						case ref = <-refs:
						case <-c.Done():
							return c.Err()
						}
						lease, err := w.Acquire(c, ref)
						if err != nil {
							return err
						}
						e, err := lease.Envelope()
						if err == nil {
							a.Payloads = append(a.Payloads, capture.Payload{Kind: e.Kind, Bytes: append([]byte(nil), e.Payload...)})
						}
						err = errors.Join(err, lease.Close())
						if err != nil {
							return err
						}
					}
					if err := s.Commit(c, a); err != nil {
						return err
					}
					return errInjected
				})
			require.ErrorIs(t, err, errInjected)
			s := service(t, db, newOracle())
			_, err = s.Read(ctx, input(1, 1, common.Hash{2}, chain.GenesisHash).Manifest.Identity)
			require.NoError(t, err)
			opts, err := cfg.Storage.OptionsFor("arc", chain)
			require.NoError(t, err)
			w, err := s.OpenWindow(ctx, opts)
			require.NoError(t, err, "all tasks and monitor must release the writer before return")
			require.Equal(t, 2, w.Stats().Records)
			require.NoError(t, w.Close())
		})
	}
}

func TestConfiguredSessionPressureKeepsProcessorRunning(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	cfg := sessionConfig(t)
	wc := cfg.Storage.Windows["arc"]
	wc.MinFreeBytes = 1 << 60
	cfg.Storage.Windows["arc"] = wc
	core, logs := observer.New(zap.InfoLevel)
	cfg.Logger = zap.New(core)
	called := make(chan struct{}, 1)
	processor := make(chan struct{})
	done := make(chan error, 1)
	c, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		done <- capture.RunConfiguredSession(c, db, newOracle(), cfg,
			func(c context.Context, _ *window.Store, _ *capture.Store) error {
				called <- struct{}{}
				<-c.Done()
				return c.Err()
			},
			func(c context.Context, _ *window.Store, _ *capture.Store) error {
				close(processor)
				<-c.Done()
				return c.Err()
			})
	}()
	select {
	case <-processor:
	case err := <-done:
		t.Fatalf("processor did not start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("processor blocked by disk pressure")
	}
	require.Eventually(t, func() bool { return logs.Len() == 1 }, time.Second, time.Millisecond)
	require.Equal(t, "critical", logs.All()[0].ContextMap()["pressure"])
	select {
	case <-called:
		t.Fatal("receiver must wait for physical headroom")
	default:
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("capacity waiter/monitor did not join")
	}
	require.Equal(t, 1, logs.Len(), "unchanged pressure must not spam every poll")
}

func TestManagedSessionMonitorCancelsAndJoinsLeaseOwner(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	cfg := sessionConfig(t)
	opts, err := cfg.Storage.OptionsFor("arc", chain)
	require.NoError(t, err)
	refs := make(chan window.Reference, 1)
	entered, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	c, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	go func() {
		done <- capture.RunConfiguredSession(c, db, newOracle(), cfg,
			func(c context.Context, w *window.Store, _ *capture.Store) error {
				a := input(1, 1, common.Hash{2}, chain.GenesisHash)
				r, e := w.Append(c, chainfeed.Envelope{Block: a.Manifest.Identity.Block, Kind: a.Payloads[0].Kind, Codec: "fixture.v1", Payload: a.Payloads[0].Bytes})
				if e != nil {
					return e
				}
				refs <- r
				<-c.Done()
				return c.Err()
			},
			func(c context.Context, w *window.Store, _ *capture.Store) error {
				var r window.Reference
				select {
				case r = <-refs:
				case <-c.Done():
					return c.Err()
				}
				lease, e := w.Acquire(c, r)
				if e != nil {
					return e
				}
				defer func() { _ = lease.Close() }()
				close(entered)
				<-c.Done()
				close(stopped)
				<-release
				return c.Err()
			})
	}()
	select {
	case <-entered:
	case e := <-done:
		t.Fatalf("lease worker did not start: %v", e)
	case <-time.After(5 * time.Second):
		t.Fatal("worker startup timeout")
	}
	root := opts.Volume.Root
	require.NoError(t, os.Rename(root, root+"-offline"))
	defer func() { require.NoError(t, os.Rename(root+"-offline", root)) }()
	select {
	case <-stopped:
	case e := <-done:
		t.Fatalf("session returned with outstanding lease: %v", e)
	case <-time.After(5 * time.Second):
		t.Fatal("idle monitor did not detect removed volume")
	}
	select {
	case e := <-done:
		t.Fatalf("session did not join lease worker: %v", e)
	default:
	}
	_, err = os.Stat(root)
	require.ErrorIs(t, err, os.ErrNotExist, "no internal disk fallback")
	close(release)
	select {
	case e := <-done:
		require.ErrorIs(t, e, window.ErrUnavailable)
	case <-time.After(5 * time.Second):
		t.Fatal("session did not join workers")
	}
}
