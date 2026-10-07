package events

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// TestMarshalEventRoundTrip encodes every built-in event type and decodes it
// again: what subscribers read must survive the outbox.
func TestMarshalEventRoundTrip(t *testing.T) {
	at := time.Unix(1_700_000_000, 123).UTC()
	to := common.HexToAddress("0x2")
	header := &types.Header{
		ParentHash: common.HexToHash("0xp"), Coinbase: common.HexToAddress("0xc"),
		Number: big.NewInt(9), Time: 77, Difficulty: big.NewInt(1), GasLimit: 30_000_000,
		BaseFee: big.NewInt(1000), Extra: []byte("x"),
	}
	cases := []Event{
		&BlockEvent{Block: types.NewBlockWithHeader(header), Number: 9, Hash: common.HexToHash("0xb"), TxCount: 3, CreatedAt: at},
		&TransactionEvent{Hash: common.HexToHash("0x1"), BlockNumber: 9, BlockHash: common.HexToHash("0xb"), Index: 2,
			From: common.HexToAddress("0x1"), To: &to, Value: "123456789012345678901234567890", CreatedAt: at},
		&LogEvent{Log: &types.Log{Address: to, Topics: []common.Hash{common.HexToHash("0xt")}, Data: []byte{1, 2},
			BlockNumber: 9, TxHash: common.HexToHash("0x1"), TxIndex: 2, BlockHash: common.HexToHash("0xb"), Index: 5, Removed: true}, CreatedAt: at},
		&ReorgEvent{Seq: 2, ForkNumber: 7, ForkHash: common.HexToHash("0xf"), OldHead: 9,
			Removed: []BlockRef{{Number: 9, Hash: common.HexToHash("0xb")}}, CreatedAt: at},
		&ChainConfigEvent{BlockNumber: 9, BlockHash: common.HexToHash("0xb"), Parameter: "gasLimit", OldValue: "1", NewValue: "2", CreatedAt: at},
		&ValidatorSetEvent{BlockNumber: 9, BlockHash: common.HexToHash("0xb"), ChangeType: "added",
			Validator: to, ValidatorInfo: "info", ValidatorSetSize: 4, CreatedAt: at},
		&ContractLogEvent{Contract: to, EventName: "Transfer", BlockNumber: 9, TxHash: common.HexToHash("0x1"), LogIndex: 1,
			Data: map[string]interface{}{"amount": "5"}, CreatedAt: at},
	}
	for _, ev := range cases {
		t.Run(string(ev.Type()), func(t *testing.T) {
			ev.(Sequenced).SetSequence(42)
			data, err := MarshalEvent(ev)
			require.NoError(t, err)
			got, err := UnmarshalEvent(ev.Type(), data)
			require.NoError(t, err)
			require.Zero(t, SequenceOf(got), "the sequence is not part of the value")
			got.(Sequenced).SetSequence(42)

			if b, ok := ev.(*BlockEvent); ok {
				gb := got.(*BlockEvent)
				require.Equal(t, b.Block.Header().Hash(), gb.Block.Header().Hash(), "the header survives")
				require.Equal(t, b.Block.ParentHash(), gb.Block.ParentHash())
				require.Equal(t, b.Block.Coinbase(), gb.Block.Coinbase())
				b.Block, gb.Block = nil, nil
			}
			require.Equal(t, normTime(ev), normTime(got))
		})
	}
}

// normTime returns the event's JSON with times compared as instants.
func normTime(ev Event) string {
	b, _ := json.Marshal(ev)
	return string(b)
}

// TestTransactionEventDropsTxAndReceipt documents what the encoding leaves
// out: subscribers of committed transactions read the copied fields.
func TestTransactionEventDropsTxAndReceipt(t *testing.T) {
	tx := types.NewTx(&types.LegacyTx{Nonce: 1, Gas: 21000, GasPrice: big.NewInt(1)})
	data, err := MarshalEvent(&TransactionEvent{Tx: tx, Receipt: &types.Receipt{}, Hash: tx.Hash()})
	require.NoError(t, err)
	got, err := UnmarshalEvent(EventTypeTransaction, data)
	require.NoError(t, err)
	te := got.(*TransactionEvent)
	require.Nil(t, te.Tx)
	require.Nil(t, te.Receipt)
	require.Equal(t, tx.Hash(), te.Hash)
}

func TestMarshalEventUnknownType(t *testing.T) {
	_, err := MarshalEvent(&unknownEvent{})
	require.ErrorContains(t, err, "no codec")
	_, err = UnmarshalEvent("no.such.type", []byte("{}"))
	require.ErrorContains(t, err, "no codec")
}

type unknownEvent struct{ Stream }

func (*unknownEvent) Type() EventType      { return "no.such.type" }
func (*unknownEvent) Timestamp() time.Time { return time.Time{} }

// TestStructCodecKeepsNumbers: numbers in interface{} values decode as
// json.Number, so large amounts encode again to the same digits.
func TestStructCodecKeepsNumbers(t *testing.T) {
	c := StructCodec[ContractLogEvent]()
	v, err := c.Encode(&ContractLogEvent{Data: map[string]interface{}{"amount": json.RawMessage("123456789012345678901234567890")}})
	require.NoError(t, err)
	data, err := json.Marshal(v)
	require.NoError(t, err)
	ev, err := c.Decode(data)
	require.NoError(t, err)
	again, err := json.Marshal(ev.(*ContractLogEvent).Data)
	require.NoError(t, err)
	require.JSONEq(t, `{"amount":123456789012345678901234567890}`, string(again))
	require.Contains(t, string(again), "123456789012345678901234567890")
}

// TestBusDropsDeliveredSequences: the bus delivers sequenced events once,
// so a relay that delivers again after a restart repeats nothing.
func TestBusDropsDeliveredSequences(t *testing.T) {
	bus := NewEventBus(16, 16)
	go bus.Run()
	defer bus.Stop()
	sub := bus.Subscribe("s", []EventType{EventTypeBlock}, nil, 16)
	for _, seq := range []uint64{1, 2, 1, 2, 3, 0, 0} {
		ev := &BlockEvent{Number: seq}
		ev.SetSequence(seq)
		require.True(t, bus.Publish(ev))
	}
	var got []uint64
	for len(got) < 5 {
		select {
		case ev := <-sub.Channel:
			got = append(got, ev.(*BlockEvent).Number)
		case <-time.After(5 * time.Second):
			t.Fatalf("got %v", got)
		}
	}
	require.Equal(t, []uint64{1, 2, 3, 0, 0}, got, "unsequenced events always pass")
	require.Equal(t, uint64(2), bus.Duplicates())
	require.Equal(t, uint64(3), bus.LastSequence())
}
