package multichain

import (
	"context"
	"time"

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

// NodeHeightObserver optionally shares recent raw-head reads with ingestion.
// Latency is the duration of the observed RPC, even when the result is reused.
// An implementation must respect maxAge (zero means fresh), expire observations
// and invalidate them after errors.
// Indexers without it keep the fresh NodeHeight health probe.
type NodeHeightObserver interface {
	ObserveNodeHeight(context.Context, time.Duration) (uint64, time.Duration, error)
}
