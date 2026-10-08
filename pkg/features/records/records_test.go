package records

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/declared"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

type saved struct {
	record *port.Record
	keys   []port.RecordKey
}

type memStore struct {
	port.Reader // unused
	saved       []saved
}

func (s *memStore) SaveRecord(_ context.Context, r *port.Record, keys []port.RecordKey) error {
	s.saved = append(s.saved, saved{r, keys})
	return nil
}

type registrar struct {
	deps     feature.Deps
	handlers []feature.BlockHandler
}

func (r *registrar) Deps() feature.Deps                 { return r.deps }
func (r *registrar) OnBlock(h feature.BlockHandler)     { r.handlers = append(r.handlers, h) }
func (r *registrar) Enabled(string) bool                { return true }
func (r *registrar) OnRollback(feature.RollbackHandler) {}

const settled = "PaymentSettled(address indexed merchant, bytes32 indexed orderId, address device, uint256 amount)"

var settlement = common.HexToAddress("0x00000000000000000000000000000000005e771e")

// TestTablesOfOneEvent: two tables of the same event each get the log,
// with their own keys; a log of the declared contract and event that does
// not decode is skipped.
func TestTablesOfOneEvent(t *testing.T) {
	spec := declared.Spec{
		Sources: []declared.Source{{Name: "settlement", Address: settlement.Hex(), Events: []string{settled}}},
		Tables: []declared.Table{
			{Name: "receipts", Source: "settlement", Event: "PaymentSettled", Keys: [][]string{{"merchant", "orderId"}}},
			{Name: "by_device", Source: "settlement", Event: "PaymentSettled", Keys: [][]string{{"device"}}},
		},
	}
	store := &memStore{}
	r := &registrar{deps: feature.Deps{Storage: store, Logger: zap.NewNop(), Settings: func(name string, into any) error {
		*(into.(*declared.Spec)) = spec
		return nil
	}}}
	require.NoError(t, recordsFeature{}.Register(r))
	require.Len(t, r.handlers, 1)

	ev, err := declared.ParseEvent(settled)
	require.NoError(t, err)
	device := common.HexToAddress("0x0de5")
	data, err := ev.Inputs.NonIndexed().Pack(device, big.NewInt(7))
	require.NoError(t, err)
	merchant, order := common.BytesToHash(common.HexToAddress("0xa1").Bytes()), common.HexToHash("0x01")
	good := &model.Log{Address: settlement, Topics: []common.Hash{ev.ID, merchant, order}, Data: data, BlockNumber: 3, Index: 4, TxHash: common.HexToHash("0x77")}
	bad := &model.Log{Address: settlement, Topics: []common.Hash{ev.ID, merchant}, Data: data, BlockNumber: 3, Index: 5}
	b := &feature.Block{Model: &model.Block{Number: 3, Time: 1234}, Receipts: []*model.Receipt{{Logs: []*model.Log{good, bad}}}}
	require.NoError(t, r.handlers[0].HandleBlock(context.Background(), b), "a log that does not decode is skipped")

	require.Len(t, store.saved, 2)
	byTable := map[string]saved{}
	for _, s := range store.saved {
		byTable[s.record.Table] = s
	}
	rec := byTable["receipts"]
	assert.Equal(t, uint64(1234), rec.record.BlockTime)
	assert.Equal(t, uint(4), rec.record.LogIndex)
	assert.Equal(t, []port.RecordKey{{ID: "merchant,orderId", Values: []string{"0x00000000000000000000000000000000000000a1", order.Hex()}}}, rec.keys)
	assert.Equal(t, []port.RecordKey{{ID: "device", Values: []string{"0x0000000000000000000000000000000000000de5"}}}, byTable["by_device"].keys)

	r = &registrar{deps: feature.Deps{Storage: store, Settings: func(string, any) error { return nil }}}
	assert.ErrorContains(t, recordsFeature{}.Register(r), "no tables", "the feature needs tables")
}
