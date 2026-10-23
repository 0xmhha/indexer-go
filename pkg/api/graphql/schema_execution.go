package graphql

import (
	"github.com/0xmhha/indexer-go/internal/api/executionstate"
	"github.com/graphql-go/graphql"
)

func executionStateType(name string, extra graphql.Fields) *graphql.Object {
	fields := graphql.Fields{}
	for _, field := range []string{"address", "height", "generation", "blockHash", "stateRoot"} {
		fields[field] = &graphql.Field{Type: graphql.NewNonNull(graphql.String)}
	}
	for key, value := range extra {
		fields[key] = value
	}
	return graphql.NewObject(graphql.ObjectConfig{Name: name, Fields: fields})
}

var executionAccountType = executionStateType("ExecutionAccount", graphql.Fields{
	"exists":      &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)},
	"nonce":       &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
	"balance":     &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
	"extra":       &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
	"codeHash":    &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
	"storageRoot": &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
})
var executionStorageType = executionStateType("ExecutionStorage", graphql.Fields{"slot": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, "value": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}})
var executionCodeType = executionStateType("ExecutionCode", graphql.Fields{"code": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}})

func (b *SchemaBuilder) WithExecutionQueries() *SchemaBuilder {
	for _, q := range []struct {
		name, kind string
		value      *graphql.Object
	}{{"executionAccount", "account", executionAccountType}, {"executionStorage", "storage", executionStorageType}, {"executionCode", "code", executionCodeType}} {
		args := graphql.FieldConfigArgument{"address": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)}}
		if q.kind == "storage" {
			args["slot"] = &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)}
		}
		b.queries[q.name] = &graphql.Field{Type: q.value, Args: args, Description: "Explicitly covered execution state at the public block; unknown coverage returns an error.", Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			address, _ := p.Args["address"].(string)
			slot, _ := p.Args["slot"].(string)
			return executionstate.View(p.Context, b.schema.storage, q.kind, address, slot)
		}}
	}
	return b
}
