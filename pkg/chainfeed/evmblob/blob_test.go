package evmblob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"sync"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// Independent algebraic vectors: a zero polynomial commits to the identity;
// a constant-one polynomial commits to the standard BLS12-381 G1 generator.
// Both quotient polynomials are zero, so both proofs are the identity. No
// commitment/proof generation function from the verifier is used by the tests.
func vector(one bool) Entry {
	var v Entry
	v.Proof[0] = 0xc0
	if !one {
		v.Commitment[0] = 0xc0
		return v
	}
	for i := 31; i < len(v.Blob); i += 32 {
		v.Blob[i] = 1
	}
	g, _ := hex.DecodeString("97f1d3a73197d7942695638c4fa9ac0fc3688c4f9774b905a14e3a3f171bac586c55e83ff97a1aeffb3af00adb22c6bb")
	copy(v.Commitment[:], g)
	return v
}
func versioned(c kzg4844.Commitment) common.Hash { h := sha256.Sum256(c[:]); h[0] = 1; return h }

type fixture struct {
	block                       *model.Block
	id                          model.CaptureIdentity
	header, body, receipts, raw []byte
	response                    Response
}

func blockFixture(t *testing.T, entries []Entry) fixture {
	t.Helper()
	key, e := crypto.HexToECDSA("0000000000000000000000000000000000000000000000000000000000000001")
	require.NoError(t, e)
	to := common.Address{2}
	txs := []*types.Transaction{}
	receipts := types.Receipts{}
	if len(entries) > 0 {
		hashes := []common.Hash{}
		for _, v := range entries {
			hashes = append(hashes, versioned(v.Commitment))
		}
		tx, e := types.SignNewTx(key, types.LatestSignerForChainID(big.NewInt(1)), &types.BlobTx{ChainID: uint256.NewInt(1), GasTipCap: uint256.NewInt(1), GasFeeCap: uint256.NewInt(2), Gas: 50000, To: to, Value: uint256.NewInt(7), BlobFeeCap: uint256.NewInt(1), BlobHashes: hashes})
		require.NoError(t, e)
		txs = append(txs, tx)
		receipts = append(receipts, &types.Receipt{Type: 3, Status: 1, CumulativeGasUsed: 21000, Logs: []*types.Log{}})
	}
	gas := uint64(len(entries)) * 131072
	excess := uint64(0)
	beacon := common.Hash{3}
	header := &types.Header{ParentHash: common.Hash{1}, Root: common.Hash{2}, Number: big.NewInt(1), Difficulty: big.NewInt(0), GasLimit: 30_000_000, Time: 1, BaseFee: big.NewInt(1), BlobGasUsed: &gas, ExcessBlobGas: &excess, ParentBeaconRoot: &beacon}
	if len(txs) > 0 {
		header.GasUsed = 21000
	}
	b := types.NewBlock(header, &types.Body{Transactions: txs, Withdrawals: []*types.Withdrawal{}}, receipts, trie.NewStackTrie(nil))
	plan := evmdata.BasePlan()
	digest, e := plan.Digest()
	require.NoError(t, e)
	id := model.CaptureIdentity{Block: model.FeedBlock{Chain: model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}, Number: 1, Hash: b.Hash()}, Generation: 1, PlanVersion: 1, PlanDigest: digest}
	h, e := rlp.EncodeToBytes(b.Header())
	require.NoError(t, e)
	body, e := rlp.EncodeToBytes(b.Body())
	require.NoError(t, e)
	rs, e := rlp.EncodeToBytes(receipts)
	require.NoError(t, e)
	m, _, e := evmdata.Decode(evm.New("evm"), id.Block, h, body, rs)
	require.NoError(t, e)
	for i := range entries {
		entries[i].Index = uint64(i)
	}
	response := Response{Version: 1, Block: id.Block, Entries: entries}
	raw, e := rlp.EncodeToBytes(response)
	require.NoError(t, e)
	return fixture{block: m, id: id, header: h, body: body, receipts: rs, response: response, raw: raw}
}
func TestBlobIndependentVectorsAndPositions(t *testing.T) {
	f := blockFixture(t, []Entry{vector(false), vector(true), vector(false)})
	refs, e := References(f.block)
	require.NoError(t, e)
	require.Len(t, refs, 3)
	require.Equal(t, refs[0].VersionedHash, refs[2].VersionedHash)
	require.EqualValues(t, 2, refs[2].BlobIndex)
	v, e := Decode(f.raw)
	require.NoError(t, e)
	require.NoError(t, Verify(t.Context(), v, f.id.Block, refs))
	require.Equal(t, f.response, v)
}
func TestBlobRejectsIncompleteMalformedAndInvalidProof(t *testing.T) {
	f := blockFixture(t, []Entry{vector(false), vector(true)})
	refs, e := References(f.block)
	require.NoError(t, e)
	cases := map[string]func(*Response){
		"missing":         func(v *Response) { v.Entries = v.Entries[:1] },
		"extra":           func(v *Response) { v.Entries = append(v.Entries, vector(false)) },
		"reordered":       func(v *Response) { v.Entries[0], v.Entries[1] = v.Entries[1], v.Entries[0] },
		"duplicate-index": func(v *Response) { v.Entries[1].Index = 0 },
		"other-block":     func(v *Response) { v.Block.Hash[0] ^= 1 },
		"other-chain":     func(v *Response) { v.Block.Chain.ID = "2" },
		"cells-version":   func(v *Response) { v.Version = 2 },
		"blob-tampered":   func(v *Response) { v.Entries[0].Blob[31] = 1 },
		"noncanonical-field": func(v *Response) {
			for i := 0; i < 32; i++ {
				v.Entries[0].Blob[i] = 0xff
			}
		},
		"commitment-tampered": func(v *Response) { v.Entries[0].Commitment[1] = 1 },
		"invalid-point":       func(v *Response) { v.Entries[0].Proof = [48]byte{} },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			v, e := Decode(f.raw)
			require.NoError(t, e)
			edit(&v)
			require.Error(t, Verify(t.Context(), v, f.id.Block, refs))
		})
	}
	encoded := func(v any) []byte { raw, e := rlp.EncodeToBytes(v); require.NoError(t, e); return raw }
	bad := [][]byte{nil, {0xc0}, append(append([]byte{}, f.raw...), 0xc0), make([]byte, MaxResponseBytes+1)}
	tooMany := Response{Version: 1, Block: f.id.Block, Entries: make([]Entry, MaxBlobs+1)}
	bad = append(bad, encoded(tooMany))
	short := []any{uint64(0), make([]byte, 131071), make([]byte, 48), make([]byte, 48)}
	bad = append(bad, encoded([]any{uint64(1), f.id.Block, []any{short}}))
	for _, raw := range bad {
		_, e := Decode(raw)
		require.ErrorIs(t, e, ErrBlob)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, Verify(ctx, f.response, f.id.Block, refs), context.Canceled)
}
func TestBlobReferencesFailClosed(t *testing.T) {
	f := blockFixture(t, []Entry{vector(false)})
	old := *f.block.BlobGasUsed
	*f.block.BlobGasUsed = 0
	_, e := References(f.block)
	require.ErrorIs(t, e, ErrBlob)
	*f.block.BlobGasUsed = old
	f.block.Transactions[0].BlobHashes[0][0] = 2
	_, e = References(f.block)
	require.ErrorIs(t, e, ErrBlob)
	f.block.Transactions[0].BlobHashes[0][0] = 1
	f.block.Transactions[0].Opaque = true
	_, e = References(f.block)
	require.ErrorIs(t, e, ErrBlob)
}

