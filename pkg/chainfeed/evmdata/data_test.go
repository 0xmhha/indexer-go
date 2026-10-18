package evmdata_test

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/0xmhha/indexer-go/pkg/window"
)

func TestBoundedConsensusLists(t *testing.T) {
	root, err := evmdata.Root([]byte{0xc0}, true)
	require.NoError(t, err)
	require.Equal(t, types.EmptyReceiptsHash, root)
	_, err = evmdata.Items([]byte{0xc0, 0x80}, 2)
	require.Error(t, err)
	_, err = evmdata.Items([]byte{0xc3, 0x80, 0x80, 0x80}, 2)
	require.Error(t, err)
	_, err = evmdata.Root([]byte{0xc1, 0x80}, true)
	require.Error(t, err)
	_, err = evmdata.Items(bytes.Repeat([]byte{0xff}, 65536), 16)
	require.Error(t, err)
}

func assemble(t *testing.T, p chains.BinaryProfile, header, body, receipts []byte) error {
	t.Helper()
	ch := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	id := model.FeedBlock{Chain: ch, Number: 1, Hash: crypto.Keccak256Hash(header)}
	v := window.VolumeSpec{Root: t.TempDir(), ID: "malformed-input", AllowDirectory: true}
	require.NoError(t, window.InitializeVolume(v))
	raw, err := window.Create(window.Options{Volume: v, Path: filepath.Join(v.Root, "raw"), Chain: ch, MaxBytes: 2 << 20, SegmentBytes: 256 << 10, MaxPayloadBytes: 128 << 10, MaxSegmentRecords: 8})
	require.NoError(t, err)
	defer func() { require.NoError(t, raw.Close()) }()
	var refs []window.Reference
	for i, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		r, err := raw.Append(context.Background(), chainfeed.Envelope{Block: id, Kind: k, Codec: evmdata.Codec(k), Payload: [][]byte{header, body, receipts}[i]})
		require.NoError(t, err)
		refs = append(refs, r)
	}
	_, err = (evmdata.Verifier{Profile: p, Provenance: "negative-fixture"}).Assemble(context.Background(), raw, capture.State{Generation: 1, Plan: evmdata.BasePlan(), Anchor: model.FeedBlock{Chain: ch, Hash: ch.GenesisHash}}, refs)
	return err
}

type guardedDecoder struct {
	chains.BinaryProfile
	called bool
}

func (p *guardedDecoder) DecodeBlockRLP(_, _ []byte) (*model.Block, error) {
	p.called = true
	return nil, errors.New("profile decoder must not be reached")
}

func TestWithdrawalLimitPrecedesProfileAllocation(t *testing.T) {
	withdrawals := make([]rlp.RawValue, evmdata.MaxItems+1)
	for i := range withdrawals {
		withdrawals[i] = rlp.RawValue{0xc0}
	}
	w, err := rlp.EncodeToBytes(withdrawals)
	require.NoError(t, err)
	body, err := rlp.EncodeToBytes([]rlp.RawValue{{0xc0}, {0xc0}, w})
	require.NoError(t, err)
	p := &guardedDecoder{}
	require.Error(t, assemble(t, p, []byte{0xc0}, body, []byte{0xc0}))
	require.False(t, p.called, "oversized nested lists are rejected before profile allocation")
}

func TestTypedTransactionRejectsLegacyReceipt(t *testing.T) {
	a, to := testchain.NewAccount(1), testchain.NewAccount(2).Address
	c := testchain.NewChain(1, map[common.Address]*big.Int{a.Address: big.NewInt(1_000_000_000_000_000_000)})
	b := c.AddBlock(testchain.TxSpec{From: a, Tx: &types.DynamicFeeTx{To: &to, Gas: 21000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(0)}, GasUsed: 21000})
	bad := *b.Receipts[0]
	bad.Type = types.LegacyTxType
	header := b.Block.Header()
	header.ReceiptHash = types.DeriveSha(types.Receipts{&bad}, trie.NewStackTrie(nil))
	h, err := rlp.EncodeToBytes(header)
	require.NoError(t, err)
	body, err := rlp.EncodeToBytes(b.Block.Body())
	require.NoError(t, err)
	receipts, err := rlp.EncodeToBytes(types.Receipts{&bad})
	require.NoError(t, err)
	err = assemble(t, evm.New("evm"), h, body, receipts)
	require.ErrorContains(t, err, "receipt type mismatch", "even matching commitments cannot change receipt type")
}
