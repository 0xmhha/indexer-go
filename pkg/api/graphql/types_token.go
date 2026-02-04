package graphql

import (
	"github.com/graphql-go/graphql"
)

// Token types (initialized in initTokenTypes)
var (
	tokenStandardEnumType      *graphql.Enum
	tokenMetadataType          *graphql.Object
	tokenMetadataConnectionType *graphql.Object
)

// initTokenMetadataTypes initializes token metadata types (for contract metadata, not transfers)
func initTokenMetadataTypes() {
	// Token standard enum type
	tokenStandardEnumType = graphql.NewEnum(graphql.EnumConfig{
		Name:        "TokenStandard",
		Description: "Token standard enum (ERC20, ERC721, ERC1155)",
		Values: graphql.EnumValueConfigMap{
			"UNKNOWN": &graphql.EnumValueConfig{
				Value:       "UNKNOWN",
				Description: "Unknown token standard",
			},
			"ERC20": &graphql.EnumValueConfig{
				Value:       "ERC20",
				Description: "ERC-20 fungible token",
			},
			"ERC721": &graphql.EnumValueConfig{
				Value:       "ERC721",
				Description: "ERC-721 non-fungible token",
			},
			"ERC1155": &graphql.EnumValueConfig{
				Value:       "ERC1155",
				Description: "ERC-1155 multi-token",
			},
		},
	})

	// Token metadata type
	tokenMetadataType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "TokenMetadata",
		Description: "Token metadata representing cached information about a token contract",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Token contract address",
			},
			"standard": &graphql.Field{
				Type:        graphql.NewNonNull(tokenStandardEnumType),
				Description: "Detected token standard",
			},
			"name": &graphql.Field{
				Type:        graphql.String,
				Description: "Token name (from name() function)",
			},
			"symbol": &graphql.Field{
				Type:        graphql.String,
				Description: "Token symbol (from symbol() function)",
			},
			"decimals": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Number of decimals (ERC20 only, 0 for NFTs)",
			},
			"totalSupply": &graphql.Field{
				Type:        bigIntType,
				Description: "Total supply (ERC20 only)",
			},
			"baseURI": &graphql.Field{
				Type:        graphql.String,
				Description: "Base URI for token metadata (ERC721/ERC1155)",
			},
			"detectedAt": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Block height when the token was first detected",
			},
			"createdAt": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Timestamp when metadata was created (RFC3339 format)",
			},
			"updatedAt": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Timestamp when metadata was last updated (RFC3339 format)",
			},
			"supportsERC165": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether the contract supports ERC-165 interface detection",
			},
			"supportsMetadata": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "Whether the contract supports metadata extension",
			},
			"supportsEnumerable": &graphql.Field{
				Type:        graphql.Boolean,
				Description: "Whether ERC721 contract supports enumerable extension",
			},
		},
	})

	// Token metadata connection type for pagination
	tokenMetadataConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "TokenMetadataConnection",
		Description: "Token metadata connection for pagination",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(tokenMetadataType))),
				Description: "List of token metadata",
			},
			"totalCount": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Int),
				Description: "Total count",
			},
			"pageInfo": &graphql.Field{
				Type:        graphql.NewNonNull(pageInfoType),
				Description: "Page info",
			},
		},
	})
}
