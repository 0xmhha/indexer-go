package graphql

import (
	"context"
	"fmt"

	abiDecoder "github.com/0xmhha/indexer-go/pkg/abi"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
	"github.com/0xmhha/indexer-go/pkg/stream"
	"github.com/0xmhha/indexer-go/pkg/verifier"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// Schema holds the GraphQL schema
type Schema struct {
	schema     graphql.Schema
	storage    port.QueryStore
	logger     *zap.Logger
	abiDecoder *abiDecoder.Decoder
	verifier   verifier.Verifier
	rpcProxy   *rpcproxy.Proxy

	// Notification service
	notificationService notifications.Service

	// Dynamic contract registration service
	contractRegistrationService *events.ContractRegistrationService

	// stream is the outbox of the change stream (streamSequence); nil when
	// events are not kept
	stream stream.Outbox
}

// SchemaBuilder helps construct a GraphQL schema using the Builder pattern
type SchemaBuilder struct {
	schema        *Schema
	queries       graphql.Fields
	mutations     graphql.Fields
	subscriptions graphql.Fields
	extended      bool // registered extensions applied (extension.go)
}

// NewSchemaBuilder creates a new schema builder
func NewSchemaBuilder(store port.QueryStore, logger *zap.Logger) *SchemaBuilder {
	return &SchemaBuilder{
		schema: &Schema{
			storage:    store,
			logger:     logger,
			abiDecoder: abiDecoder.NewDecoder(),
		},
		queries:       make(graphql.Fields),
		mutations:     make(graphql.Fields),
		subscriptions: make(graphql.Fields),
	}
}

// Build constructs the final GraphQL schema
func (b *SchemaBuilder) Build() (*Schema, error) {
	b.applyExtensions()

	// Load stored ABIs
	if err := b.schema.loadStoredABIs(context.Background()); err != nil {
		b.schema.logger.Warn("failed to load stored ABIs", zap.Error(err))
		// Don't fail initialization, ABIs can be loaded later
	}

	// Create query type
	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: b.queries,
	})

	// Mutation and subscription types exist only with fields: a schema of
	// extensions alone (HandlerOptions.ExtensionsOnly) may have none.
	config := graphql.SchemaConfig{Query: queryType}
	if len(b.subscriptions) > 0 {
		config.Subscription = graphql.NewObject(graphql.ObjectConfig{Name: "Subscription", Fields: b.subscriptions})
	}
	if len(b.mutations) > 0 {
		config.Mutation = graphql.NewObject(graphql.ObjectConfig{Name: "Mutation", Fields: b.mutations})
	}
	schema, err := graphql.NewSchema(config)
	if err != nil {
		return nil, err
	}

	b.schema.schema = schema
	return b.schema, nil
}

// NewSchema creates a new GraphQL schema using the builder pattern
func NewSchema(store port.QueryStore, logger *zap.Logger) (*Schema, error) {
	return NewSchemaBuilder(store, logger).WithModules(nil).Build()
}

// Schema returns the GraphQL schema
func (s *Schema) Schema() graphql.Schema {
	return s.schema
}

// loadStoredABIs loads all ABIs from storage into the decoder
func (s *Schema) loadStoredABIs(ctx context.Context) error {
	addresses, err := s.storage.ListABIs(ctx)
	if err != nil {
		return fmt.Errorf("failed to list ABIs: %w", err)
	}

	loaded := 0
	for _, addr := range addresses {
		abiJSON, err := s.storage.GetABI(ctx, addr)
		if err != nil {
			s.logger.Warn("failed to get ABI",
				zap.String("address", addr.Hex()),
				zap.Error(err),
			)
			continue
		}

		// Load into decoder
		if err := s.abiDecoder.LoadABI(addr, "", string(abiJSON)); err != nil {
			s.logger.Warn("failed to load ABI into decoder",
				zap.String("address", addr.Hex()),
				zap.Error(err),
			)
			continue
		}

		loaded++
	}

	s.logger.Info("loaded ABIs from storage into GraphQL schema",
		zap.Int("total", len(addresses)),
		zap.Int("loaded", loaded),
	)

	return nil
}

// SetVerifier sets the contract verifier for the schema
func (s *Schema) SetVerifier(v verifier.Verifier) {
	s.verifier = v
}
