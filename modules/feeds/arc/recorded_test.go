package arcfeed_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains/arc"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/window"
)

type recordedCanonical map[uint64]common.Hash

func (r recordedCanonical) HashAt(_ context.Context, n uint64) (common.Hash, error) {
	if h, ok := r[n]; ok {
		return h, nil
	}
	return common.Hash{}, fmt.Errorf("recorded: missing height %d", n)
}

// Frozen public Arc execution data tests the real optional header fields and
// consensus roots. It is not a live peer, execution or consensus-finality test.
func TestRecordedArcExecutionInputs(t *testing.T) {
	var fixture struct {
		ChainID      string      `json:"chain_id"`
		GenesisHash  common.Hash `json:"genesis_hash"`
		Height       uint64      `json:"height"`
		Hash         common.Hash `json:"hash"`
		ParentHash   common.Hash `json:"parent_hash"`
		Transactions int         `json:"transactions"`
		Header       []byte      `json:"header"`
		Body         []byte      `json:"body"`
		Receipts     []byte      `json:"receipts"`
	}
	data, err := os.ReadFile("testdata/arc-67019625.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &fixture))
	chain := model.FeedChain{ID: fixture.ChainID, GenesisHash: fixture.GenesisHash}
	id := model.FeedBlock{Chain: chain, Number: fixture.Height, Hash: fixture.Hash}
	v := window.VolumeSpec{Root: t.TempDir(), ID: "recorded-arc", AllowDirectory: true}
	require.NoError(t, window.InitializeVolume(v))
	raw, err := window.Create(window.Options{Volume: v, Path: filepath.Join(v.Root, "raw"), Chain: chain, MaxBytes: 1 << 20, SegmentBytes: 64 << 10, MaxPayloadBytes: 32 << 10, MaxSegmentRecords: 8})
	require.NoError(t, err)
	defer func() { require.NoError(t, raw.Close()) }()
	var refs []window.Reference
	values := [][]byte{fixture.Header, fixture.Body, fixture.Receipts}
	for i, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		r, err := raw.Append(context.Background(), chainfeed.Envelope{Block: id, Kind: k, Codec: evmdata.Codec(k), Payload: values[i]})
		require.NoError(t, err)
		refs = append(refs, r)
	}
	state := capture.State{Generation: 1, Plan: evmdata.BasePlan(), Anchor: model.FeedBlock{Chain: chain, Number: fixture.Height - 1, Hash: fixture.ParentHash}}
	verifier := evmdata.Verifier{Profile: arc.New(), Provenance: "recorded-arc-20261014"}
	in, err := verifier.Assemble(context.Background(), raw, state, refs)
	require.NoError(t, err)
	require.Equal(t, id, in.Manifest.Identity.Block)
	require.Equal(t, fixture.ParentHash, in.ParentHash)
	require.NoError(t, state.Plan.ValidateComplete(in.Manifest))
	require.Len(t, in.Payloads, 3)
	for i, p := range in.Payloads {
		require.Equal(t, values[i], p.Bytes)
	}
	b, err := arc.New().DecodeBlockRLP(fixture.Header, fixture.Body)
	require.NoError(t, err)
	require.Len(t, b.Transactions, fixture.Transactions)
	rs, err := arc.New().DeriveReceipts(b, fixture.Receipts)
	require.NoError(t, err)
	require.Len(t, rs, fixture.Transactions)
	// Persist the normalized source bundle, then query individual records
	// without node RPC or the raw window. Model codecs preserve extensions.
	store := db(t)
	fresh := recordedCanonical{0: chain.GenesisHash, state.Anchor.Number: state.Anchor.Hash, id.Number: id.Hash}
	facts, err := capture.New(store, chain, fresh)
	require.NoError(t, err)
	require.NoError(t, facts.Initialize(context.Background(), state.Plan, state.Anchor, nil))
	require.NoError(t, facts.Commit(context.Background(), in))
	require.NoError(t, raw.Close())
	page, err := facts.Rows(context.Background(), in.Manifest.Identity, capture.RowHeader, 0, 256)
	require.NoError(t, err)
	require.Len(t, page.Rows, 1)
	header, err := model.DecodeBlock(page.Rows[0].Value)
	require.NoError(t, err)
	expectedHeader := *b
	expectedHeader.Transactions, expectedHeader.Uncles, expectedHeader.Withdrawals = nil, nil, nil
	wantHeader, err := model.EncodeBlock(&expectedHeader)
	require.NoError(t, err)
	require.Equal(t, wantHeader, page.Rows[0].Value)
	require.Equal(t, b.Hash, header.Hash)
	require.Equal(t, b.StateRoot, header.StateRoot)
	page, err = facts.Rows(context.Background(), in.Manifest.Identity, capture.RowTransaction, 0, 256)
	require.NoError(t, err)
	require.Len(t, page.Rows, fixture.Transactions)
	for i, row := range page.Rows {
		tx, e := model.DecodeTransaction(row.Value)
		require.NoError(t, e)
		want, e := model.EncodeTransaction(b.Transactions[i])
		require.NoError(t, e)
		require.Equal(t, want, row.Value)
		require.Equal(t, b.Transactions[i].Hash, tx.Hash)
	}
	page, err = facts.Rows(context.Background(), in.Manifest.Identity, capture.RowReceipt, 0, 256)
	require.NoError(t, err)
	require.Len(t, page.Rows, fixture.Transactions)
	logs := 0
	for i, row := range page.Rows {
		receipt, e := model.DecodeReceipt(row.Value)
		require.NoError(t, e)
		expected := *rs[i]
		logs += len(expected.Logs)
		expected.Logs = nil
		want, e := model.EncodeReceipt(&expected)
		require.NoError(t, e)
		require.Equal(t, want, row.Value)
		require.Equal(t, expected.TxHash, receipt.TxHash)
	}
	page, err = facts.Rows(context.Background(), in.Manifest.Identity, capture.RowLog, 0, 256)
	require.NoError(t, err)
	require.Len(t, page.Rows, logs)
	i := 0
	for _, r := range rs {
		for _, want := range r.Logs {
			var got model.Log
			require.NoError(t, json.Unmarshal(page.Rows[i].Value, &got))
			require.Equal(t, want, &got)
			i++
		}
	}
	page, err = facts.Rows(context.Background(), in.Manifest.Identity, capture.RowBody, 0, 256)
	require.NoError(t, err)
	require.Len(t, page.Rows, 1)
	var body capture.BodyFacts
	require.NoError(t, json.Unmarshal(page.Rows[0].Value, &body))
	require.Len(t, body.TransactionHashes, fixture.Transactions)
	for i, h := range body.TransactionHashes {
		require.Equal(t, b.Transactions[i].Hash, h)
	}
	require.Zero(t, raw.Stats().ReadReservedBytes)
}
