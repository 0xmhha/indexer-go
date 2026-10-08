// Package token is the token.transfers feature: it indexes ERC-20 and
// ERC-721 Transfer events.
package token

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

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
	t.excluded = ExcludedContract(r.Deps().Profile)
	r.OnBlock(t)
	return nil
}

func init() { feature.Register(transfersFeature{}) }

type transfers struct {
	w        port.AddressIndexWriter
	excluded *common.Address
}

// ExcludedContract returns the contract whose Transfer events are no token
// transfers on the profile's chain: its native coin contract, whose
// transfers are native value moves (balance.native). Nil when there is none.
func ExcludedContract(profile chains.Profile) *common.Address {
	if a, ok := chains.NativeCoinContract(profile); ok {
		return &a
	}
	return nil
}

// Transfer is a token Transfer(address,address,uint256) log. The standard is
// told apart by the number of topics: three for ERC-20 (value in data), four
// for ERC-721 (token id as a topic).
type Transfer struct {
	Token    common.Address
	From, To common.Address
	ERC721   bool
	Value    *big.Int // ERC-20
	TokenID  *big.Int // ERC-721
}

// ReadTransfer reads a log as token.transfers does; false when the log is
// no token transfer. Transfer logs of excluded (ExcludedContract; nil for
// none) are not token transfers.
func ReadTransfer(log *types.Log, excluded *common.Address) (Transfer, bool) {
	if log == nil || len(log.Topics) == 0 || log.Topics[0].Hex() != port.ERC20TransferTopic {
		return Transfer{}, false
	}
	if excluded != nil && log.Address == *excluded {
		return Transfer{}, false
	}
	if len(log.Topics) != 3 && len(log.Topics) != 4 {
		return Transfer{}, false
	}
	t := Transfer{Token: log.Address, From: common.BytesToAddress(log.Topics[1].Bytes())}
	switch len(log.Topics) {
	case 3:
		if len(log.Data) < 32 {
			return Transfer{}, false
		}
		t.To, t.Value = common.BytesToAddress(log.Topics[2].Bytes()), new(big.Int).SetBytes(log.Data)
	case 4:
		t.To, t.ERC721, t.TokenID = common.BytesToAddress(log.Topics[2].Bytes()), true, new(big.Int).SetBytes(log.Topics[3].Bytes())
	}
	return t, true
}

// HandleBlock indexes the token transfers of a block (ReadTransfer).
func (t *transfers) HandleBlock(ctx context.Context, b *feature.Block) error {
	for _, receipt := range b.GethReceipts {
		for _, log := range receipt.Logs {
			tr, ok := ReadTransfer(log, t.excluded)
			if !ok {
				continue
			}
			if !tr.ERC721 {
				if err := t.w.SaveERC20Transfer(ctx, &port.ERC20Transfer{
					ContractAddress: tr.Token,
					From:            tr.From,
					To:              tr.To,
					Value:           tr.Value,
					TransactionHash: log.TxHash,
					BlockNumber:     log.BlockNumber,
					LogIndex:        log.Index,
					Timestamp:       b.Model.Time,
				}); err != nil {
					return fmt.Errorf("save ERC-20 transfer in %s: %w", log.TxHash.Hex(), err)
				}
				continue
			}
			if err := t.w.SaveERC721Transfer(ctx, &port.ERC721Transfer{
				ContractAddress: tr.Token,
				From:            tr.From,
				To:              tr.To,
				TokenId:         tr.TokenID,
				TransactionHash: log.TxHash,
				BlockNumber:     log.BlockNumber,
				LogIndex:        log.Index,
				Timestamp:       b.Model.Time,
			}); err != nil {
				return fmt.Errorf("save ERC-721 transfer in %s: %w", log.TxHash.Hex(), err)
			}
		}
	}
	return nil
}