func TestBlobCodecBoundaryAndJSON(t *testing.T) {
	f := blockFixture(t, nil)
	full := Response{Version: 1, Block: f.id.Block, Entries: make([]Entry, MaxBlobs)}
	for i := range full.Entries {
		full.Entries[i].Index = uint64(i)
	}
	raw, e := rlp.EncodeToBytes(full)
	require.NoError(t, e)
	require.LessOrEqual(t, len(raw), MaxResponseBytes)
	v, e := Decode(raw)
	require.NoError(t, e)
	require.Len(t, v.Entries, MaxBlobs)
	encoded, e := json.Marshal(vector(false))
	require.NoError(t, e)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	var blob string
	require.NoError(t, json.Unmarshal(fields["blob"], &blob))
	require.Len(t, blob, 2+2*131072)
	require.Equal(t, "0x", blob[:2])
	d, e := Descriptor("blob-v1", 1, Definition{Provider: "relay", Profile: "evm", Format: Format})
	require.NoError(t, e)
	_, e = DefinitionFor(d)
	require.NoError(t, e)
	d.Required = true
	d.ParentCheckpoint = true
	_, e = DefinitionFor(d)
	require.ErrorIs(t, e, ErrBlob)
	_, e = Descriptor("blob-v1", 1, Definition{Provider: "https://secret.example/key", Profile: "evm", Format: Format})
	require.ErrorIs(t, e, ErrBlob)
}
func TestBlobDecodeVerifyConcurrent(t *testing.T) {
	f := blockFixture(t, []Entry{vector(false)})
	refs, e := References(f.block)
	require.NoError(t, e)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := Decode(f.raw)
			if e == nil {
				e = Verify(t.Context(), v, f.id.Block, refs)
			}
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
}
