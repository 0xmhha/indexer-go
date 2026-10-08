package graphql

import (
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
)

// TestSchemaModules: modules over an optional service are left out without
// it, and a field two modules define is a wiring bug.
func TestSchemaModules(t *testing.T) {
	names := map[string]bool{}
	for _, m := range schemaModules {
		assert.False(t, names[m.name], "module %q listed twice", m.name)
		names[m.name] = true
	}

	base := NewSchemaBuilder(&mockStorage{}, zap.NewNop()).WithModules(nil)
	assert.Contains(t, base.queries, "block")
	assert.NotContains(t, base.queries, "rpcProxyMetrics", "the RPC proxy module needs a proxy")

	full := NewSchemaBuilder(&mockStorage{}, zap.NewNop()).WithModules(&HandlerOptions{
		RPCProxy:                    &rpcproxy.Proxy{},
		ContractRegistrationService: &events.ContractRegistrationService{},
	})
	assert.Contains(t, full.queries, "rpcProxyMetrics")
	assert.Greater(t, len(full.queries), len(base.queries))
	_, err := full.Build()
	require.NoError(t, err)

	b := NewSchemaBuilder(&mockStorage{}, zap.NewNop()).WithModules(nil)
	dup := schemaModule{name: "dup", build: func(b *SchemaBuilder, _ *HandlerOptions) {
		b.queries["block"] = &graphql.Field{Type: graphql.String}
	}}
	assert.PanicsWithValue(t, `graphql: query "block" defined twice`, func() { b.withModule(dup, nil) })
}
