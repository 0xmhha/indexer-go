package stablenet_test

import (
	"crypto/ecdsa"
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// liveVectors were captured from a local go-stablenet network (Gstable
// v1.1.0, commit 740526d, three validators, chain id 8283, Applepie at
// genesis): full blocks 0, 1, 5, 33 and 100, the canonical encodings of
// block 33's transactions (eth_getRawTransactionByHash) and its receipts.
type liveVectors struct {
	Blocks          map[string]json.RawMessage `json:"blocks"`
	RawTransactions map[string]string          `json:"rawTransactions"`
	Receipts        map[string]json.RawMessage `json:"receipts"`
}

func loadVectors(t *testing.T) liveVectors {
	t.Helper()
	b, err := os.ReadFile("testdata/live_vectors.json")
	require.NoError(t, err)
	var v liveVectors
	require.NoError(t, json.Unmarshal(b, &v))
	return v
}

func TestHeaderHashMatchesLiveBlocks(t *testing.T) {
	v := loadVectors(t)
	for n, raw := range v.Blocks {
		var head types.Header
		require.NoError(t, json.Unmarshal(raw, &head))
		var reported struct {
			Hash common.Hash `json:"hash"`
		}
		require.NoError(t, json.Unmarshal(raw, &reported))

		require.Equal(t, reported.Hash, stablenet.HeaderHash(&head), "block %s", n)
		if n != "0" {
			// D16: the plain Ethereum rule gives a different hash for every
			// sealed WBFT block, which the legacy ingest path stored.
			require.NotEqual(t, reported.Hash, head.Hash(), "block %s", n)
		}
	}
}

func TestDecodeLiveFeeDelegationBlock(t *testing.T) {
	v := loadVectors(t)
	p := stablenet.New()

	b, err := p.DecodeBlock(v.Blocks["33"])
	require.NoError(t, err)
	require.Len(t, b.Transactions, 2)

	var reported struct {
		Transactions []struct {
			Hash     common.Hash     `json:"hash"`
			Type     hexutil.Uint64  `json:"type"`
			From     common.Address  `json:"from"`
			FeePayer *common.Address `json:"feePayer"`
		} `json:"transactions"`
	}
	require.NoError(t, json.Unmarshal(v.Blocks["33"], &reported))

	for i, want := range reported.Transactions {
		got := b.Transactions[i]
		require.Equal(t, want.Hash, got.Hash, "tx %d hash", i)
		require.Equal(t, uint8(want.Type), got.Type, "tx %d type kept as reported", i)
		require.Equal(t, want.From, got.From, "tx %d sender", i)
		require.False(t, got.Opaque)
		require.Equal(t, hexutil.MustDecode(v.RawTransactions[want.Hash.Hex()]), got.Raw, "tx %d canonical encoding", i)

		fd, isFD := stablenet.FeeDelegationOf(got)
		require.Equal(t, want.FeePayer != nil, isFD)
		if isFD {
			require.Equal(t, *want.FeePayer, fd.FeePayer)
			require.Equal(t, *want.FeePayer, stablenet.FeePayerOf(got))
			require.NotEqual(t, got.From, stablenet.FeePayerOf(got), "gas is paid by the fee payer")
		} else {
			require.Equal(t, got.From, stablenet.FeePayerOf(got))
		}
	}

	receipts, err := p.DecodeReceipts(v.Receipts["33"])
	require.NoError(t, err)
	require.Len(t, receipts, 2)
	require.Equal(t, uint8(stablenet.FeeDelegationTxType), receipts[1].Type, "receipt type kept as reported")
	require.Equal(t, b.Transactions[1].Hash, receipts[1].TxHash, "receipt links to the canonical hash")
}

func TestGenericEVMRejectsStableNetBlocks(t *testing.T) {
	v := loadVectors(t)
	_, err := evm.New("plain").DecodeBlock(v.Blocks["33"])
	require.ErrorIs(t, err, evm.ErrHashMismatch, "the generic rule cannot verify WBFT block hashes")
}

func TestDetection(t *testing.T) {
	p, err := chains.Detect(chains.NodeInfo{ClientVersion: "Gstable/v1.1.0-stable-740526d0/darwin-arm64/go1.25.2", ChainID: 8283})
	require.NoError(t, err)
	require.Equal(t, stablenet.ID, p.ID())
	require.ElementsMatch(t, []string{"stablenet.system_contracts", "stablenet.fee_delegation", "stablenet.wbft"}, p.Features())

	p, err = chains.Detect(chains.NodeInfo{ClientVersion: "Geth/v1.16.5-stable"})
	require.NoError(t, err)
	require.Equal(t, evm.ID, p.ID())
}

// synthFeeDelegationJSON builds a type 0x16 transaction object signed by
// sender and payerSigner while declaring declaredPayer as the fee payer.
func synthFeeDelegationJSON(t *testing.T, sender, payerSigner *ecdsaKey, declaredPayer common.Address, to *common.Address) []byte {
	t.Helper()
	chainID := big.NewInt(8283)
	inner := &types.DynamicFeeTx{
		ChainID: chainID, Nonce: 7, GasTipCap: big.NewInt(2), GasFeeCap: big.NewInt(100),
		Gas: 21000, To: to, Value: big.NewInt(5), Data: []byte{0xca, 0xfe},
	}
	signed := types.MustSignNewTx(sender.key, types.NewLondonSigner(chainID), inner)
	sv, sr, ss := signed.RawSignatureValues()

	var toField any = []byte{}
	if to != nil {
		toField = *to
	}
	senderFields := []any{chainID, inner.Nonce, inner.GasTipCap, inner.GasFeeCap, inner.Gas, toField, inner.Value, inner.Data, types.AccessList{}, sv, sr, ss}
	payerPayload, err := rlp.EncodeToBytes([]any{senderFields, declaredPayer})
	require.NoError(t, err)
	sig, err := crypto.Sign(crypto.Keccak256(append([]byte{0x16}, payerPayload...)), payerSigner.key)
	require.NoError(t, err)
	fr, fs, fv := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:64]), big.NewInt(int64(sig[64]))

	payload, err := rlp.EncodeToBytes([]any{senderFields, declaredPayer, fv, fr, fs})
	require.NoError(t, err)
	hash := crypto.Keccak256Hash(append([]byte{0x16}, payload...))

	obj := map[string]any{
		"type": "0x16", "hash": hash, "from": sender.addr,
		"chainId": (*hexutil.Big)(chainID), "nonce": hexutil.Uint64(inner.Nonce),
		"maxPriorityFeePerGas": (*hexutil.Big)(inner.GasTipCap), "maxFeePerGas": (*hexutil.Big)(inner.GasFeeCap),
		"gas": hexutil.Uint64(inner.Gas), "to": to, "value": (*hexutil.Big)(inner.Value),
		"input": hexutil.Bytes(inner.Data), "accessList": types.AccessList{},
		"v": (*hexutil.Big)(sv), "r": (*hexutil.Big)(sr), "s": (*hexutil.Big)(ss),
		"feePayer": declaredPayer, "fv": (*hexutil.Big)(fv), "fr": (*hexutil.Big)(fr), "fs": (*hexutil.Big)(fs),
	}
	out, err := json.Marshal(obj)
	require.NoError(t, err)
	return out
}

