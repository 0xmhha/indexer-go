// Package feedelegation is the stablenet.fee_delegation feature: it stores
// the fee payer and its signature of every fee delegation transaction
// (type 0x16) for the fee delegation queries.
//
// Fee payers are also used by address indexing and balance tracking, which
// do not depend on this feature: who pays gas is needed for correct
// balances whether or not the metadata is kept.
package feedelegation

import (
	"context"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

// Name is the feature name.
const Name = "stablenet.fee_delegation"

type feeDelegationFeature struct{}

func (feeDelegationFeature) Name() string       { return Name }
func (feeDelegationFeature) Requires() []string { return nil }

// OrderIndependent: entries are keyed by transaction and log, with no
// running totals, so blocks can be processed in any order.
func (feeDelegationFeature) OrderIndependent() bool { return true }

func (feeDelegationFeature) Register(r feature.Registrar) error {
	w, ok := r.Deps().Storage.(port.FeeDelegationWriter)
	if !ok {
		return fmt.Errorf("storage does not support fee delegation metadata")
	}
	r.OnBlock(feature.BlockHandlerFunc(func(ctx context.Context, b *feature.Block) error {
		for _, tx := range b.Model.Transactions {
			fd, ok := chains.FeeDelegationOf(tx)
			if !ok {
				continue
			}
			meta := &port.FeeDelegationTxMeta{
				TxHash:       tx.Hash,
				BlockNumber:  b.Model.Number,
				OriginalType: tx.Type,
				FeePayer:     fd.Payer,
				FeePayerV:    fd.V,
				FeePayerR:    fd.R,
				FeePayerS:    fd.S,
			}
			if err := w.SetFeeDelegationTxMeta(ctx, meta); err != nil {
				return fmt.Errorf("store fee delegation metadata of %s: %w", tx.Hash.Hex(), err)
			}
		}
		return nil
	}))
	return nil
}

func init() { feature.Register(feeDelegationFeature{}) }
