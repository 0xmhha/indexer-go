package receipts_test

// P07 acceptance on the indexer framework (refactoring plan R6-3): the P07
// receipt indexer's requirements (nu-54v-dk-toy docs/content/products/p07/
// srs.md, P07-FR-01 to 06, NFR-02 and 03, design 4) checked against this
// example, which reproduces P07 with configuration (the declared receipts
// table) and the handlers of receipts.go.

import (
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/examples/receipts"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// settle appends a block where the settlement contract emits one
// PaymentSettled log, and returns the payment.
func settle(sc *testchain.ReceiptsScenario, merchant common.Address, order common.Hash, amount, nonce int64) testchain.Payment {
	word := func(v int64) []byte { return common.LeftPadBytes(big.NewInt(v).Bytes(), 32) }
	to := sc.Settlement
	sc.Chain.AddBlock(testchain.TxSpec{From: sc.Accounts[0], Tx: &types.LegacyTx{To: &to, Gas: 200000, GasPrice: big.NewInt(1_000_000_000)}, GasUsed: 90000,
		Logs: []*types.Log{{Address: sc.Settlement, Topics: []common.Hash{testchain.SigPaymentSettled,
			common.BytesToHash(merchant.Bytes()), order, common.BytesToHash(sc.Device.Bytes())}, Data: append(word(amount), word(nonce)...)}}})
	return testchain.Payment{Block: sc.Chain.Head(), Merchant: merchant, OrderID: order, Device: sc.Device, Amount: amount, Nonce: nonce}
}

// plainBlocks appends n blocks without settlement logs.
func plainBlocks(sc *testchain.ReceiptsScenario, n int) {
	to := sc.Accounts[2].Address
	for range n {
		sc.Chain.AddBlock(testchain.TxSpec{From: sc.Accounts[1], Tx: &types.LegacyTx{To: &to, Value: big.NewInt(1), Gas: 21000, GasPrice: big.NewInt(1_000_000_000)}})
	}
}

// receiptPath is the lookup of a payment's receipt.
func receiptPath(p testchain.Payment) string {
	return "/receipts/" + p.Merchant.Hex() + "/" + p.OrderID.Hex()
}

func TestP07Acceptance(t *testing.T) {
	// The topic0 the kiosk and the week-6 gate use.
	require.Equal(t, "0xeef4300dbd9217414481ce2ade0c4791c3b47c75b0c0d4b6bbe5fbb05260195f", testchain.SigPaymentSettled.Hex())
	require.Equal(t, crypto.Keccak256Hash([]byte("PaymentSettled(address,bytes32,address,uint256,uint256)")), testchain.SigPaymentSettled)

	sc := testchain.BuildReceipts()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := t.TempDir()
	port := freePort(t)
	cfg := filepath.Join(dir, "config.yaml")
	// The cursor starts before the first settlement block.
	require.NoError(t, os.WriteFile(cfg, []byte(config(sc, srv.URL(), dir, port, sc.Payments[0].Block)), 0o600))
	order1, order2, dupOfOrder1 := sc.Payments[0], sc.Payments[1], sc.Payments[3]
	require.Equal(t, order1.OrderID, dupOfOrder1.OrderID, "the scenario settles order 1 twice")
	lastBlock := uint64(sc.Chain.Len() - 1)

	// Before the payments are final only block 1 is visible.
	sc.Chain.SetHead(1)
	top := t // indexers live for the whole test, not one subtest
	ix := start(top, cfg, port)

	t.Run("FR-04 before the payment: 404", func(t *testing.T) {
		require.Eventually(t, func() bool { _, b := ix.get("/healthz"); return b["polled"] == true }, 30*time.Second, 10*time.Millisecond)
		code, body := ix.get(receiptPath(order1))
		assert.Equal(t, http.StatusNotFound, code)
		assert.Equal(t, map[string]any{"error": "NOT_INDEXED"}, body)
	})

	t.Run("FR-04 after the payment: the receipt", func(t *testing.T) {
		sc.Chain.SetHead(order2.Block)
		ix.waitCursor(t, order2.Block)
		code, body := ix.get(receiptPath(order1))
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, false, body["duplicate"], "settled once so far")
	})

	t.Run("FR-02 restart continues from the cursor", func(t *testing.T) {
		ix.stop()
		sc.Chain.SetHead(lastBlock)
		ix = start(top, cfg, port)
		ix.waitCursor(t, lastBlock)
		for i, p := range sc.Payments {
			code, body := ix.get(receiptPath(p))
			require.Equal(t, http.StatusOK, code, "payment %d", i)
			if p.OrderID != order1.OrderID || p.Merchant != order1.Merchant {
				assert.Equal(t, float64(p.Block), body["blockNumber"], "payment %d", i)
			}
		}
		code, body := ix.get("/merchants/" + order1.Merchant.Hex() + "/totals")
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, float64(3), body["receipts"], "every payment once: none missed or repeated across the restart")
	})

	t.Run("FR-05 an order settled twice: the earliest, marked duplicate", func(t *testing.T) {
		code, body := ix.get(receiptPath(order1))
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, float64(order1.Block), body["blockNumber"], "the earliest log")
		assert.Equal(t, true, body["duplicate"])
		code, body = ix.get(receiptPath(order2))
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, false, body["duplicate"])
	})

	t.Run("FR-06 the receipt's fields", func(t *testing.T) {
		code, body := ix.get(receiptPath(order2))
		require.Equal(t, http.StatusOK, code)
		require.Len(t, body, 8, "exactly the design's fields: %v", body)
		assert.Equal(t, order2.Merchant.Hex(), body["merchant"])
		assert.Equal(t, order2.OrderID.Hex(), body["orderId"])
		assert.Equal(t, order2.Device.Hex(), body["device"])
		assert.Equal(t, "1200", body["amount"])
		assert.Equal(t, float64(order2.Block), body["blockNumber"])
		assert.Equal(t, float64(sc.Chain.Block(order2.Block).Block.Time()), body["blockTime"])
		hash := sc.Chain.Block(order2.Block).Block.Transactions()[0].Hash().Hex()
		assert.Equal(t, receipts.Short(hash), body["txHashShort"])
		assert.Regexp(t, `^0x[0-9a-f]{6}…[0-9a-f]{4}$`, body["txHashShort"])
		assert.Equal(t, false, body["duplicate"])
	})

	t.Run("FR-01 only the settlement contract's PaymentSettled", func(t *testing.T) {
		// The other contract's PaymentSettled (order 9) and the settlement
		// contract's Refunded are not receipts.
		other := testchain.Payment{Merchant: sc.Merchants[0], OrderID: common.BigToHash(big.NewInt(1009))}
		code, _ := ix.get(receiptPath(other))
		assert.Equal(t, http.StatusNotFound, code)
		code, body := ix.get("/merchants/" + sc.Merchants[0].Hex() + "/totals")
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "6200", body["amount"], "the decoy of 1 is not counted")
	})

	t.Run("NFR-02 RPC errors keep the cursor; design 4 stale is 503", func(t *testing.T) {
		srv.DisableMethod("eth_getLogs")
		plainBlocks(sc, receipts.StaleBlocks+5)
		late := settle(sc, sc.Merchants[1], common.HexToHash("0x7a7e"), 300, 77)
		require.Eventually(t, func() bool {
			_, b := ix.get("/healthz")
			return b["lag"] != nil && b["lag"].(float64) > receipts.StaleBlocks
		}, 30*time.Second, 10*time.Millisecond, "the node's head moved ahead")
		_, health := ix.get("/healthz")
		assert.Equal(t, float64(lastBlock), health["cursor"], "the cursor did not move while logs could not be read")
		code, body := ix.get(receiptPath(late))
		assert.Equal(t, http.StatusServiceUnavailable, code)
		assert.Equal(t, map[string]any{"error": "RPC_STALE", "cursor": float64(lastBlock)}, body)

		srv.EnableMethod("eth_getLogs")
		ix.waitCursor(t, late.Block)
		code, body = ix.get(receiptPath(late))
		require.Equal(t, http.StatusOK, code, "the range is read again once the node answers")
		assert.Equal(t, "300", body["amount"])
		code, _ = ix.get("/receipts/" + sc.Merchants[1].Hex() + "/0x" + strings.Repeat("ee", 32))
		assert.Equal(t, http.StatusNotFound, code, "caught up again: missing is 404")
		lastBlock = late.Block
	})

	t.Run("NFR-03 no write API", func(t *testing.T) {
		resp, err := http.Post(ix.base+receiptPath(order1), "application/json", strings.NewReader("{}"))
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
		resp, err = http.Post(ix.base+"/rpc", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`))
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "no explorer JSON-RPC")
	})

	t.Run("FR-03 a log is stored once", func(t *testing.T) {
		// Restarting reads nothing twice; the framework identifies a record
		// by its log and skips indexed blocks (indexing the same range again
		// is checked in pkg/app TestRecordsFromDeclaredTables).
		ix.stop()
		ix = start(top, cfg, port)
		ix.waitCursor(t, lastBlock)
		code, body := ix.get("/merchants/" + sc.Merchants[1].Hex() + "/totals")
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, map[string]any{"receipts": float64(2), "amount": "1100"}, body, "800 and the late 300, once each")
		ix.stop()
	})

	t.Run("FR-01 storage holds no other chain data", func(t *testing.T) {
		entries, err := testchain.DumpKeyspace(filepath.Join(dir, "db"), nil)
		require.NoError(t, err)
		allowed := []string{"/data/blocks/", "/index/blockh/", "/index/time/", "/meta/", "/rec/", "/reckey/", "/undo/", "/outbox/", "/x/receipts/"}
		for _, e := range entries {
			ok := false
			for _, p := range allowed {
				ok = ok || strings.HasPrefix(string(e.Key), p)
			}
			require.True(t, ok, "stored %q", e.Key)
		}
	})
}
