// Package address is the address.index feature: it indexes every
// transaction under its sender, recipient and fee payer, and records
// contract creations.
package address

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/feature"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// Name is the feature name.
const Name = "address.index"

type addressFeature struct{}

func (addressFeature) Name() string       { return Name }
func (addressFeature) Requires() []string { return nil }
func (addressFeature) DefaultOn() bool    { return true }

func (addressFeature) Register(r feature.Registrar) error {
	st := r.Deps().Storage
	txIndex, ok := st.(storagepkg.Writer)
	if !ok {
		return fmt.Errorf("storage does not support the address transaction index")
	}
	creations, ok := st.(storagepkg.AddressIndexWriter)
	if !ok {
		return fmt.Errorf("storage does not support contract creations")
	}
	r.OnBlock(&handler{storage: st, txIndex: txIndex, creations: creations})
	return nil
}

func init() { feature.Register(addressFeature{}) }

type handler struct {
	storage   storagepkg.Storage
	txIndex   storagepkg.Writer
	creations storagepkg.AddressIndexWriter
}

// HandleBlock indexes the block's transactions. Hashes and addresses come
// from the model, so a fee delegation transaction is indexed under its own
// hash and also under its fee payer.
func (h *handler) HandleBlock(ctx context.Context, b *feature.Block) error {
	for _, p := range b.Transactions() {
		tx, receipt := p.Tx, p.Receipt

		from := tx.From
		if from != (common.Address{}) {
			if err := h.txIndex.AddTransactionToAddressIndex(ctx, from, tx.Hash); err != nil {
				return fmt.Errorf("index tx %s for sender: %w", tx.Hash.Hex(), err)
			}
		}
		if tx.To != nil && *tx.To != from { // a self-transfer is indexed once
			if err := h.txIndex.AddTransactionToAddressIndex(ctx, *tx.To, tx.Hash); err != nil {
				return fmt.Errorf("index tx %s for recipient: %w", tx.Hash.Hex(), err)
			}
		}
		if payer, ok := feature.DelegatedFeePayer(ctx, h.storage, tx); ok {
			if payer != from && (tx.To == nil || payer != *tx.To) {
				if err := h.txIndex.AddTransactionToAddressIndex(ctx, payer, tx.Hash); err != nil {
					return fmt.Errorf("index tx %s for fee payer: %w", tx.Hash.Hex(), err)
				}
			}
		}

		if tx.To == nil && receipt.ContractAddress != nil {
			contract := *receipt.ContractAddress
			creation := &storagepkg.ContractCreation{
				ContractAddress: contract,
				Creator:         from,
				TransactionHash: tx.Hash,
				BlockNumber:     b.Model.Number,
				Timestamp:       b.Model.Time,
				BytecodeSize:    len(contract.Bytes()), // simplified, as before
			}
			if err := h.creations.SaveContractCreation(ctx, creation); err != nil {
				return fmt.Errorf("save creation of %s: %w", contract.Hex(), err)
			}
		}
	}
	return nil
}
