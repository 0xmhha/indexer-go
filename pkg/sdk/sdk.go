// Package sdk is what a project needs to add its own handlers to the
// indexer without changing it (refactoring plan R6-2). A handler is a
// feature: it registers in init, receives every indexed block inside the
// block's storage transaction (so what it writes commits with the block and
// is rolled back with it on a reorganization) and may add GraphQL queries
// and HTTP routes over the storage. The project builds its own binary: a
// main package that imports the handler's package and calls Main.
//
//	package main
//
//	import (
//		"github.com/0xmhha/indexer-go/pkg/sdk"
//		_ "example.com/receipts" // registers its feature and routes
//	)
//
//	func main() { sdk.Main() }
//
// Configuration is the indexer's (config.yaml, environment, flags): the
// handler is enabled with features.<name>.enabled and reads its own section
// with Deps.DecodeSettings. With indexer.mode declared and the records
// feature, the indexer reads and stores only the declared logs, and the
// handler reads them as records (Records, Tables).
//
// Everything here is an alias or thin wrapper of the indexer's packages,
// so values pass between them unchanged.
package sdk

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/app"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/declared"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/features/records"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// Main runs the indexer with the handlers the program links in. It parses
// the command line, runs until interrupted and exits the process on error.
func Main() { app.Main() }

// Handlers (features).
type (
	// Feature is a handler: a name, the features it needs and Register,
	// which attaches its block (and rollback) handlers.
	Feature = feature.Feature
	// Registrar is what a feature attaches its handlers to.
	Registrar = feature.Registrar
	// Deps are the services a feature may use: Storage (take the ports
	// you use by type assertion, or KVOf), Logger, Publish, the node
	// (Contracts, BlockAt) and DecodeSettings for its own settings.
	Deps = feature.Deps
	// Block is an indexed block as handlers see it: Model and Receipts.
	Block = feature.Block
	// BlockHandler processes one block inside its storage transaction;
	// an error aborts the block.
	BlockHandler = feature.BlockHandler
	// BlockHandlerFunc adapts a function to BlockHandler.
	BlockHandlerFunc = feature.BlockHandlerFunc
	// RollbackHandler withdraws what a feature published for a block a
	// reorganization rolls back (its writes are undone without it).
	RollbackHandler = feature.RollbackHandler
	// OrderIndependent marks a feature whose result does not depend on
	// the order blocks are processed in, so it backfills online.
	OrderIndependent = feature.OrderIndependent
	// LogsOnly marks a feature that reads nothing but the logs of the
	// declared contracts, so it runs with indexer.mode declared.
	LogsOnly = feature.LogsOnly
)

// RegisterFeature adds a feature; call it from init. A name registered
// twice panics.
func RegisterFeature(f Feature) { feature.Register(f) }

// Chain data.
type (
	BlockModel = model.Block
	Receipt    = model.Receipt
	Log        = model.Log
	Event      = events.Event
)

// Storage.
type (
	// Store is the storage the API serves from.
	Store = port.QueryStore
	// KV is the key-value store of a handler's own data. Bound to the
	// block transaction by ctx: writes in HandleBlock commit with the
	// block and are undone on a reorganization.
	KV = port.KV
	// Page is a page request of the list ports.
	Page = port.Page
)

// ErrNotFound is returned by reads of absent keys and records.
var ErrNotFound = port.ErrNotFound

// KVOf returns the key-value store of a storage (Deps.Storage, or the
// Store a route or GraphQL extension gets).
func KVOf(s any) (KV, error) {
	kv, ok := s.(port.KV)
	if !ok {
		return nil, errors.New("sdk: the storage has no key-value store")
	}
	return kv, nil
}

// RegisterKeyspace declares the key prefixes a handler writes, so
// reindexing clears them with the chain data. Call it from init; prefixes
// must not overlap the indexer's.
func RegisterKeyspace(owner string, prefixes ...string) {
	storage.RegisterKeyspace(owner, storage.ChainData, prefixes...)
}

// Declared tables (features.records).
type (
	Record      = port.Record
	RecordKey   = port.RecordKey
	Tables      = declared.Plan
	Table       = declared.TablePlan
	RecordStore = port.RecordReader
)

// DeclaredTables compiles the declared tables (features.records) from a
// feature's dependencies, for a handler that reads them while it
// registers.
func DeclaredTables(deps Deps) (*Tables, error) { return records.Settings(deps.DecodeSettings) }

// TablesOf returns the declared tables served from a storage, nil when the
// records feature is off.
func TablesOf(s any) *Tables { return records.Lookup(s) }

// RecordsOf returns the records of a storage.
func RecordsOf(s any) (RecordStore, error) {
	r, ok := s.(port.RecordReader)
	if !ok {
		return nil, errors.New("sdk: the storage keeps no records")
	}
	return r, nil
}

// LookupRecords returns one page of a declared table's records whose
// fields have the given values for one of the table's declared keys,
// oldest first. Values are normalized as records store them.
func LookupRecords(ctx context.Context, s any, table string, values map[string]string, page Page) ([]*Record, string, error) {
	tables := TablesOf(s)
	if tables == nil {
		return nil, "", errors.New("sdk: no declared tables (features.records)")
	}
	t, ok := tables.Table(table)
	if !ok {
		return nil, "", errors.New("sdk: no table " + table + " is declared")
	}
	id, normalized, err := t.Lookup(values)
	if err != nil {
		return nil, "", err
	}
	key := RecordKey{ID: id, Values: normalized}
	rs, err := RecordsOf(s)
	if err != nil {
		return nil, "", err
	}
	return rs.ListRecordsByKey(ctx, table, key, page)
}

// Progress is how far indexing has come: Indexed is the latest indexed
// block; Target the highest block the node offered under the finality
// policy at the live loop's last poll, valid when Polled.
type Progress struct {
	Indexed uint64
	Target  uint64
	Polled  bool
}

// Lag is Target minus Indexed (0 when indexing is ahead or not polled).
func (p Progress) Lag() uint64 {
	if !p.Polled || p.Indexed >= p.Target {
		return 0
	}
	return p.Target - p.Indexed
}

// ProgressOf returns the indexing progress of a storage served in this
// process. Without a live loop indexing into it (an API process of
// node.role api) Polled is false.
func ProgressOf(ctx context.Context, s Store) (Progress, error) {
	var p Progress
	indexed, err := s.GetLatestHeight(ctx)
	if err != nil && !errors.Is(err, port.ErrNotFound) {
		return p, err
	}
	p.Indexed = indexed
	p.Target, p.Polled, _ = fetch.ProgressOf(s)
	return p, nil
}

// API.
type (
	// GraphQLExtension is what a GraphQL extension adds queries,
	// mutations and subscriptions to.
	GraphQLExtension = graphql.Extension
	// Route is an HTTP endpoint next to the GraphQL API.
	Route = api.Route
)

// RegisterGraphQL adds a GraphQL extension; call it from init.
func RegisterGraphQL(name string, fn func(*GraphQLExtension)) { graphql.RegisterExtension(name, fn) }

// RegisterRoute adds an HTTP endpoint; call it from init. handler builds
// it over the served storage.
func RegisterRoute(method, pattern string, handler func(store Store, logger *zap.Logger) http.Handler) {
	api.RegisterRoute(api.Route{Method: method, Pattern: pattern, Handler: handler})
}

// URLParam returns a parameter of a route pattern ("{name}").
func URLParam(r *http.Request, name string) string { return chi.URLParam(r, name) }
