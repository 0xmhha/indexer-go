package graphql

import (
	"github.com/graphql-go/graphql"
)

// WithAddressIndexingQueries adds address indexing related queries (contract creation, token transfers)
func (b *SchemaBuilder) WithAddressIndexingQueries() *SchemaBuilder {
	s := b.schema

	b.queries["addressOverview"] = &graphql.Field{
		Type: graphql.NewNonNull(addressOverviewType),
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Resolve: s.resolveAddressOverview,
	}
	b.queries["contractCreation"] = &graphql.Field{
		Type: contractCreationType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
		},
		Resolve: s.resolveContractCreation,
	}
	b.queries["contracts"] = &graphql.Field{
		Type: contractCreationConnectionType,
		Args: graphql.FieldConfigArgument{
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveContracts,
	}
	b.queries["contractsByCreator"] = &graphql.Field{
		Type: contractCreationConnectionType,
		Args: graphql.FieldConfigArgument{
			"creator": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveContractsByCreator,
	}
	b.queries["internalTransactions"] = &graphql.Field{
		Type: graphql.NewList(graphql.NewNonNull(internalTransactionType)),
		Args: graphql.FieldConfigArgument{
			"transactionHash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
		},
		Resolve: s.resolveInternalTransactions,
	}
	b.queries["internalTransactionsByAddress"] = &graphql.Field{
		Type: internalTransactionConnectionType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"isFrom": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveInternalTransactionsByAddress,
	}
	b.queries["erc20Transfer"] = &graphql.Field{
		Type: erc20TransferType,
		Args: graphql.FieldConfigArgument{
			"transactionHash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
			"logIndex": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.Int),
			},
		},
		Resolve: s.resolveERC20Transfer,
	}
	b.queries["erc20TransfersByToken"] = &graphql.Field{
		Type: erc20TransferConnectionType,
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveERC20TransfersByToken,
	}
	b.queries["erc20TransfersByAddress"] = &graphql.Field{
		Type: erc20TransferConnectionType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"isFrom": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveERC20TransfersByAddress,
	}
	b.queries["erc721Transfer"] = &graphql.Field{
		Type: erc721TransferType,
		Args: graphql.FieldConfigArgument{
			"transactionHash": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(hashType),
			},
			"logIndex": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.Int),
			},
		},
		Resolve: s.resolveERC721Transfer,
	}
	b.queries["erc721TransfersByToken"] = &graphql.Field{
		Type: erc721TransferConnectionType,
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveERC721TransfersByToken,
	}
	b.queries["erc721TransfersByAddress"] = &graphql.Field{
		Type: erc721TransferConnectionType,
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"isFrom": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveERC721TransfersByAddress,
	}
	b.queries["erc721Owner"] = &graphql.Field{
		Type: addressType,
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"tokenId": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
		Resolve: s.resolveERC721Owner,
	}
	b.queries["nftsByOwner"] = &graphql.Field{
		Type:        graphql.NewNonNull(nftOwnershipConnectionType),
		Description: "Get all NFTs owned by a specific address",
		Args: graphql.FieldConfigArgument{
			"owner": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(addressType),
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveNFTsByOwner,
	}

	return b
}
