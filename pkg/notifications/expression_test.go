package notifications

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/events"
)

const swapSig = "Swap(address indexed sender, uint256 amount, uint24 fee, bool exactIn)"

// swapLog is a Swap log of the pool 0xaa in block 7.
func swapLog(sender common.Address, amount *big.Int, fee uint64, exactIn bool) *types.Log {
	word := func(b *big.Int) []byte { return common.LeftPadBytes(b.Bytes(), 32) }
	in := big.NewInt(0)
	if exactIn {
		in = big.NewInt(1)
	}
	data := append(append(word(amount), word(new(big.Int).SetUint64(fee))...), word(in)...)
	return &types.Log{Address: common.HexToAddress("0xaa"), BlockNumber: 7, BlockHash: common.HexToHash("0x07"), Index: 1,
		TxHash: common.HexToHash("0x70"), Topics: []common.Hash{crypto.Keccak256Hash([]byte("Swap(address,uint256,uint24,bool)")),
			common.BytesToHash(sender.Bytes())}, Data: data}
}

// exprSetting is a log setting of the Swap event with expressions.
func exprSetting(condition, payload string) *NotificationSetting {
	return &NotificationSetting{Name: "x", Type: NotificationTypeStream, Enabled: true, Owner: "alice", Delivery: DeliveryFast,
		EventTypes: []EventType{EventTypeLog}, Filter: &NotifyFilter{Event: swapSig}, Condition: condition, Payload: payload}
}

