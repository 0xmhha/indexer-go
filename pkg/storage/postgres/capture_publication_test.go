package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestCapturePublicationLocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := Open(ctx, Options{DSN: testDSN(t), Schema: newTestSchema(t), MaxConns: 4})
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()
	var fsync string
	require.NoError(t, s.pool.QueryRow(ctx, "SHOW fsync").Scan(&fsync))
	if fsync != "on" {
		t.Skip("publication durability requires fsync=on")
	}
	ch := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	require.NoError(t, s.CaptureUpdate(ctx, ch, func(w port.CaptureWriter) error {
		if err := w.Put([]byte("state"), []byte("prefix-one")); err != nil {
			return err
		}
		return w.Put([]byte("facts/one"), []byte("immutable"))
	}))
	bound, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, s.CaptureProjectionView(bound, ch, func(r port.CaptureReader) error {
		v, err := r.Get([]byte("facts/one"))
		require.Equal(t, []byte("immutable"), v)
		return err
	}))
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- s.CaptureAppend(ctx, ch, func(w port.CaptureWriter) error {
			if err := w.Put([]byte("facts/two"), []byte("second")); err != nil {
				return err
			}
			if err := w.Put([]byte("state"), []byte("prefix-two")); err != nil {
				return err
			}
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	defer func() { close(release); require.NoError(t, <-done) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("append blocked behind prefix validation", ctx.Err())
	}
	// A new exact receipt must commit while BOTH independent fences are held.
	probe, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	require.NoError(t, s.CaptureReceipt(probe, ch, func(w port.CaptureWriter) error { return w.Put([]byte("projection/one"), []byte("receipt")) }))
	require.NoError(t, s.CaptureView(probe, ch, func(r port.CaptureReader) error {
		v, err := r.Get([]byte("state"))
		require.NoError(t, err)
		require.Equal(t, []byte("prefix-one"), v)
		_, err = r.Get([]byte("facts/two"))
		require.ErrorIs(t, err, port.ErrNotFound)
		return nil
	}), "unbound snapshot must not expose uncommitted source rows")
	for name, update := range map[string]func(context.Context, model.FeedChain, func(port.CaptureWriter) error) error{"reset": s.CaptureUpdate, "second-append": s.CaptureAppend} {
		blocked, stop := context.WithTimeout(ctx, 15*time.Millisecond)
		err := update(blocked, ch, func(port.CaptureWriter) error { t.Errorf("%s crossed active fence", name); return nil })
		stop()
		require.Error(t, err)
	}
	// This explicit rollback releases only the writer's prefix fence. The
	// deferred release then lets append commit and all callbacks are joined.
	tx.Rollback()
}

func TestCaptureReceiptRaceImmutable(t *testing.T) {
	s := newTestStore(t)
	ch := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	var fsync string
	require.NoError(t, s.pool.QueryRow(t.Context(), "SHOW fsync").Scan(&fsync))
	if fsync != "on" {
		t.Skip("receipts require fsync=on")
	}
	require.NoError(t, s.CaptureUpdate(t.Context(), ch, func(port.CaptureWriter) error { return nil }))
	for _, mode := range []string{"same", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			key := []byte("projection/'\"; DROP TABLE capture_records; --/" + mode)
			ready, release := make(chan struct{}, 2), make(chan struct{})
			var wg sync.WaitGroup
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			var result [2]error
			defer func() { unblock(); cancel(); wg.Wait() }()
			for i := range result {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					result[i] = s.CaptureReceipt(ctx, ch, func(w port.CaptureWriter) error {
						if _, err := w.Get(key); !errors.Is(err, port.ErrNotFound) {
							return errors.New("receipt must initially be absent")
						}
						ready <- struct{}{}
						select {
						case <-release:
						case <-ctx.Done():
							return ctx.Err()
						}
						value := []byte("same")
						if mode == "conflict" && i == 1 {
							value = []byte("other")
						}
						return w.Put(key, value)
					})
				}(i)
			}
			require.Eventually(t, func() bool { return len(ready) == 2 }, time.Second, time.Millisecond)
			unblock()
			wg.Wait()
			if mode == "same" {
				require.NoError(t, result[0])
				require.NoError(t, result[1])
			} else {
				require.True(t, (result[0] == nil) != (result[1] == nil), "only one conflicting receipt may commit: %v", result)
				if result[0] != nil {
					require.ErrorIs(t, result[0], port.ErrInvalidData)
				} else {
					require.ErrorIs(t, result[1], port.ErrInvalidData)
				}
			}
			require.NoError(t, s.CaptureView(ctx, ch, func(r port.CaptureReader) error {
				value, err := r.Get(key)
				want := "same"
				if mode == "conflict" && result[1] == nil {
					want = "other"
				}
				require.Equal(t, want, string(value))
				return err
			}))
		})
	}
}

