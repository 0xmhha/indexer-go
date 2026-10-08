package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithReorgQueries adds queries for chain reorganizations and the blocks
// they removed. After a reorganization the node forgets the old branch;
// these queries keep it available so applications can correct what they
// showed.
func (b *SchemaBuilder) WithReorgQueries() *SchemaBuilder {
	s := b.schema
	b.queries["reorgs"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reorgType))),
		Description: "Chain reorganizations, newest first",
		Args: graphql.FieldConfigArgument{
			"limit":  &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 20},
			"offset": &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 0},
		},
		Resolve: s.resolveReorgs,
	}
	b.queries["reorg"] = &graphql.Field{
		Type:        reorgType,
		Description: "A chain reorganization by id",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(bigIntType)},
		},
		Resolve: s.resolveReorg,
	}
	b.queries["orphanedBlocks"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(orphanedBlockType))),
		Description: "Blocks removed by reorganizations at a height",
		Args: graphql.FieldConfigArgument{
			"number": &graphql.ArgumentConfig{Type: graphql.NewNonNull(bigIntType)},
		},
		Resolve: s.resolveOrphanedBlocks,
	}
	b.queries["orphanedBlock"] = &graphql.Field{
		Type:        orphanedBlockType,
		Description: "A block removed by a reorganization, by hash",
		Args: graphql.FieldConfigArgument{
			"hash": &graphql.ArgumentConfig{Type: graphql.NewNonNull(hashType)},
		},
		Resolve: s.resolveOrphanedBlock,
	}
	b.queries["orphanedTransaction"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(orphanedTransactionType))),
		Description: "A transaction as it was in removed blocks (more than one if it was orphaned again)",
		Args: graphql.FieldConfigArgument{
			"hash": &graphql.ArgumentConfig{Type: graphql.NewNonNull(hashType)},
		},
		Resolve: s.resolveOrphanedTransaction,
	}
	return b
}
