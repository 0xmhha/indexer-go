package evmtrace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sync"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

type traceFixture struct {
	chain                         *testchain.Chain
	block                         *model.Block
	receipts                      []*model.Receipt
	raw                           []byte
	header, body, encodedReceipts []byte
}

// The checked-in output of Geth's pinned native tracer is independent of this
// decoder. Signed transaction/receipt commitments are constructed separately;
// no tracing dependency, full node or EVM execution is added to ordinary tests.
func nativeFixture(t *testing.T) traceFixture {
	t.Helper()
	a := testchain.NewAccount(1)
	to := testchain.NewAccount(2).Address
	c := testchain.NewChain(1, map[common.Address]*big.Int{a.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)})
	specs := []testchain.TxSpec{}
	for i := 0; i < 4; i++ {
		var target *common.Address
		if i%2 == 0 {
			target = &to
		}
		specs = append(specs, testchain.TxSpec{From: a, Tx: &types.DynamicFeeTx{Nonce: uint64(i), To: target, Gas: 100000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(int64(i + 7)), Data: []byte{byte(i + 1)}}, GasUsed: uint64(50000 + i*10000), Failed: i >= 2})
	}
	b := c.AddBlock(specs...)
	raw, e := os.ReadFile("testdata/geth-v1.16.5-calltrace.json")
	require.NoError(t, e)
	header, e := rlp.EncodeToBytes(b.Block.Header())
	require.NoError(t, e)
	body, e := rlp.EncodeToBytes(b.Block.Body())
	require.NoError(t, e)
	rs, e := rlp.EncodeToBytes(b.Receipts)
	require.NoError(t, e)
	id := model.FeedBlock{Chain: model.FeedChain{ID: "1", GenesisHash: c.Block(0).Block.Hash()}, Number: 1, Hash: b.Block.Hash()}
	modelBlock, receipts, e := evmdata.Decode(evm.New("evm"), id, header, body, rs)
	require.NoError(t, e)
	return traceFixture{chain: c, block: modelBlock, receipts: receipts, raw: raw, header: header, body: body, encodedReceipts: rs}
}
func TestCallTraceNativeFormatAndRevertedPaths(t *testing.T) {
	f := nativeFixture(t)
	frames, e := Decode(f.raw, f.block, f.receipts)
	require.NoError(t, e)
	require.Len(t, frames, 10)
	require.Empty(t, frames[0].Path)
	require.False(t, frames[0].Reverted)
	require.Equal(t, []uint32{0}, frames[1].Path)
	require.True(t, frames[1].Reverted)
	require.False(t, frames[1].AncestorReverted)
	require.Equal(t, []uint32{0, 0}, frames[2].Path)
	require.True(t, frames[2].Reverted)
	require.True(t, frames[2].AncestorReverted)
	require.Empty(t, frames[2].Error)
	require.Equal(t, "DELEGATECALL", frames[3].Type)
	require.Equal(t, "0x7", frames[3].Value.String())
	require.Nil(t, frames[4].Value)
	require.Equal(t, "SELFDESTRUCT", frames[5].Type)
	require.False(t, frames[5].Reverted)
	require.Equal(t, "CREATE", frames[6].Type)
	require.Equal(t, *f.receipts[1].ContractAddress, *frames[6].To)
	require.True(t, frames[8].AncestorReverted)
	require.True(t, frames[8].Reverted)
	require.Equal(t, "CREATE", frames[9].Type)
	require.Nil(t, frames[9].To)
	require.True(t, frames[9].Reverted)
}
func TestCallTraceRejectsMismatchAndMalformed(t *testing.T) {
	f := nativeFixture(t)
	cases := map[string]func([]map[string]any) []map[string]any{
		"missing-tx":       func(v []map[string]any) []map[string]any { return v[:3] },
		"wrong-order":      func(v []map[string]any) []map[string]any { v[0], v[1] = v[1], v[0]; return v },
		"missing-root":     func(v []map[string]any) []map[string]any { v[0]["result"] = nil; return v },
		"rpc-member-error": func(v []map[string]any) []map[string]any { v[0]["error"] = "trace unavailable"; return v },
	}
	for _, field := range []string{"from", "to", "value", "gas", "gasUsed", "input", "type", "error"} {
		field := field
		cases["root-"+field] = func(v []map[string]any) []map[string]any {
			root := v[0]["result"].(map[string]any)
			switch field {
			case "from", "to":
				root[field] = common.Address{99}.Hex()
			case "type":
				root[field] = "STATICCALL"
			case "error":
				root[field] = "execution reverted"
			default:
				root[field] = "0x0"
			}
			return v
		}
	}
	cases["unknown-opcode"] = func(v []map[string]any) []map[string]any {
		v[0]["result"].(map[string]any)["calls"].([]any)[0].(map[string]any)["type"] = "AUTHCALL"
		return v
	}
	cases["unknown-field"] = func(v []map[string]any) []map[string]any { v[0]["result"].(map[string]any)["logs"] = []any{}; return v }
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			var v []map[string]any
			require.NoError(t, json.Unmarshal(f.raw, &v))
			raw, e := json.Marshal(change(v))
			require.NoError(t, e)
			_, e = Decode(raw, f.block, f.receipts)
			require.ErrorIs(t, e, ErrTrace)
		})
	}
	for _, raw := range [][]byte{[]byte("null"), []byte(`[] {}`), []byte(`[{"txHash":"a","txHash":"b"}]`), bytes.Repeat([]byte(" "), MaxResponseBytes+1), []byte(`[{"result":{"type":"CALL","type":"STATICCALL"}}]`)} {
		_, e := Decode(raw, f.block, f.receipts)
		require.ErrorIs(t, e, ErrTrace)
	}
	var v []map[string]any
	require.NoError(t, json.Unmarshal(f.raw, &v))
	v[0]["result"].(map[string]any)["input"] = "0x" + fmt.Sprintf("%x", bytes.Repeat([]byte{1}, MaxFieldBytes+1))
	raw, e := json.Marshal(v)
	require.NoError(t, e)
	_, e = Decode(raw, f.block, f.receipts)
	require.ErrorIs(t, e, ErrTrace)
	deep := `{"type":"CALL","calls":` + `[]` + `}`
	for i := 0; i < MaxDepth+2; i++ {
		deep = `{"type":"CALL","calls":[` + deep + `]}`
	}
	require.ErrorIs(t, guardJSON([]byte(`[{"result":`+deep+`}]`)), ErrTrace)
}
func TestCallTraceDecodeConcurrent(t *testing.T) {
	f := nativeFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := Decode(f.raw, f.block, f.receipts); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
}