type ecdsaKey struct {
	key  *ecdsa.PrivateKey
	addr common.Address
}

func newKey(t *testing.T) *ecdsaKey {
	t.Helper()
	k, err := crypto.GenerateKey()
	require.NoError(t, err)
	return &ecdsaKey{key: k, addr: crypto.PubkeyToAddress(k.PublicKey)}
}

func TestSyntheticFeeDelegation(t *testing.T) {
	sender, payer, other := newKey(t), newKey(t), newKey(t)
	to := common.HexToAddress("0x00000000000000000000000000000000000ABCDE")

	t.Run("valid", func(t *testing.T) {
		tx, err := stablenet.DecodeFeeDelegationTx(synthFeeDelegationJSON(t, sender, payer, payer.addr, &to))
		require.NoError(t, err)
		require.Equal(t, sender.addr, tx.From)
		require.Equal(t, payer.addr, stablenet.FeePayerOf(tx))
	})
	t.Run("contract creation", func(t *testing.T) {
		tx, err := stablenet.DecodeFeeDelegationTx(synthFeeDelegationJSON(t, sender, payer, payer.addr, nil))
		require.NoError(t, err)
		require.Nil(t, tx.To)
	})
	t.Run("fee payer signature from another key", func(t *testing.T) {
		_, err := stablenet.DecodeFeeDelegationTx(synthFeeDelegationJSON(t, sender, other, payer.addr, &to))
		require.ErrorIs(t, err, stablenet.ErrFeePayerMismatch)
	})
	t.Run("missing field", func(t *testing.T) {
		var obj map[string]any
		require.NoError(t, json.Unmarshal(synthFeeDelegationJSON(t, sender, payer, payer.addr, &to), &obj))
		delete(obj, "feePayer")
		raw, _ := json.Marshal(obj)
		_, err := stablenet.DecodeFeeDelegationTx(raw)
		require.ErrorContains(t, err, "feePayer")
	})
}

func TestFeeDelegationSurvivesStorageEncoding(t *testing.T) {
	v := loadVectors(t)
	b, err := stablenet.New().DecodeBlock(v.Blocks["33"])
	require.NoError(t, err)

	enc, err := model.EncodeBlock(b)
	require.NoError(t, err)
	got, err := model.DecodeBlock(enc)
	require.NoError(t, err)
	require.Equal(t, b.Hash, got.Hash)

	want, _ := stablenet.FeeDelegationOf(b.Transactions[1])
	tx := got.Transactions[1]
	fd, ok := stablenet.FeeDelegationOf(tx)
	require.True(t, ok)
	require.Equal(t, want.FeePayer, fd.FeePayer)
	require.Zero(t, want.V.Cmp(fd.V))
	require.Zero(t, want.R.Cmp(fd.R))
	require.Zero(t, want.S.Cmp(fd.S))
	require.Equal(t, want.SenderHash, fd.SenderHash)
	require.Equal(t, b.Transactions[1].Hash, tx.Hash)
	require.Equal(t, b.Transactions[1].Raw, tx.Raw)
	require.Equal(t, uint8(stablenet.FeeDelegationTxType), tx.Type)

	_, ok = stablenet.FeeDelegationOf(got.Transactions[0])
	require.False(t, ok)
}
