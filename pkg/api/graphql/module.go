package graphql

import (
	"github.com/graphql-go/graphql"
)

// The schema is built from modules: each module is one group of queries,
// mutations and subscriptions with their types, defined in its own
// schema_<module>.go file (refactoring plan R4-4). schemaModules lists them
// in build order; chain-specific groups are extensions (extension.go) and
// follow them. Two modules defining the same field is a wiring bug and
// panics, as it does for extensions.

// schemaModule is one group of the schema.
type schemaModule struct {
	name string
	// serves reports whether the module is part of a schema built with
	// opts (modules over an optional service); nil means always.
	serves func(opts *HandlerOptions) bool
	build  func(b *SchemaBuilder, opts *HandlerOptions)
}

// always adapts a builder method that needs no options.
func always(with func(*SchemaBuilder) *SchemaBuilder) func(*SchemaBuilder, *HandlerOptions) {
	return func(b *SchemaBuilder, _ *HandlerOptions) { with(b) }
}

var schemaModules = []schemaModule{
	{name: "core", build: always((*SchemaBuilder).WithCoreQueries)},
	{name: "historical", build: always((*SchemaBuilder).WithHistoricalQueries)},
	{name: "analytics", build: always((*SchemaBuilder).WithAnalyticsQueries)},
	{name: "address", build: always((*SchemaBuilder).WithAddressIndexingQueries)},
	{name: "setcode", build: always((*SchemaBuilder).WithSetCodeQueries)},
	{name: "modules", build: always((*SchemaBuilder).WithModuleQueries)},
	{name: "userops", build: always((*SchemaBuilder).WithUserOpQueries)},
	{name: "token.metadata", build: always((*SchemaBuilder).WithTokenMetadataQueries)},
	{name: "token.holders", build: always((*SchemaBuilder).WithTokenHolderQueries)},
	{name: "reorg", build: always((*SchemaBuilder).WithReorgQueries)},
	{name: "subscriptions", build: always((*SchemaBuilder).WithSubscriptions)},
	{name: "verification", build: always((*SchemaBuilder).WithMutations)},
	{
		name:   "rpcproxy",
		serves: func(o *HandlerOptions) bool { return o != nil && o.RPCProxy != nil },
		build: func(b *SchemaBuilder, o *HandlerOptions) {
			b.WithRPCProxy(o.RPCProxy).WithRPCProxyQueries()
		},
	},
	{
		name:   "notifications",
		serves: func(o *HandlerOptions) bool { return o != nil && o.NotificationService != nil },
		build: func(b *SchemaBuilder, o *HandlerOptions) {
			b.WithNotificationService(o.NotificationService).WithNotificationQueries()
		},
	},
	{
		name:   "contracts.dynamic",
		serves: func(o *HandlerOptions) bool { return o != nil && o.ContractRegistrationService != nil },
		build: func(b *SchemaBuilder, o *HandlerOptions) {
			b.WithContractRegistrationService(o.ContractRegistrationService).WithDynamicContractQueries()
		},
	},
}

// WithModules adds every module the options serve.
func (b *SchemaBuilder) WithModules(opts *HandlerOptions) *SchemaBuilder {
	for _, m := range schemaModules {
		if m.serves == nil || m.serves(opts) {
			b.withModule(m, opts)
		}
	}
	return b
}

// withModule builds a module into fields of its own and adds them to the
// schema's, so a field defined by two modules is caught.
func (b *SchemaBuilder) withModule(m schemaModule, opts *HandlerOptions) {
	queries, mutations, subscriptions := b.queries, b.mutations, b.subscriptions
	b.queries, b.mutations, b.subscriptions = graphql.Fields{}, graphql.Fields{}, graphql.Fields{}
	m.build(b, opts)
	for name, f := range b.queries {
		addField(queries, "query", name, f)
	}
	for name, f := range b.mutations {
		addField(mutations, "mutation", name, f)
	}
	for name, f := range b.subscriptions {
		addField(subscriptions, "subscription", name, f)
	}
	b.queries, b.mutations, b.subscriptions = queries, mutations, subscriptions
}
