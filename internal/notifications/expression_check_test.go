package notifications

import (
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckExpressions (subscriptions design phase 5b): a dry run reports
// whether expressions would be registered and, over a sample log or
// transaction, whether it would be notified and with what payload; a
// sample that is not the filter event, an evaluation error and a
// malformed sample are told apart.
func TestCheckExpressions(t *testing.T) {
	svc, _ := scopedService(t, 0)
	svc.SetChainID(8283)
	transfer := "Transfer(address indexed from, address indexed to, uint256 value)"
	topic0 := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex()
	from, to := common.BytesToHash(common.HexToAddress("0x01").Bytes()).Hex(), common.BytesToHash(common.HexToAddress("0x02").Bytes()).Hex()
	value := "0x" + common.Bytes2Hex(common.LeftPadBytes([]byte{0x27, 0x10}, 32)) // 10000
	erc20 := &SampleLog{Address: "0x00000000000000000000000000000000000000aa", Topics: []string{topic0, from, to}, Data: value, Index: 2}

	check := func(in ExpressionCheck) *ExpressionCheckResult {
		t.Helper()
		out, err := svc.CheckExpressions(in)
		require.NoError(t, err)
		return out
	}

	out := check(ExpressionCheck{Event: transfer, Condition: `bigCmp(event.value, 15) >= 0`})
	assert.Equal(t, &ExpressionCheckResult{Valid: true}, out, "compiles; no sample, nothing evaluated")

	out = check(ExpressionCheck{Event: transfer, Condition: `event.value >= 15`})
	assert.False(t, out.Valid)
	assert.Contains(t, out.Error, "no matching overload")

	out = check(ExpressionCheck{Event: transfer, Condition: `bigCmp(event.value, 15) >= 0 && chain.id == 8283u`,
		Payload: `{"to": event.to, "value": event.value, "block": block.number}`, Log: erc20, Block: &SampleBlock{Number: 9, Time: 1700000000}})
	require.True(t, out.Valid)
	assert.Empty(t, out.Error)
	assert.True(t, out.Notify)
	assert.Equal(t, "10000", out.Decoded["value"])
	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Result, &result))
	assert.Equal(t, map[string]any{"to": "0x0000000000000000000000000000000000000002", "value": "10000", "block": float64(9)}, result)

	out = check(ExpressionCheck{Event: transfer, Condition: `bigCmp(event.value, 100000) >= 0`, Log: erc20})
	assert.True(t, out.Valid)
	assert.False(t, out.Notify, "10000 is below the condition")
	assert.Empty(t, out.Error)

	erc721 := *erc20
	erc721.Topics = append(append([]string{}, erc20.Topics...), from)
	erc721.Data = ""
	out = check(ExpressionCheck{Event: transfer, Condition: `true`, Log: &erc721})
	assert.False(t, out.Notify)
	assert.Contains(t, out.Error, "not the filter event")

	out = check(ExpressionCheck{Condition: `bigCmp(tx.value, "1000000000000000000") > 0 && tx.to == "0x0000000000000000000000000000000000000002"`,
		Transaction: &SampleTransaction{Hash: topic0, From: "0x0000000000000000000000000000000000000001", To: "0x0000000000000000000000000000000000000002", Value: "2000000000000000000"}})
	assert.True(t, out.Notify, "a transaction sample")

	out = check(ExpressionCheck{Condition: `100u / log.index > 0u`, Log: &SampleLog{Address: erc20.Address}})
	assert.True(t, out.Valid)
	assert.Contains(t, out.Error, "division by zero", "an evaluation error is reported, not returned")

	_, err := svc.CheckExpressions(ExpressionCheck{Condition: "true", Log: &SampleLog{Address: "0x12"}})
	require.Error(t, err)
	assert.True(t, IsSampleError(err))
	_, err = svc.CheckExpressions(ExpressionCheck{Log: erc20, Transaction: &SampleTransaction{}})
	assert.True(t, IsSampleError(err), "a log or a transaction, not both")
}
