package app

import (
	"context"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// errNoFetcher is FetchRange's answer when the App indexes nothing itself.
var errNoFetcher = errors.New("app: this App has no fetcher (multi-chain or api role)")

// Storage returns the store a single-chain App indexes into, for programs
// that embed the engine and for tests (pkg/app/apptest); nil in multi-chain
// mode.
func (a *App) Storage() storage.Storage { return a.storage }

// EventBus returns the App's event bus; nil in multi-chain mode.
func (a *App) EventBus() *events.EventBus { return a.eventBus }

// FetchRange indexes the blocks from..to (inclusive) and returns once they
// are committed, without the live loop.
func (a *App) FetchRange(ctx context.Context, from, to uint64) error {
	if a.fetcher == nil {
		return errNoFetcher
	}
	return a.fetcher.FetchRange(ctx, from, to)
}
