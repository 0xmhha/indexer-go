package bundle_test

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
	"github.com/0xmhha/indexer-go/pkg/app/apptest"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/0xmhha/indexer-go/x/stablenet/consensus"
	"github.com/0xmhha/indexer-go/x/stablenet/features/feedelegation"
	"github.com/0xmhha/indexer-go/x/stablenet/features/systemcontracts"
	fdmeta "github.com/0xmhha/indexer-go/x/stablenet/feedelegation"
	sccontracts "github.com/0xmhha/indexer-go/x/stablenet/systemcontracts"

	_ "github.com/0xmhha/indexer-go/x/stablenet/bundle"
)

// The end-to-end tests of StableNet: the engine with the StableNet bundle
// indexes the test chain's StableNet scenario (pkg/app/apptest).

// TestWBFTSigningFromCanonicalSeals indexes the StableNet scenario, whose
// blocks carry the previous block's seals and epoch blocks every 4 blocks.
// Each block's signers must be the validators set in the Prev seals of the
// next block, against the validator set of the block's epoch, with the
// round recorded there; epochs must be numbered in order from genesis.
func TestWBFTSigningFromCanonicalSeals(t *testing.T) {
	sc := testchain.BuildStableNet()
	a := apptest.Index(t, sc.Chain, systemcontracts.Name)
	r := consensus.NewStore(a.Storage().(consensus.Backend), nil)
	cfg := sc.Chain.StableNetConfig()
	ctx := context.Background()

	head := sc.Chain.Head()
	for n := uint64(1); n < head; n++ {
		preparers, committers, err := r.GetBlockSigners(ctx, n)
		require.NoError(t, err)
		var want []common.Address
		for _, i := range cfg.Committers(n) {
			want = append(want, cfg.Candidates[i].Address)
		}
		require.ElementsMatch(t, want, committers, "commit signers of block %d", n)
		require.Len(t, preparers, len(cfg.Candidates), "prepare signers of block %d", n)

		for _, c := range cfg.Candidates {
			acts, err := r.GetValidatorSigningActivity(ctx, c.Address, n, n, 10, 0)
			require.NoError(t, err)
			require.Len(t, acts, 1, "activity of %s at %d", c.Address.Hex(), n)
			require.Equal(t, cfg.PrevRound(n), acts[0].Round, "round of block %d", n)
		}
	}
	_, committers, err := r.GetBlockSigners(ctx, head)
	require.NoError(t, err)
	require.Empty(t, committers, "the head's seals arrive with the next block")

	epochs, total, err := r.GetEpochsList(ctx, 100, 0)
	require.NoError(t, err)
	require.Equal(t, int(head/cfg.EpochLength)+1, total)
	for _, e := range epochs {
		require.Equal(t, e.BlockNumber/cfg.EpochLength, e.EpochNumber, "epoch at block %d", e.BlockNumber)
		require.Zero(t, e.BlockNumber%cfg.EpochLength)
	}
}

// TestSystemContractEventsArePublished indexes the StableNet scenario and
// requires its governance events on the event bus, where the
// systemContractEvents subscription reads them.
func TestSystemContractEventsArePublished(t *testing.T) {
	sc := testchain.BuildStableNet()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	cfg := apptest.Config(t, srv.URL())
	apptest.Enable(cfg, systemcontracts.Name)
	a := apptest.Start(t, cfg)

	sub := a.EventBus().Subscribe("system-contracts", []events.EventType{sccontracts.EventTypeSystemContract}, nil, 1000)
	apptest.FetchAll(t, a, 0, sc.Chain.Head())

	seen := map[sccontracts.SystemContractEventType]bool{}
	require.Eventually(t, func() bool {
		for {
			select {
			case ev := <-sub.Channel:
				if e, ok := ev.(*sccontracts.SystemContractEvent); ok {
					seen[e.EventName] = true
				}
			default:
				return seen[sccontracts.SystemContractEventProposalCreated] &&
					seen[sccontracts.SystemContractEventDepositMintProposed]
			}
		}
	}, 5*time.Second, 10*time.Millisecond, "published: %v", seen)
}

// liveVectors are blocks and receipts recorded from a local go-stablenet
// network (chain 8283); block 33 holds a fee delegation transaction (0x16).
func liveVectors(t *testing.T) (blocks, receipts map[string]json.RawMessage) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "live_vectors.json"))
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
	cfg := apptest.Config(t, srv.URL())
	apptest.SetFeatures(cfg, features)
	a := apptest.Start(t, cfg)
	apptest.FetchAll(t, a, 33, 33)
	ctx := context.Background()

	h, err := graphql.NewHandler(a.Storage(), zap.NewNop())
	require.NoError(t, err)
	payer := fd.FeePayer.Hex()
	res := h.ExecuteQuery(`query($tx: String!, $payer: String!) {
		transaction(hash: $tx) { feePayer feePayerSignatures { v r s } }
		feeDelegationStats(fromBlock: "33", toBlock: "33") { totalFeeDelegatedTxs totalFeesSaved adoptionRate }
		topFeePayers(fromBlock: "33", toBlock: "33") { nodes { address txCount totalFeesPaid } }
		feePayerStats(address: $payer, fromBlock: "33", toBlock: "33") { address txCount totalFeesPaid percentage }
		feePayerTransactions(feePayer: $payer) { nodes { transactionHash blockNumber } pageInfo { hasNextPage } }
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

	// The fee payer's index the feature writes, served by feePayerTransactions.
	paid := data["feePayerTransactions"].(map[string]interface{})
	assert.Equal(t, []interface{}{map[string]interface{}{"transactionHash": fd.Hash.Hex(), "blockNumber": "33"}}, paid["nodes"])
	assert.Equal(t, false, paid["pageInfo"].(map[string]interface{})["hasNextPage"])

	// The feature's metadata.
	meta, err := fdmeta.OpenMetaStore(a.Storage())
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
