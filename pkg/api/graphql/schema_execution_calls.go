package graphql

import (
	"github.com/0xmhha/indexer-go/internal/api/executioncalls"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/graphql-go/graphql"
)

func (b *SchemaBuilder) withExecutionCallQueries() {
	coverage := graphql.NewObject(graphql.ObjectConfig{Name: "ExecutionCallCoverage", Fields: graphql.Fields{
		"height": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, "epoch": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, "generation": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, "blockHash": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, "digest": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, "profile": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, "collected": &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)},
	}})
	missing := graphql.NewObject(graphql.ObjectConfig{Name: "ExecutionCallMissingRange", Fields: graphql.Fields{"fromBlock": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, "toBlock": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}}})
	fields := graphql.Fields{"coverage": &graphql.Field{Type: graphql.NewNonNull(coverage)}, "path": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(graphql.Int)))}}
	for _, name := range []string{"verification", "type", "from", "transactionHash", "gas", "gasUsed", "input", "output", "error"} {
		fields[name] = &graphql.Field{Type: graphql.NewNonNull(graphql.String)}
	}
	for _, name := range []string{"to", "value"} {
		fields[name] = &graphql.Field{Type: graphql.String}
	}
	for _, name := range []string{"ordinal", "parent", "transactionIndex"} {
		fields[name] = &graphql.Field{Type: graphql.NewNonNull(graphql.Int)}
	}
	for _, name := range []string{"internal", "reverted", "ancestorReverted"} {
		fields[name] = &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)}
	}
	call := graphql.NewObject(graphql.ObjectConfig{Name: "ExecutionRangeCall", Fields: fields})
	page := graphql.NewObject(graphql.ObjectConfig{Name: "ExecutionCallRangePage", Fields: graphql.Fields{
		"selection": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, "epoch": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, "complete": &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)}, "next": &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, "missing": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(missing)))}, "coverage": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(coverage)))}, "calls": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(call)))},
	}})
	b.queries["executionCalls"] = &graphql.Field{Type: page, Description: "Bounded DB-only address calls; strict collection coverage by default. Orphans require selection orphan and original public orphanEpoch.", Args: graphql.FieldConfigArgument{
		"address": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)}, "fromBlock": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)}, "toBlock": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)}, "limit": &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 50}, "selection": &graphql.ArgumentConfig{Type: graphql.String, DefaultValue: "canonical"}, "orphanEpoch": &graphql.ArgumentConfig{Type: graphql.String}, "partial": &graphql.ArgumentConfig{Type: graphql.Boolean, DefaultValue: false}, "cursor": &graphql.ArgumentConfig{Type: graphql.String},
	}, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
		str := func(name string) string { v, _ := p.Args[name].(string); return v }
		limit, _ := p.Args["limit"].(int)
		if limit <= 0 || limit > 256 {
			return nil, executioncalls.Error(port.ErrInvalidData)
		}
		partial, _ := p.Args["partial"].(bool)
		return executioncalls.View(p.Context, b.schema.storage, executioncalls.Params{Address: str("address"), FromBlock: str("fromBlock"), ToBlock: str("toBlock"), Limit: uint32(limit), Selection: str("selection"), OrphanEpoch: str("orphanEpoch"), Partial: partial, Cursor: str("cursor")})
	}}
}
