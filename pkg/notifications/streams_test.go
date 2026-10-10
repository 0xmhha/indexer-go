package notifications

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// TestStreamsLimitAndFanOut: a key holds at most its connection limit;
// a message reaches every connection of its owner and no other.
func TestStreamsLimitAndFanOut(t *testing.T) {
	s := NewStreams(2)
	a1, err := s.Open("alice")
	require.NoError(t, err)
	a2, err := s.Open("alice")
	require.NoError(t, err)
	_, err = s.Open("alice")
	require.ErrorIs(t, err, ErrTooManyStreams)
	b, err := s.Open("bob")
	require.NoError(t, err)
	_, err = s.Open("")
	require.Error(t, err, "a stream needs a key")

	assert.Equal(t, 2, s.send("alice", []byte("m")))
	assert.Equal(t, "m", string(<-a1.Messages()))
	assert.Equal(t, "m", string(<-a2.Messages()))
	assert.Empty(t, b.Messages(), "bob gets none of alice's messages")

	s.Close(a1)
	s.Close(a1) // again: nothing happens
	assert.Equal(t, 1, s.Connected("alice"))
	_, err = s.Open("alice")
	require.NoError(t, err, "a closed connection frees its place")
}

// TestStreamOverflowEndsTheConnection: a connection with no room for a
// message is ended as overflowed and leaves its owner's set.
func TestStreamOverflowEndsTheConnection(t *testing.T) {
	s := NewStreams(1)
	c, err := s.Open("alice")
	require.NoError(t, err)
	for i := 0; i < streamBuffer; i++ {
		require.Equal(t, 1, s.send("alice", []byte("m")))
	}
	assert.Equal(t, 0, s.send("alice", []byte("one too many")))
	<-c.Done()
	assert.True(t, c.Overflowed())
	assert.Zero(t, s.Connected("alice"))
}

// decodeStream decodes the next message of c.
func decodeStream(t *testing.T, c *Stream) StreamMessage {
	t.Helper()
	var m StreamMessage
	select {
	case data := <-c.Messages():
		require.NoError(t, json.Unmarshal(data, &m))
	default:
		t.Fatal("no stream message")
	}
	return m
}

// TestStreamSettingSendsWithoutStoring: a stream setting needs an owner;
// its notifications go to the owner's connections and are not stored.
func TestStreamSettingSendsWithoutStoring(t *testing.T) {
	ctx := context.Background()
	svc, storage := scopedService(t, 0)
	_, err := svc.CreateSetting(ctx, &NotificationSetting{Name: "s", Type: NotificationTypeStream, Enabled: true,
		EventTypes: []EventType{EventTypeLog}})
	require.ErrorContains(t, err, "owner", "a stream setting without an owner has no stream")

	st, err := svc.CreateSetting(ctx, &NotificationSetting{Name: "s", Type: NotificationTypeStream, Enabled: true, Owner: "alice",
		Delivery: DeliveryFast, EventTypes: []EventType{EventTypeLog}})
	require.NoError(t, err)
	c, err := svc.Streams().Open("alice")
	require.NoError(t, err)
	other, err := svc.Streams().Open("bob")
	require.NoError(t, err)

	log := &types.Log{Address: common.HexToAddress("0xaa"), BlockNumber: 7, BlockHash: common.HexToHash("0x07"), Index: 3,
		Topics: []common.Hash{common.HexToHash("0x01")}}
	require.NoError(t, svc.notifyFast(ctx, []events.Event{events.NewLogEvent(log)}))
	m := decodeStream(t, c)
	assert.Equal(t, StreamNotification, m.Type)
	require.NotNil(t, m.Notification)
	assert.Equal(t, st.ID, m.Notification.SettingID)
	assert.Equal(t, uint64(7), m.Notification.Payload.BlockNumber)
	assert.NotEmpty(t, m.Notification.ID, "the id lets the client drop a message sent again")
	assert.Empty(t, other.Messages(), "only the owner's connections")

	all, err := storage.ListNotifications(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, all, "stream notifications are not stored")

	result, err := svc.TestSetting(ctx, st.ID)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, StreamNotification, decodeStream(t, c).Type)
}

// TestLaggingToldOncePerRun: when the fast path drops blocks, each owner of
// a fast setting is told once, from the first dropped block, until a block
// is queued again.
func TestLaggingToldOncePerRun(t *testing.T) {
	ctx := context.Background()
	svc, _ := scopedService(t, 0)
	st := hookSetting("f")
	st.Delivery = DeliveryFast
	st.Owner = "alice"
	_, err := svc.CreateSetting(ctx, st)
	require.NoError(t, err)
	c, err := svc.Streams().Open("alice")
	require.NoError(t, err)

	// Not started: nothing drains the queue.
	for i := 0; i < fastQueueBlocks+3; i++ {
		svc.OfferBlock(uint64(i), nil)
	}
	m := decodeStream(t, c)
	assert.Equal(t, StreamLagging, m.Type)
	assert.Equal(t, uint64(fastQueueBlocks), m.FromBlock, "from the first dropped block")
	assert.Empty(t, c.Messages(), "once per run of dropped blocks")

	<-svc.fastPath.queue // room for one block: the run ends
	svc.OfferBlock(2000, nil)
	svc.OfferBlock(2001, nil)
	assert.Equal(t, uint64(2001), decodeStream(t, c).FromBlock, "a new run is told again")
}
