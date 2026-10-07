package multichain

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// Indexer is one chain's indexing pipeline with its own storage
// (refactoring plan R2-8). The binary builds it with the same wiring as
// single-chain mode, so a chain indexed here stores exactly what a
// single-chain indexer would, in a database no other chain writes to.
type Indexer interface {
	// Run indexes until ctx ends or a fatal error.
	Run(ctx context.Context) error
	// Close releases the storage and the node connections.
	Close()
	// IndexedHeight is the chain's latest indexed block.
	IndexedHeight(ctx context.Context) (uint64, error)
	// NodeHeight is the head the chain's node reports.
	NodeHeight(ctx context.Context) (uint64, error)
	// Store serves the chain's data to the APIs.
	Store() port.QueryStore
	// EventBus publishes the chain's events.
	EventBus() *events.EventBus
}

// IndexerFactory builds the indexer of a chain.
type IndexerFactory func(ctx context.Context, cfg *ChainConfig) (Indexer, error)
