package evm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// rpcResult calls a JSON-RPC method and returns its raw result.
func rpcResult(t *testing.T, url, method string, params ...any) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	require.NoError(t, err)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Empty(t, out.Error, "%s", method)
	return out.Result
}

// TestBinaryMatchesJSON decodes every block of the reference scenario both
// from the node's JSON and from its consensus encoding and requires equal
// models, including effective gas prices derived with the default (London)
// rule.
func TestBinaryMatchesJSON(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	p := New("test")
	sawCreate := false
	for n := 0; n < sc.Chain.Len(); n++ {
		tag := fmt.Sprintf("%#x", n)
		wantBlock, err := p.DecodeBlock(rpcResult(t, srv.URL(), "eth_getBlockByNumber", tag, true))
		require.NoError(t, err)
		wantReceipts, err := p.DecodeReceipts(rpcResult(t, srv.URL(), "eth_getBlockReceipts", tag))
		require.NoError(t, err)

		blk := sc.Chain.Block(uint64(n))
		header, err := rlp.EncodeToBytes(blk.Block.Header())
		require.NoError(t, err)
		body, err := rlp.EncodeToBytes(blk.Block.Body())
		require.NoError(t, err)
		receipts, err := rlp.EncodeToBytes(blk.Receipts)
		require.NoError(t, err)

		gotBlock, err := p.DecodeBlockRLP(header, body)
		require.NoError(t, err)
		require.Equal(t, wantBlock, gotBlock, "block %d", n)
		gotReceipts, err := p.DeriveReceipts(gotBlock, receipts)
		require.NoError(t, err)
		require.Equal(t, wantReceipts, gotReceipts, "receipts of block %d", n)
		for _, r := range gotReceipts {
			sawCreate = sawCreate || r.ContractAddress != nil
		}
	}
	require.True(t, sawCreate, "scenario should cover contract creation")
}

func TestDeriveReceiptsRejectsCountMismatch(t *testing.T) {
	p := New("test")
	b := &model.Block{Number: 1, Transactions: []*model.Transaction{{}}}
	_, err := p.DeriveReceipts(b, []byte{0xc0})
	require.Error(t, err)
}

func TestLondonEffectiveGasPrice(t *testing.T) {
	tx := &model.Transaction{GasTipCap: big.NewInt(2), GasFeeCap: big.NewInt(10)}
	require.Equal(t, big.NewInt(10), LondonEffectiveGasPrice(&model.Block{}, tx, nil))
	require.Equal(t, big.NewInt(7), LondonEffectiveGasPrice(&model.Block{BaseFee: big.NewInt(5)}, tx, nil))
	require.Equal(t, big.NewInt(10), LondonEffectiveGasPrice(&model.Block{BaseFee: big.NewInt(9)}, tx, nil))
}

// TestDeriveReceiptsBlobTx derives a blob transaction's receipt: blob gas
// used follows from the transaction, the blob gas price is unsupported (nil)
// because it depends on the chain's blob schedule.
func TestDeriveReceiptsBlobTx(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	chainID := big.NewInt(7777)
	to := common.Address{9}
	tx := types.MustSignNewTx(key, types.LatestSignerForChainID(chainID), &types.BlobTx{
		ChainID: uint256.MustFromBig(chainID), Nonce: 0, GasTipCap: uint256.NewInt(2), GasFeeCap: uint256.NewInt(100),
		Gas: 21000, To: to, BlobFeeCap: uint256.NewInt(1), BlobHashes: []common.Hash{{1}, {2}},
	})
	excess, used := uint64(0), uint64(2*params.BlobTxBlobGasPerBlob)
	header := &types.Header{
		Number: big.NewInt(1), Difficulty: big.NewInt(0), GasLimit: 30_000_000, GasUsed: 21000,
		BaseFee: big.NewInt(10), ExcessBlobGas: &excess, BlobGasUsed: &used, ParentBeaconRoot: &common.Hash{},
	}
	receipts := types.Receipts{{Type: types.BlobTxType, Status: 1, CumulativeGasUsed: 21000, Logs: []*types.Log{}}}
	blk := types.NewBlock(header, &types.Body{Transactions: types.Transactions{tx}, Withdrawals: []*types.Withdrawal{}}, receipts, trie.NewStackTrie(nil))

	h, err := rlp.EncodeToBytes(blk.Header())
	require.NoError(t, err)
	body, err := rlp.EncodeToBytes(blk.Body())
	require.NoError(t, err)
	rs, err := rlp.EncodeToBytes(receipts)
	require.NoError(t, err)

	p := New("test")
	b, err := p.DecodeBlockRLP(h, body)
	require.NoError(t, err)
	require.Equal(t, blk.Hash(), b.Hash)
	require.Equal(t, []common.Hash{{1}, {2}}, b.Transactions[0].BlobHashes)
	require.NotNil(t, b.Withdrawals, "Cancun body carries a withdrawals list")
	got, err := p.DeriveReceipts(b, rs)
	require.NoError(t, err)
	require.Equal(t, used, got[0].BlobGasUsed)
	require.Nil(t, got[0].BlobGasPrice, "unsupported in binary decoding")
	require.Equal(t, big.NewInt(12), got[0].EffectiveGasPrice)
}
