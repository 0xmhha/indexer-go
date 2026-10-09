package notifications

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/stream"
)

// countingHandler records every delivery by the block number of the
// notified log, which is its change stream sequence in these tests.
type countingHandler struct {
	mu    sync.Mutex
	got   map[uint64]int
	delay time.Duration
}

func (h *countingHandler) Type() NotificationType              { return NotificationTypeWebhook }
func (h *countingHandler) Validate(*NotificationSetting) error { return nil }

func (h *countingHandler) Deliver(_ context.Context, n *Notification, _ *NotificationSetting) (*DeliveryResult, error) {
	time.Sleep(h.delay)
	h.mu.Lock()
	h.got[n.Payload.BlockNumber]++
	h.mu.Unlock()
	return &DeliveryResult{Success: true}, nil
}

// delivered returns how many sequences were delivered, and how many
// deliveries repeated one.
func (h *countingHandler) delivered() (distinct, repeats int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, n := range h.got {
		distinct++
		repeats += n - 1
	}
	return distinct, repeats
}

// streamFixture is an indexer database with an outbox, and notification
// storage in the same database.
type streamFixture struct {
	db    *storage.PebbleStorage
	store Storage
	bus   *stream.OutboxBus
	seq   uint64
}

func newStreamFixture(t *testing.T) *streamFixture {
	db, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &streamFixture{
		db:    db,
		store: NewPebbleStorage(db),
		bus:   stream.NewOutboxBus(db, stream.OutboxBusConfig{Poll: 10 * time.Millisecond}, nil),
	}
}

// commit commits blocks of perBlock log events; each log's block number is
// its sequence.
func (f *streamFixture) commit(t *testing.T, blocks, perBlock int) {
	for b := 0; b < blocks; b++ {
		txCtx, tx, err := f.db.BeginBlock(context.Background())
		require.NoError(t, err)
		var entries []port.OutboxEntry
		for i := 0; i < perBlock; i++ {
			f.seq++
			data, err := events.MarshalEvent(events.NewLogEvent(&types.Log{
				Address: common.Address{0xaa}, BlockNumber: f.seq, Topics: []common.Hash{{1}},
			}))
			require.NoError(t, err)
			entries = append(entries, port.OutboxEntry{Type: string(events.EventTypeLog), Data: data})
		}
		require.NoError(t, f.db.AppendOutbox(txCtx, entries))
		require.NoError(t, tx.Commit())
	}
	f.bus.Notify()
}

// startService starts a notification service on the fixture's stream with
// a queue of four notifications and one worker, so the queue is full most of
// the time.
func (f *streamFixture) startService(t *testing.T, store Storage, h *countingHandler) *NotificationService {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Queue.BufferSize = 4
	cfg.Queue.Workers = 1
	cfg.Queue.BatchSize = 50
	cfg.Queue.FlushInterval = 10 * time.Millisecond
	svc := NewService(cfg, store, nil, zap.NewNop())
	svc.RegisterHandler(h)
	require.NoError(t, svc.SetStream(f.bus, ""))
	require.NoError(t, svc.Start(context.Background()))
	return svc
}

func (f *streamFixture) createSetting(t *testing.T, svc *NotificationService) {
	_, err := svc.CreateSetting(context.Background(), &NotificationSetting{
		ID: "logs", Name: "logs", Type: NotificationTypeWebhook, Enabled: true,
		EventTypes:  []EventType{EventTypeLog},
		Destination: Destination{WebhookURL: "http://127.0.0.1/hook"},
	})
	require.NoError(t, err)
}

func stopService(t *testing.T, svc *NotificationService) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, svc.Stop(ctx))
}

// storedNotifications counts the notifications stored per status.
func storedNotifications(t *testing.T, store Storage) map[DeliveryStatus]int {
	all, err := store.ListNotifications(context.Background(), &NotificationsFilter{SettingID: "logs", Limit: 100000})
	require.NoError(t, err)
	out := map[DeliveryStatus]int{}
	for _, n := range all {
		out[n.Status]++
	}
	return out
}

// TestNotificationsLoseNothingWithFullQueue is the R3-5 criterion: with a
// queue that is nearly always full, every event of the stream leads to
// exactly one delivered notification. Before, a full queue dropped the
// notification without storing it.
func TestNotificationsLoseNothingWithFullQueue(t *testing.T) {
	f := newStreamFixture(t)
	h := &countingHandler{got: map[uint64]int{}, delay: time.Millisecond}
	svc := f.startService(t, f.store, h)
	defer stopService(t, svc)
	f.createSetting(t, svc)

	f.commit(t, 20, 15) // 300 events
	require.Eventually(t, func() bool { d, _ := h.delivered(); return d == 300 }, time.Minute, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond) // nothing more may come
	distinct, repeats := h.delivered()
	require.Equal(t, 300, distinct)
	require.Zero(t, repeats, "each notification is delivered once")
	require.Eventually(t, func() bool { return storedNotifications(t, f.store)[DeliveryStatusSent] == 300 }, 5*time.Second, 10*time.Millisecond)
}

