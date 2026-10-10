package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// TestFastPathNeverBlocksAndCountsDrops: OfferBlock returns at once when
// its queue is full and counts the dropped block; Active follows the
// enabled fast settings.
func TestFastPathNeverBlocksAndCountsDrops(t *testing.T) {
	ctx := context.Background()
	svc, _ := scopedService(t, 0)
	assert.False(t, svc.Active(), "no fast setting")
	st := hookSetting("f")
	st.Delivery = DeliveryFast
	created, err := svc.CreateSetting(ctx, st)
	require.NoError(t, err)
	assert.True(t, svc.Active())

	// Not started: nothing drains the queue.
	start := time.Now()
	for i := 0; i < fastQueueBlocks+5; i++ {
		svc.OfferBlock(uint64(i), nil)
	}
	assert.Less(t, time.Since(start), time.Second, "offering never waits")
	assert.Equal(t, uint64(5), svc.FastDropped())

	require.NoError(t, svc.DeleteSetting(ctx, created.ID))
	assert.False(t, svc.Active())

	bad := hookSetting("b")
	bad.Delivery = "soon"
	_, err = svc.CreateSetting(ctx, bad)
	assert.ErrorContains(t, err, "delivery")
}

// TestFastPathEvaluatesABlockOnce: evaluating a block's events again (its
// indexing was retried) creates no second notification.
func TestFastPathEvaluatesABlockOnce(t *testing.T) {
	ctx := context.Background()
	svc, storage := scopedService(t, 0)
	st := hookSetting("f")
	st.Delivery = DeliveryFast
	st.EventTypes = []EventType{EventTypeLog}
	_, err := svc.CreateSetting(ctx, st)
	require.NoError(t, err)

	block := []events.Event{events.NewLogEvent(&types.Log{Address: common.HexToAddress("0xaa"), BlockNumber: 7,
		BlockHash: common.HexToHash("0x07"), Index: 3, Topics: []common.Hash{common.HexToHash("0x01")}})}
	require.NoError(t, svc.notifyFast(ctx, block))
	require.NoError(t, svc.notifyFast(ctx, block))
	all, err := storage.ListNotifications(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, all, 1)
}
