// Package aa holds the account abstraction features: aa.eip7702 (SetCode
// authorizations), aa.erc4337 (UserOperations, bundlers, paymasters) and
// aa.erc7579 (smart account modules). The processors themselves are still
// in pkg/fetch; these features attach them to the block pipeline.
package aa

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/fetch"
)

// Feature names.
const (
	EIP7702 = "aa.eip7702"
	ERC4337 = "aa.erc4337"
	ERC7579 = "aa.erc7579"
)

func logger(d feature.Deps) *zap.Logger {
	if d.Logger == nil {
		return zap.NewNop()
	}
	return d.Logger
}

type eip7702 struct{}

func (eip7702) Name() string       { return EIP7702 }
func (eip7702) Requires() []string { return nil }
func (eip7702) DefaultOn() bool    { return true }

func (eip7702) Register(r feature.Registrar) error {
	d := r.Deps()
	s, ok := d.Storage.(fetch.SetCodeIndexer)
	if !ok {
		return fmt.Errorf("storage does not support SetCode authorizations")
	}
	log := logger(d)
	p := fetch.NewSetCodeProcessor(log, s)
	r.OnBlock(feature.BlockHandlerFunc(func(ctx context.Context, b *feature.Block) error {
		for _, t := range b.Transactions() {
			if t.Tx.Type != types.SetCodeTxType {
				continue
			}
			if err := p.ProcessSetCodeTransactionAt(ctx, t.GethTx, t.GethReceipt, b.Model.Number, b.Model.Hash, b.Model.Time, uint64(t.Index)); err != nil {
				log.Warn("Failed to process SetCode transaction",
					zap.Uint64("block", b.Model.Number), zap.String("tx", t.Tx.Hash.Hex()), zap.Error(err))
			}
		}
		return nil
	}))
	return nil
}

type erc4337 struct{}

func (erc4337) Name() string       { return ERC4337 }
func (erc4337) Requires() []string { return nil }
func (erc4337) DefaultOn() bool    { return true }

func (erc4337) Register(r feature.Registrar) error {
	d := r.Deps()
	s, ok := d.Storage.(fetch.UserOpIndexer)
	if !ok {
		return fmt.Errorf("storage does not support UserOperations")
	}
	log := logger(d)
	p := fetch.NewUserOpProcessor(log, s)
	r.OnBlock(feature.BlockHandlerFunc(func(ctx context.Context, b *feature.Block) error {
		txs := b.Transactions()
		bundles := make([]fetch.UserOpBundle, 0, len(txs))
		for _, t := range txs {
			bundles = append(bundles, fetch.UserOpBundle{Sender: t.Tx.From, Receipt: t.GethReceipt})
		}
		if err := p.ProcessUserOps(ctx, b.Model.Number, b.Model.Hash, b.Model.Time, bundles); err != nil {
			log.Warn("Failed to process ERC-4337 UserOperations", zap.Uint64("block", b.Model.Number), zap.Error(err))
		}
		return nil
	}))
	return nil
}

type erc7579 struct{}

func (erc7579) Name() string       { return ERC7579 }
func (erc7579) Requires() []string { return nil }
func (erc7579) DefaultOn() bool    { return true }

func (erc7579) Register(r feature.Registrar) error {
	d := r.Deps()
	s, ok := d.Storage.(fetch.ModuleIndexer)
	if !ok {
		return fmt.Errorf("storage does not support ERC-7579 modules")
	}
	log := logger(d)
	p := fetch.NewModuleProcessor(log, s)
	r.OnBlock(feature.BlockHandlerFunc(func(ctx context.Context, b *feature.Block) error {
		if err := p.ProcessModuleEventsFromBlock(ctx, b.Geth, b.GethReceipts); err != nil {
			log.Warn("Failed to process ERC-7579 module events", zap.Uint64("block", b.Model.Number), zap.Error(err))
		}
		return nil
	}))
	return nil
}

func init() {
	feature.Register(eip7702{})
	feature.Register(erc4337{})
	feature.Register(erc7579{})
}