// TestExpressionsAreCheckedWhenRegistered: syntax errors, unknown
// variables and arguments, a condition that is not a bool, an argument of
// the wrong type, an over-long expression and one whose estimated cost
// exceeds the limit are refused when the setting is created.
func TestExpressionsAreCheckedWhenRegistered(t *testing.T) {
	ctx := context.Background()
	svc, _ := scopedService(t, 0)
	for name, c := range map[string]struct{ condition, payload, want string }{
		"syntax":            {condition: "event.fee ==", want: "condition"},
		"unknown argument":  {condition: "event.fees == 3000u", want: "undeclared reference"},
		"unknown variable":  {condition: "tx.gas > 0u", want: "undeclared reference"},
		"not a bool":        {condition: "event.fee", want: "must be a bool"},
		"wrong type":        {condition: "event.amount > 1000", want: "no matching overload"},
		"payload unknown":   {payload: `{"x": event.nope}`, want: "payload"},
		"too long":          {condition: strings.Repeat("true && ", 300) + "true", want: "longer than"},
		"cost over limit":   {condition: `[1,2,3,4,5,6,7,8,9,10].all(a, [1,2,3,4,5,6,7,8,9,10].all(b, [1,2,3,4,5,6,7,8,9,10].all(c, log.address.matches(tx.from + log.txHash))))`, want: "over the limit"},
		"no event, no args": {condition: "event.fee == 3000u", want: "undeclared reference"},
	} {
		t.Run(name, func(t *testing.T) {
			st := exprSetting(c.condition, c.payload)
			if name == "no event, no args" {
				st.Filter = nil
			}
			_, err := svc.CreateSetting(ctx, st)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
	_, err := svc.CreateSetting(ctx, exprSetting(`bigCmp(event.amount, "1000000000000000000000") > 0 && event.fee == 3000u && event.exactIn`,
		`{"sender": event.sender, "amount": event.amount, "block": block.number, "chain": chain.id}`))
	require.NoError(t, err)
}

// TestExpressionsSelectAndShape: the condition selects the events
// (uint256 compared with bigCmp, small integers and bools typed), and the
// payload's value is sent as payload.result, with the chain id.
func TestExpressionsSelectAndShape(t *testing.T) {
	ctx := context.Background()
	svc, _ := scopedService(t, 0)
	svc.SetChainID(8283)
	st, err := svc.CreateSetting(ctx, exprSetting(`bigCmp(event.amount, "1000000000000000000000") > 0 && event.fee == 3000u && event.exactIn && chain.id == 8283u`,
		`{"sender": event.sender, "amount": event.amount, "block": block.number, "pool": log.address}`))
	require.NoError(t, err)
	c, err := svc.Streams().Open("alice")
	require.NoError(t, err)

	alice := common.HexToAddress("0x00000000000000000000000000000000000A11CE")
	large := new(big.Int).Exp(big.NewInt(10), big.NewInt(22), nil) // 1e22, over 64 bits
	block := []events.Event{
		events.NewLogEvent(swapLog(alice, large, 3000, true)),         // notified
		events.NewLogEvent(swapLog(alice, big.NewInt(5), 3000, true)), // amount too small
		events.NewLogEvent(swapLog(alice, large, 500, true)),          // other fee
		events.NewLogEvent(swapLog(alice, large, 3000, false)),        // not exact in
	}
	for i, ev := range block {
		ev.(*events.LogEvent).Log.Index = uint(i)
	}
	require.NoError(t, svc.notifyFast(ctx, block))

	m := decodeStream(t, c)
	require.Equal(t, StreamNotification, m.Type)
	assert.Equal(t, st.ID, m.Notification.SettingID)
	assert.Equal(t, uint64(8283), m.Notification.Payload.ChainID)
	var result map[string]any
	require.NoError(t, json.Unmarshal(m.Notification.Payload.Result, &result))
	assert.Equal(t, map[string]any{"sender": strings.ToLower(alice.Hex()), "amount": "10000000000000000000000", "block": float64(7),
		"pool": "0x00000000000000000000000000000000000000aa"}, result)
	assert.Empty(t, c.Messages(), "the other three events do not meet the condition")
}

// TestExpressionErrorsDisableTheSetting: an expression that fails at run
// time notifies nothing; each failure is sent to the owner's stream, and
// the tenth in a row disables the setting.
func TestExpressionErrorsDisableTheSetting(t *testing.T) {
	ctx := context.Background()
	svc, storage := scopedService(t, 0)
	// tx.value is empty on a log event: not an integer.
	st, err := svc.CreateSetting(ctx, exprSetting(`bigCmp(tx.value, 1) > 0`, ""))
	require.NoError(t, err)
	c, err := svc.Streams().Open("alice")
	require.NoError(t, err)

	for i := 0; i < maxExpressionErrors; i++ {
		log := swapLog(common.HexToAddress("0x01"), big.NewInt(1), 1, true)
		log.Index = uint(i)
		require.NoError(t, svc.notifyFast(ctx, []events.Event{events.NewLogEvent(log)}), "an expression error stops nothing")
		m := decodeStream(t, c)
		require.Equal(t, StreamError, m.Type)
		assert.Equal(t, st.ID, m.SettingID)
		assert.Equal(t, uint64(7), m.Block)
		assert.Contains(t, m.Error, "not an integer")
		assert.Equal(t, i == maxExpressionErrors-1, m.Disabled, "error %d", i+1)
	}
	stored, err := storage.GetSetting(ctx, st.ID)
	require.NoError(t, err)
	assert.False(t, stored.Enabled, "disabled after %d errors in a row", maxExpressionErrors)
	assert.False(t, svc.Active(), "no fast setting left")
	all, err := storage.ListNotifications(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, all)
}

// TestExpressionErrorsMustBeConsecutive: a success resets the count.
func TestExpressionErrorsMustBeConsecutive(t *testing.T) {
	ctx := context.Background()
	svc, storage := scopedService(t, 0)
	// Fails when the fee is 0 (division by zero).
	st, err := svc.CreateSetting(ctx, exprSetting(`100u / event.fee > 0u`, ""))
	require.NoError(t, err)
	for i := 0; i < 3*maxExpressionErrors; i++ {
		fee := uint64(0)
		if i%(maxExpressionErrors-1) == 0 {
			fee = 1 // a success every nine events
		}
		log := swapLog(common.HexToAddress("0x01"), big.NewInt(1), fee, true)
		log.Index = uint(i)
		require.NoError(t, svc.notifyFast(ctx, []events.Event{events.NewLogEvent(log)}))
	}
	stored, err := storage.GetSetting(ctx, st.ID)
	require.NoError(t, err)
	assert.True(t, stored.Enabled, "never ten errors in a row")
}

// BenchmarkExpressionEvaluate is one event through a setting's condition
// and payload: variables, decoded argument conversion and evaluation.
func BenchmarkExpressionEvaluate(b *testing.B) {
	st := exprSetting(`bigCmp(event.amount, "1000000000000000000000") > 0 && event.fee == 3000u && event.exactIn`,
		`{"sender": event.sender, "amount": event.amount, "block": block.number}`)
	x, err := compileExpressions(st)
	require.NoError(b, err)
	large := new(big.Int).Exp(big.NewInt(10), big.NewInt(22), nil)
	ev := events.NewLogEvent(swapLog(common.HexToAddress("0x01"), large, 3000, true))
	decoded := decodeFilterEvent(st.Filter, ev.Log)
	b.ReportAllocs()
	for b.Loop() {
		if ok, _, err := x.evaluate(x.activation(7777, ev, decoded)); err != nil || !ok {
			b.Fatal(ok, err)
		}
	}
}
