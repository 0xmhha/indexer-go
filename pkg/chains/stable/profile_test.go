package stable_test

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/chains/stable"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// The full RPC block was fetched from rpc.testnet.stable.xyz on 2026-10-13.
// It reproduces the mismatch between the CometBFT hash and Ethereum RLP.
func TestLiveConsensusBlockIdentity(t *testing.T) {
	raw, err := os.ReadFile("testdata/block-72432091.json")
	require.NoError(t, err)
	_, err = evm.New("evm").DecodeBlock(raw)
	require.ErrorIs(t, err, evm.ErrHashMismatch)
	p := stable.New()
	b, err := p.DecodeBlock(raw)
	require.NoError(t, err)
	want := common.HexToHash("0x83a83680e87263d4d092166d6c67770b74326f3812b7fe7b1f7e2bba75ce7f43")
	require.Equal(t, want, b.Hash)
	require.Equal(t, uint64(72432091), b.Number)
	require.Len(t, b.Transactions, 1)
	require.True(t, b.Transactions[0].Opaque, "protocol-generated transaction has no Ethereum sender signature")
	require.Equal(t, want, b.Transactions[0].BlockHash)
	encoded, err := model.EncodeBlock(b)
	require.NoError(t, err)
	stored, err := model.DecodeBlock(encoded)
	require.NoError(t, err)
	require.Equal(t, want, stored.Hash)
	_, binary := any(p).(chains.BinaryProfile)
	require.False(t, binary, "Ethereum RLP cannot recover a CometBFT identity")

	for _, field := range []string{"hash", "from", "blockHash"} {
		t.Run("reject changed transaction "+field, func(t *testing.T) {
			var block map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &block))
			var txs []map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(block["transactions"], &txs))
			value := common.HexToHash("0x01").Hex()
			if field == "from" {
				value = common.HexToAddress("0x02").Hex()
			}
			txs[0][field], err = json.Marshal(value)
			require.NoError(t, err)
			block["transactions"], err = json.Marshal(txs)
			require.NoError(t, err)
			changed, err := json.Marshal(block)
			require.NoError(t, err)
			_, err = p.DecodeBlock(changed)
			switch field {
			case "hash":
				require.ErrorIs(t, err, evm.ErrHashMismatch)
			case "from":
				require.ErrorContains(t, err, "not a recognized system call")
			case "blockHash":
				require.ErrorContains(t, err, "different consensus block hash")
			}
		})
	}
	var block map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &block))
	delete(block, "hash")
	missing, err := json.Marshal(block)
	require.NoError(t, err)
	_, err = p.DecodeBlock(missing)
	require.ErrorContains(t, err, "missing consensus block hash")
	for _, field := range []string{"number", "transactionIndex"} {
		t.Run("reject changed location "+field, func(t *testing.T) {
			var data map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &data))
			if field == "number" {
				data[field] = json.RawMessage(`"0x1"`)
			} else {
				var txs []map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(data["transactions"], &txs))
				txs[0][field] = json.RawMessage(`"0x1"`)
				data["transactions"], err = json.Marshal(txs)
				require.NoError(t, err)
			}
			changed, err := json.Marshal(data)
			require.NoError(t, err)
			_, err = p.DecodeBlock(changed)
			require.ErrorContains(t, err, "different block location")
		})
	}
}

func TestDetection(t *testing.T) {
	for _, id := range []uint64{988, 2201} {
		p, err := chains.Detect(chains.NodeInfo{ChainID: id})
		require.NoError(t, err)
		require.Equal(t, stable.ID, p.ID())
	}
	for _, id := range []uint64{1, 8283, 5042002} {
		require.False(t, stable.New().Detect(chains.NodeInfo{ChainID: id}))
	}
}

func TestSignedUserTransactionIsVerified(t *testing.T) {
	raw, err := os.ReadFile("testdata/block-72432091.json")
	require.NoError(t, err)
	var block map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &block))
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	to := common.HexToAddress("0x1234")
	tx, err := types.SignNewTx(key, types.LatestSignerForChainID(big.NewInt(2201)), &types.DynamicFeeTx{
		ChainID: big.NewInt(2201), To: &to, Gas: 21000, GasFeeCap: big.NewInt(10), GasTipCap: big.NewInt(0), Value: big.NewInt(7),
	})
	require.NoError(t, err)
	txRaw, err := tx.MarshalJSON()
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(txRaw, &fields))
	fields["from"], err = json.Marshal(crypto.PubkeyToAddress(key.PublicKey))
	require.NoError(t, err)
	fields["blockHash"] = block["hash"]
	fields["blockNumber"] = block["number"]
	fields["transactionIndex"] = json.RawMessage(`"0x0"`)
	encode := func() []byte {
		block["transactions"], err = json.Marshal([]map[string]json.RawMessage{fields})
		require.NoError(t, err)
		result, err := json.Marshal(block)
		require.NoError(t, err)
		return result
	}
	p := stable.New()
	b, err := p.DecodeBlock(encode())
	require.NoError(t, err)
	require.False(t, b.Transactions[0].Opaque)
	fields["from"], err = json.Marshal(common.HexToAddress("0x02"))
	require.NoError(t, err)
	_, err = p.DecodeBlock(encode())
	require.ErrorIs(t, err, evm.ErrSenderMismatch)
	fields["hash"], err = json.Marshal(common.HexToHash("0x02"))
	require.NoError(t, err)
	_, err = p.DecodeBlock(encode())
	require.ErrorIs(t, err, evm.ErrHashMismatch)
}
