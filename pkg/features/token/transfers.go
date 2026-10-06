// Package token is the token.transfers feature: it indexes ERC-20 and
// ERC-721 Transfer events.
package token

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

// TransfersName is the feature name.
const TransfersName = "token.transfers"

type transfersFeature struct{}

func (transfersFeature) Name() string       { return TransfersName }
func (transfersFeature) Requires() []string { return nil }
func (transfersFeature) DefaultOn() bool    { return true }

// token.transfers is order-dependent: ERC-721 transfers update the token's
// current owner, so processing an older transfer after a newer one would
// restore an old owner. It is therefore not feature.OrderIndependent.

func (transfersFeature) Register(r feature.Registrar) error {
	w, ok := r.Deps().Storage.(port.AddressIndexWriter)
	if !ok {
		return fmt.Errorf("storage does not support token transfers")
	}
	t := &transfers{w: w}
	t.nativeCoin, t.hasNativeCoin = chains.NativeCoinContract(r.Deps().Profile)
	r.OnBlock(t)
	return nil
}

func init() { feature.Register(transfersFeature{}) }

type transfers struct {
	w port.AddressIndexWriter
	// The chain's native coin contract: its Transfer events are native
	// value moves (balance.native), not token transfers.
	nativeCoin    common.Address
	hasNativeCoin bool
}

// HandleBlock indexes Transfer(address,address,uint256) logs. The standard is
// told apart by the number of topics: three for ERC-20 (value in data), four
// for ERC-721 (token id as a topic).
func (t *transfers) HandleBlock(ctx context.Context, b *feature.Block) error {
	for _, receipt := range b.GethReceipts {
		for _, log := range receipt.Logs {
			if log == nil || len(log.Topics) == 0 || log.Topics[0].Hex() != port.ERC20TransferTopic {
				continue
			}
			if t.hasNativeCoin && log.Address == t.nativeCoin {
				continue
			}
			switch len(log.Topics) {
			case 3:
				if len(log.Data) < 32 {
					continue
				}
				if err := t.w.SaveERC20Transfer(ctx, &port.ERC20Transfer{
					ContractAddress: log.Address,
					From:            common.BytesToAddress(log.Topics[1].Bytes()),
					To:              common.BytesToAddress(log.Topics[2].Bytes()),
					Value:           new(big.Int).SetBytes(log.Data),
					TransactionHash: log.TxHash,
					BlockNumber:     log.BlockNumber,
					LogIndex:        log.Index,
					Timestamp:       b.Model.Time,
				}); err != nil {
					return fmt.Errorf("save ERC-20 transfer in %s: %w", log.TxHash.Hex(), err)
				}
			case 4:
				if err := t.w.SaveERC721Transfer(ctx, &port.ERC721Transfer{
					ContractAddress: log.Address,
					From:            common.BytesToAddress(log.Topics[1].Bytes()),
					To:              common.BytesToAddress(log.Topics[2].Bytes()),
					TokenId:         new(big.Int).SetBytes(log.Topics[3].Bytes()),
					TransactionHash: log.TxHash,
					BlockNumber:     log.BlockNumber,
					LogIndex:        log.Index,
					Timestamp:       b.Model.Time,
				}); err != nil {
					return fmt.Errorf("save ERC-721 transfer in %s: %w", log.TxHash.Hex(), err)
				}
			}
		}
	}
	return nil
}
