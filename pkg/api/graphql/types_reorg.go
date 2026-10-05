package graphql

import (
	"github.com/graphql-go/graphql"
)

// Types for chain reorganizations and the blocks they removed (orphans).
var (
	blockRefType            *graphql.Object
	reorgType               *graphql.Object
	orphanedBlockType       *graphql.Object
	orphanedTransactionType *graphql.Object
)

func initReorgTypes() {
	blockRefType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "BlockRef",
		Description: "A block identified by number and hash",
		Fields: graphql.Fields{
			"number": &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"hash":   &graphql.Field{Type: graphql.NewNonNull(hashType)},
		},
	})

	reorgType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "Reorg",
		Description: "A chain reorganization the indexer rolled back",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Sequence number, increasing in the order reorganizations happened",
			},
			"forkNumber": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Newest block both branches share",
			},
			"forkHash": &graphql.Field{Type: graphql.NewNonNull(hashType)},
			"oldHead": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Indexed height before the rollback",
			},
			"depth": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of blocks rolled back",
			},
			"removedBlocks": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(blockRefType))),
				Description: "Rolled-back blocks, newest first; query them with orphanedBlock",
			},
			"detectedAt": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Unix time the rollback happened",
			},
		},
	})

	orphanedBlockType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "OrphanedBlock",
		Description: "A block removed from the canonical chain by a reorganization, as it was indexed",
		Fields: graphql.Fields{
			"block": &graphql.Field{Type: graphql.NewNonNull(blockType)},
			"receipts": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(receiptType))),
				Description: "Receipts in transaction order",
			},
			"reorg": &graphql.Field{
				Type:        reorgType,
				Description: "The reorganization that removed the block",
			},
		},
	})

	orphanedTransactionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "OrphanedTransaction",
		Description: "A transaction of a removed block",
		Fields: graphql.Fields{
			"transaction": &graphql.Field{Type: graphql.NewNonNull(transactionType)},
			"receipt":     &graphql.Field{Type: receiptType},
			"block": &graphql.Field{
				Type:        graphql.NewNonNull(blockRefType),
				Description: "The removed block that held it",
			},
			"reorgId": &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
			"reincludedIn": &graphql.Field{
				Type:        blockRefType,
				Description: "The canonical block holding the transaction now, if it was included again",
			},
		},
	})
}