// TestNotificationsSurviveRestart: the service stops while events are
// being notified and a new one starts on the same database. Every event
// gets its notification once and it is delivered.
func TestNotificationsSurviveRestart(t *testing.T) {
	f := newStreamFixture(t)
	h := &countingHandler{got: map[uint64]int{}, delay: 2 * time.Millisecond}
	svc := f.startService(t, f.store, h)
	f.createSetting(t, svc)
	f.commit(t, 20, 10)
	require.Eventually(t, func() bool { d, _ := h.delivered(); return d >= 50 }, time.Minute, time.Millisecond)
	stopService(t, svc)
	f.commit(t, 10, 10) // committed while the service is down

	svc = f.startService(t, f.store, h)
	defer stopService(t, svc)
	require.Eventually(t, func() bool { d, _ := h.delivered(); return d == 300 }, time.Minute, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	distinct, repeats := h.delivered()
	require.Equal(t, 300, distinct)
	require.Zero(t, repeats, "a graceful stop lets the delivery in progress record its status")
	stored := 0
	for _, n := range storedNotifications(t, f.store) {
		stored += n
	}
	require.Equal(t, 300, stored, "one notification per event")
}

// blockingHandler behaves like a webhook whose receiver got the request
// and answers once release is closed: the delivery counts at once, and an
// answer that arrives after ctx ended is lost to the sender, as an HTTP
// client with a cancelled context reports an error.
type blockingHandler struct {
	countingHandler
	started chan struct{}
	release chan struct{}
}

func (h *blockingHandler) Deliver(ctx context.Context, n *Notification, s *NotificationSetting) (*DeliveryResult, error) {
	res, err := h.countingHandler.Deliver(ctx, n, s)
	select {
	case h.started <- struct{}{}:
	default:
	}
	<-h.release
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return res, err
}

// TestStopLetsDeliveriesRecordTheirStatus: Stop stops taking new work but
// waits for a delivery under way to finish and record it as sent. Before,
// Stop cancelled the context the status was written with, so a delivery
// that succeeded stayed pending and was sent again after a restart.
func TestStopLetsDeliveriesRecordTheirStatus(t *testing.T) {
	f := newStreamFixture(t)
	h := &blockingHandler{countingHandler: countingHandler{got: map[uint64]int{}}, started: make(chan struct{}, 1), release: make(chan struct{})}
	svc := NewService(func() *Config {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.Queue.Workers = 1
		cfg.Queue.FlushInterval = 10 * time.Millisecond
		return cfg
	}(), f.store, nil, zap.NewNop())
	svc.RegisterHandler(h)
	require.NoError(t, svc.SetStream(f.bus, ""))
	require.NoError(t, svc.Start(context.Background()))
	f.createSetting(t, svc)
	f.commit(t, 1, 1)
	select {
	case <-h.started:
	case <-time.After(10 * time.Second):
		t.Fatal("no delivery started")
	}

	stopped := make(chan struct{})
	go func() { stopService(t, svc); close(stopped) }()
	time.Sleep(50 * time.Millisecond) // Stop has cancelled the intake
	close(h.release)
	<-stopped
	require.Equal(t, map[DeliveryStatus]int{DeliveryStatusSent: 1}, storedNotifications(t, f.store))
}

// failingStore fails storing one notification once.
type failingStore struct {
	Storage
	failed atomic.Bool
	at     uint64
}

func (s *failingStore) SaveNotification(ctx context.Context, n *Notification) error {
	if n.Payload.BlockNumber == s.at && s.failed.CompareAndSwap(false, true) {
		return errors.New("disk full")
	}
	return s.Storage.SaveNotification(ctx, n)
}

// TestNotificationsRedeliveredBatchCreatesNoDuplicates: a storage error in
// the middle of a batch leaves the batch to be delivered again; the
// notifications stored before the error are not created twice.
func TestNotificationsRedeliveredBatchCreatesNoDuplicates(t *testing.T) {
	f := newStreamFixture(t)
	h := &countingHandler{got: map[uint64]int{}}
	store := &failingStore{Storage: f.store, at: 37}
	svc := f.startService(t, store, h)
	defer stopService(t, svc)
	f.createSetting(t, svc)
	f.commit(t, 1, 60)
	require.Eventually(t, func() bool { d, _ := h.delivered(); return d == 60 }, time.Minute, 10*time.Millisecond)
	require.True(t, store.failed.Load())
	time.Sleep(100 * time.Millisecond)
	_, repeats := h.delivered()
	require.Zero(t, repeats)
	stored := 0
	for _, n := range storedNotifications(t, f.store) {
		stored += n
	}
	require.Equal(t, 60, stored)
}
