package app

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/features/feedelegation"
	fdmeta "github.com/0xmhha/indexer-go/pkg/chains/stablenet/feedelegation"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// liveVectors are blocks and receipts recorded from a local go-stablenet
// network (chain 8283); block 33 holds a fee delegation transaction (0x16).
func liveVectors(t *testing.T) (blocks, receipts map[string]json.RawMessage) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "chains", "stablenet", "testdata", "live_vectors.json"))
	require.NoError(t, err)
	var v struct {
		Blocks   map[string]json.RawMessage `json:"blocks"`
		Receipts map[string]json.RawMessage `json:"receipts"`
	}
	require.NoError(t, json.Unmarshal(raw, &v))
	return v.Blocks, v.Receipts
}

// TestFeeDelegationQueries (refactoring plan R0-6, F2): a recorded StableNet
// block with a fee delegation transaction (0x16) answers the fee delegation
// queries with the transaction's fee payer, signature and the fee it paid,
// and the stablenet.fee_delegation feature stores the payer and signature.
// (The queries read the stored transactions, which keep the fee payer;
// the feature's metadata is read by no query.)
func TestFeeDelegationQueries(t *testing.T) {
	blocks, receipts := liveVectors(t)
	sc := testchain.BuildStableNet()
	for sc.Chain.Head() < 33 {
		sc.Chain.AddBlock()
	}
	sc.Chain.SetRawBlock(33, testchain.RawBlock{Block: blocks["33"], Receipts: receipts["33"]})
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	// The block's facts, from the recording.
	var block struct {
		Transactions []struct {
			Hash     common.Hash    `json:"hash"`
			Type     hexutil.Uint64 `json:"type"`
			FeePayer common.Address `json:"feePayer"`
			FV       hexutil.Big    `json:"fv"`
			FR       hexutil.Big    `json:"fr"`
			FS       hexutil.Big    `json:"fs"`
		} `json:"transactions"`
	}
	require.NoError(t, json.Unmarshal(blocks["33"], &block))
	var rs []struct {
		GasUsed           hexutil.Uint64 `json:"gasUsed"`
		EffectiveGasPrice hexutil.Big    `json:"effectiveGasPrice"`
	}
	require.NoError(t, json.Unmarshal(receipts["33"], &rs))
	fd := block.Transactions[1]
	require.Equal(t, hexutil.Uint64(0x16), fd.Type)
	fee := new(big.Int).Mul(new(big.Int).SetUint64(uint64(rs[1].GasUsed)), rs[1].EffectiveGasPrice.ToInt())

	// Only the fee delegation feature: the other features would ask the
	// test chain about the recorded block's accounts and contracts.
	profile, ok := chains.Lookup("stablenet")
	require.True(t, ok)
	features := map[string]bool{}
	for _, name := range append(feature.Defaults(), profile.Features()...) {
		features[name] = false
	}
	features[feedelegation.Name] = true
	app, err := startAppFeatures(t, srv, filepath.Join(t.TempDir(), "db"), features)
	require.NoError(t, err)
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 33, 33))

	h, err := graphql.NewHandler(app.storage, zap.NewNop())
	require.NoError(t, err)
	payer := fd.FeePayer.Hex()
	res := h.ExecuteQuery(`query($tx: String!, $payer: String!) {
		transaction(hash: $tx) { feePayer feePayerSignatures { v r s } }
		feeDelegationStats(fromBlock: "33", toBlock: "33") { totalFeeDelegatedTxs totalFeesSaved adoptionRate }
		topFeePayers(fromBlock: "33", toBlock: "33") { nodes { address txCount totalFeesPaid } }
		feePayerStats(address: $payer, fromBlock: "33", toBlock: "33") { address txCount totalFeesPaid percentage }
	}`, map[string]interface{}{"tx": fd.Hash.Hex(), "payer": payer})
	require.Empty(t, res.Errors)
	data := res.Data.(map[string]interface{})

	tx := data["transaction"].(map[string]interface{})
	assert.True(t, strings.EqualFold(payer, tx["feePayer"].(string)), "the recorded fee payer")
	sigs := tx["feePayerSignatures"].([]interface{})
	require.Len(t, sigs, 1)
	sig := sigs[0].(map[string]interface{})
	assert.Equal(t, fd.FR.ToInt().String(), mustBig(t, sig["r"].(string)).String())
	assert.Equal(t, fd.FS.ToInt().String(), mustBig(t, sig["s"].(string)).String())

	stats := data["feeDelegationStats"].(map[string]interface{})
	assert.Equal(t, "1", stats["totalFeeDelegatedTxs"])
	assert.Equal(t, fee.String(), stats["totalFeesSaved"])
	assert.InDelta(t, 50.0, stats["adoptionRate"], 0.001, "one of the block's two transactions")

	top := data["topFeePayers"].(map[string]interface{})["nodes"].([]interface{})
	require.Len(t, top, 1)
	assert.True(t, strings.EqualFold(payer, top[0].(map[string]interface{})["address"].(string)))
	assert.Equal(t, "1", top[0].(map[string]interface{})["txCount"])
	assert.Equal(t, fee.String(), top[0].(map[string]interface{})["totalFeesPaid"])

	ps := data["feePayerStats"].(map[string]interface{})
	assert.Equal(t, "1", ps["txCount"])
	assert.Equal(t, fee.String(), ps["totalFeesPaid"])
	assert.InDelta(t, 100.0, ps["percentage"], 0.001, "the only fee payer")

	// The feature's metadata.
	meta, err := fdmeta.OpenMetaStore(app.storage)
	require.NoError(t, err)
	m, err := meta.TxMeta(ctx, fd.Hash)
	require.NoError(t, err, "the feature stored the transaction's fee delegation")
	assert.Equal(t, fd.FeePayer, m.FeePayer)
	assert.Equal(t, uint8(0x16), m.OriginalType)
	assert.Equal(t, uint64(33), m.BlockNumber)
	assert.Equal(t, fd.FR.ToInt().String(), m.FeePayerR.String())
	assert.Equal(t, fd.FS.ToInt().String(), m.FeePayerS.String())
	assert.Equal(t, fd.FV.ToInt().String(), m.FeePayerV.String())
}

// mustBig reads a decimal or 0x hex integer.
func mustBig(t *testing.T, s string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s, 0)
	require.True(t, ok, s)
	return v
}
