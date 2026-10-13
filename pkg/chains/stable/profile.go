// Package stable is the RPC profile for the USDT0-native Stable chain.
// Its Ethereum-shaped RPC header reports the CometBFT consensus hash,
// which cannot be recomputed from the Ethereum fields alone.
package stable

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

const ID = "stable"

// Profile reuses EVM transaction verification and receipt decoding while
// preserving the node's consensus block identity. It deliberately does
// not implement BinaryProfile: Ethereum RLP cannot recover that identity.
type Profile struct{ decoder *evm.Profile }

var _ chains.Profile = (*Profile)(nil)

func New() *Profile {
	return &Profile{decoder: evm.New(ID, evm.WithHeaderHashCheck(false), evm.WithTxDecoder(2, decodeDynamicFeeTx))}
}

func (*Profile) ID() string { return ID }
func (*Profile) Detect(info chains.NodeInfo) bool {
	return info.ChainID == 988 || info.ChainID == 2201
}
func (*Profile) Features() []string { return nil }

// DecodeBlock trusts the RPC's consensus hash, not an Ethereum RLP hash.
// Standard transaction hashes and senders are still verified by EVM; each
// transaction's reported location must agree with its containing block.
// This is RPC identity validation, not CometBFT consensus verification.
func (p *Profile) DecodeBlock(raw json.RawMessage) (*model.Block, error) {
	b, err := p.decoder.DecodeBlock(raw)
	if err != nil {
		return nil, err
	}
	if b.Hash == (common.Hash{}) {
		return nil, fmt.Errorf("stable: missing consensus block hash")
	}
	var body struct {
		Transactions []struct {
			BlockHash        *common.Hash    `json:"blockHash"`
			BlockNumber      *hexutil.Uint64 `json:"blockNumber"`
			TransactionIndex *hexutil.Uint64 `json:"transactionIndex"`
		} `json:"transactions"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for i, tx := range body.Transactions {
		if tx.BlockHash == nil || *tx.BlockHash != b.Hash {
			return nil, fmt.Errorf("stable: transaction %d has a different consensus block hash", i)
		}
		if tx.BlockNumber == nil || uint64(*tx.BlockNumber) != b.Number ||
			tx.TransactionIndex == nil || uint64(*tx.TransactionIndex) != uint64(i) {
			return nil, fmt.Errorf("stable: transaction %d has a different block location", i)
		}
	}
	return b, nil
}

func (p *Profile) DecodeReceipts(raw json.RawMessage) ([]*model.Receipt, error) {
	return p.decoder.DecodeReceipts(raw)
}

func init() { chains.Register(New(), 100) }