func TestCapturePublicationFullViewAndWriteGuards(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	ch := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	var fsync string
	require.NoError(t, s.pool.QueryRow(ctx, "SHOW fsync").Scan(&fsync))
	for _, update := range []func(context.Context, model.FeedChain, func(port.CaptureWriter) error) error{s.CaptureUpdate, s.CaptureAppend, s.CaptureReceipt} {
		if fsync != "on" {
			err := update(ctx, ch, func(port.CaptureWriter) error { t.Error("fsync-off callback"); return nil })
			require.ErrorContains(t, err, "fsync must be on")
		}
	}
	if fsync != "on" {
		return
	}
	require.NoError(t, s.CaptureUpdate(ctx, ch, func(w port.CaptureWriter) error { return w.Put([]byte("facts/one"), []byte("immutable")) }))
	bound, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, s.CaptureView(bound, ch, func(r port.CaptureReader) error { _, err := r.Get([]byte("facts/one")); return err }))
	for _, update := range []func(context.Context, model.FeedChain, func(port.CaptureWriter) error) error{s.CaptureAppend, s.CaptureReceipt} {
		blocked, stop := context.WithTimeout(ctx, 15*time.Millisecond)
		err := update(blocked, ch, func(port.CaptureWriter) error { t.Error("full view must freeze all writes"); return nil })
		stop()
		require.Error(t, err)
		require.ErrorIs(t, update(bound, ch, func(port.CaptureWriter) error { t.Fatal("nested callback"); return nil }), port.ErrInvalidData)
	}
	tx.Rollback()
	ro := openTestStore(t, s.schema, true)
	for _, update := range []func(context.Context, model.FeedChain, func(port.CaptureWriter) error) error{ro.CaptureAppend, ro.CaptureReceipt} {
		require.ErrorIs(t, update(ctx, ch, func(port.CaptureWriter) error { t.Fatal("read-only callback"); return nil }), port.ErrReadOnly)
	}
	for _, trial := range []struct {
		receipt bool
		key     string
	}{{false, "facts/one"}, {false, "projection/escape"}, {true, "state"}, {true, "facts/escape"}} {
		update := s.CaptureAppend
		if trial.receipt {
			update = s.CaptureReceipt
		}
		err := update(ctx, ch, func(w port.CaptureWriter) error {
			_ = w.Put([]byte(trial.key), []byte("different")) // ignored errors must still abort
			return nil
		})
		require.ErrorIs(t, err, port.ErrInvalidData)
	}
	for _, trial := range []struct {
		key     string
		receipt bool
	}{{"facts/retry", false}, {"projection/retry", true}} {
		update := s.CaptureAppend
		if trial.receipt {
			update = s.CaptureReceipt
		}
		put := func(w port.CaptureWriter) error { return w.Put([]byte(trial.key), []byte("same")) }
		require.NoError(t, update(ctx, ch, put))
		require.NoError(t, update(ctx, ch, put))
		boom := errors.New("abort publication")
		require.ErrorIs(t, update(ctx, ch, func(w port.CaptureWriter) error {
			if err := w.Put([]byte(trial.key+"/rollback"), []byte("never")); err != nil {
				return err
			}
			return boom
		}), boom)
		require.NoError(t, s.CaptureView(ctx, ch, func(r port.CaptureReader) error {
			_, err := r.Get([]byte(trial.key + "/rollback"))
			require.ErrorIs(t, err, port.ErrNotFound)
			return nil
		}))
	}
}
