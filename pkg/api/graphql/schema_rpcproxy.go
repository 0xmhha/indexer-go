package graphql

import (
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
)

// WithRPCProxy sets the RPC proxy service for the schema
func (b *SchemaBuilder) WithRPCProxy(proxy *rpcproxy.Proxy) *SchemaBuilder {
	b.schema.rpcProxy = proxy
	return b
}

// WithRPCProxyQueries adds RPC proxy related queries (contractCall, transactionStatus, internalTransactionsRPC)
func (b *SchemaBuilder) WithRPCProxyQueries() *SchemaBuilder {
	builder := &schemaBuilder{
		schema:  b.schema,
		queries: b.queries,
	}
	builder.buildRPCProxyQueries()
	return b
}
