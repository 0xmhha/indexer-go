package app

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/features/dex/orderbook"
	"github.com/0xmhha/indexer-go/pkg/token"
)

// initOrderBook builds the DEX order books (refactoring plan R5-2) when
// dex.orderbook is enabled and this process serves queries: it follows the
// event bus, loads every market and attaches the books to the storage, where
// the GraphQL extension finds them.
func (a *App) initOrderBook(ctx context.Context) error {
	if !a.featureOverrides()[orderbook.Name] || a.config.NodeRole() == config.RoleIngest {
		return nil
	}
	store, ok := a.storage.(orderbook.Store)
	if !ok {
		return fmt.Errorf("%s: storage does not support DEX indexing", orderbook.Name)
	}
	var st orderbook.Settings
	if err := a.config.FeatureSettings(orderbook.Name, &st); err != nil {
		return err
	}
	var caller feature.ContractReader
	if a.client != nil {
		caller = token.NewEthClientAdapter(a.client.EthClient())
	}
	svc, err := orderbook.NewService(store, caller, st, a.logger)
	if err != nil {
		return err
	}
	if a.eventBus != nil {
		if err := svc.Follow(a.eventBus); err != nil {
			svc.Close()
			return fmt.Errorf("%s: %w", orderbook.Name, err)
		}
	}
	if err := svc.Start(ctx); err != nil {
		svc.Close()
		return fmt.Errorf("%s: %w", orderbook.Name, err)
	}
	orderbook.Attach(a.storage, svc)
	a.orderBook = svc
	a.logger.Info("DEX order books started", zap.Int("markets", len(svc.Books())))
	return nil
}

// closeOrderBook stops the order books.
func (a *App) closeOrderBook() {
	if a.orderBook != nil {
		a.orderBook.Close()
		a.orderBook = nil
	}
}
