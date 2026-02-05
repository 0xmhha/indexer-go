package graphql

import (
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
)

// resolveTokenMetadata resolves a token metadata query by address
func (s *Schema) resolveTokenMetadata(p graphql.ResolveParams) (interface{}, error) {
	addressHex, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("address is required")
	}

	address := common.HexToAddress(addressHex)
	ctx := p.Context

	metadata, err := s.storage.GetTokenMetadata(ctx, address)
	if err != nil {
		if err == storage.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	return mapTokenMetadata(metadata), nil
}

// resolveTokens resolves a token list query with optional standard filter
func (s *Schema) resolveTokens(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse standard filter
	var standard storage.TokenStandard
	if standardArg, ok := p.Args["standard"].(string); ok && standardArg != "" {
		standard = storage.TokenStandard(standardArg)
	}

	// Parse pagination
	limit := 20
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok {
			limit = l
		}
		if o, ok := pagination["offset"].(int); ok {
			offset = o
		}
	}

	// Get tokens
	tokens, err := s.storage.ListTokensByStandard(ctx, standard, limit, offset)
	if err != nil {
		return nil, err
	}

	// Get total count
	totalCount, err := s.storage.GetTokensCount(ctx, standard)
	if err != nil {
		return nil, err
	}

	// Map to GraphQL types
	nodes := make([]map[string]interface{}, 0, len(tokens))
	for _, token := range tokens {
		nodes = append(nodes, mapTokenMetadata(token))
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     offset+len(tokens) < totalCount,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveSearchTokens resolves a token search query
func (s *Schema) resolveSearchTokens(p graphql.ResolveParams) (interface{}, error) {
	query, ok := p.Args["query"].(string)
	if !ok || query == "" {
		return []interface{}{}, nil
	}

	limit := 10
	if l, ok := p.Args["limit"].(int); ok {
		limit = l
	}

	ctx := p.Context

	tokens, err := s.storage.SearchTokens(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(tokens))
	for _, token := range tokens {
		result = append(result, mapTokenMetadata(token))
	}

	return result, nil
}

// resolveTokenCount resolves a token count query
func (s *Schema) resolveTokenCount(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	var standard storage.TokenStandard
	if standardArg, ok := p.Args["standard"].(string); ok && standardArg != "" {
		standard = storage.TokenStandard(standardArg)
	}

	count, err := s.storage.GetTokensCount(ctx, standard)
	if err != nil {
		return nil, err
	}

	return count, nil
}

// mapTokenMetadata maps storage.TokenMetadata to GraphQL response
func mapTokenMetadata(metadata *storage.TokenMetadata) map[string]interface{} {
	result := map[string]interface{}{
		"address":            metadata.Address.Hex(),
		"standard":           string(metadata.Standard),
		"name":               metadata.Name,
		"symbol":             metadata.Symbol,
		"decimals":           int(metadata.Decimals),
		"detectedAt":         fmt.Sprintf("%d", metadata.DetectedAt),
		"createdAt":          metadata.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"updatedAt":          metadata.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"supportsERC165":     metadata.SupportsERC165,
		"supportsMetadata":   metadata.SupportsMetadata,
		"supportsEnumerable": metadata.SupportsEnumerable,
	}

	// Add optional fields
	if metadata.TotalSupply != nil {
		result["totalSupply"] = metadata.TotalSupply.String()
	}
	if metadata.BaseURI != "" {
		result["baseURI"] = metadata.BaseURI
	}

	return result
}

// WithTokenMetadataQueries adds token metadata queries to the schema builder
func (b *SchemaBuilder) WithTokenMetadataQueries() *SchemaBuilder {
	s := b.schema

	b.queries["tokenMetadata"] = &graphql.Field{
		Type:        tokenMetadataType,
		Description: "Get token metadata by contract address",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Token contract address",
			},
		},
		Resolve: s.resolveTokenMetadata,
	}

	b.queries["tokens"] = &graphql.Field{
		Type:        graphql.NewNonNull(tokenMetadataConnectionType),
		Description: "List tokens with optional standard filter and pagination",
		Args: graphql.FieldConfigArgument{
			"standard": &graphql.ArgumentConfig{
				Type:        tokenStandardEnumType,
				Description: "Filter by token standard (ERC20, ERC721, ERC1155)",
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveTokens,
	}

	b.queries["searchTokens"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(tokenMetadataType))),
		Description: "Search tokens by name or symbol",
		Args: graphql.FieldConfigArgument{
			"query": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Search query (matches name or symbol prefix)",
			},
			"limit": &graphql.ArgumentConfig{
				Type:        graphql.Int,
				Description: "Maximum number of results (default: 10)",
			},
		},
		Resolve: s.resolveSearchTokens,
	}

	b.queries["tokenCount"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Int),
		Description: "Get token count by standard",
		Args: graphql.FieldConfigArgument{
			"standard": &graphql.ArgumentConfig{
				Type:        tokenStandardEnumType,
				Description: "Filter by token standard (optional)",
			},
		},
		Resolve: s.resolveTokenCount,
	}

	return b
}

// ========== Token Holder Resolvers ==========

// resolveTokenHolders resolves token holders query
func (s *Schema) resolveTokenHolders(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	tokenHex, ok := p.Args["token"].(string)
	if !ok {
		return nil, fmt.Errorf("token address is required")
	}
	token := common.HexToAddress(tokenHex)

	// Parse pagination
	limit := 20
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok {
			limit = l
		}
		if o, ok := pagination["offset"].(int); ok {
			offset = o
		}
	}

	// Check if storage implements TokenHolderIndexReader
	holderReader, ok := s.storage.(storage.TokenHolderIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support token holder queries")
	}

	// Get holders
	holders, err := holderReader.GetTokenHolders(ctx, token, limit, offset)
	if err != nil {
		return nil, err
	}

	// Get total count
	totalCount, err := holderReader.GetTokenHolderCount(ctx, token)
	if err != nil {
		totalCount = len(holders)
	}

	// Map to GraphQL types
	nodes := make([]map[string]interface{}, 0, len(holders))
	for _, holder := range holders {
		nodes = append(nodes, mapTokenHolder(holder))
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     offset+len(holders) < totalCount,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveTokenHolderCount resolves token holder count query
func (s *Schema) resolveTokenHolderCount(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	tokenHex, ok := p.Args["token"].(string)
	if !ok {
		return nil, fmt.Errorf("token address is required")
	}
	token := common.HexToAddress(tokenHex)

	// Check if storage implements TokenHolderIndexReader
	holderReader, ok := s.storage.(storage.TokenHolderIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support token holder queries")
	}

	count, err := holderReader.GetTokenHolderCount(ctx, token)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// resolveTokenBalance resolves token balance for a specific holder
func (s *Schema) resolveTokenBalance(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	tokenHex, ok := p.Args["token"].(string)
	if !ok {
		return nil, fmt.Errorf("token address is required")
	}
	token := common.HexToAddress(tokenHex)

	holderHex, ok := p.Args["holder"].(string)
	if !ok {
		return nil, fmt.Errorf("holder address is required")
	}
	holder := common.HexToAddress(holderHex)

	// Check if storage implements TokenHolderIndexReader
	holderReader, ok := s.storage.(storage.TokenHolderIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support token holder queries")
	}

	balance, err := holderReader.GetTokenBalance(ctx, token, holder)
	if err != nil {
		if err == storage.ErrNotFound {
			return "0", nil
		}
		return nil, err
	}

	return balance.String(), nil
}

// resolveTokenHolderStats resolves token holder stats
func (s *Schema) resolveTokenHolderStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	tokenHex, ok := p.Args["token"].(string)
	if !ok {
		return nil, fmt.Errorf("token address is required")
	}
	token := common.HexToAddress(tokenHex)

	// Check if storage implements TokenHolderIndexReader
	holderReader, ok := s.storage.(storage.TokenHolderIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support token holder queries")
	}

	stats, err := holderReader.GetTokenHolderStats(ctx, token)
	if err != nil {
		if err == storage.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	return mapTokenHolderStats(stats), nil
}

// mapTokenHolder maps storage.TokenHolder to GraphQL response
func mapTokenHolder(holder *storage.TokenHolder) map[string]interface{} {
	balance := "0"
	if holder.Balance != nil {
		balance = holder.Balance.String()
	}
	return map[string]interface{}{
		"tokenAddress":     holder.TokenAddress.Hex(),
		"holderAddress":    holder.HolderAddress.Hex(),
		"balance":          balance,
		"lastUpdatedBlock": fmt.Sprintf("%d", holder.LastUpdatedAt),
	}
}

// mapTokenHolderStats maps storage.TokenHolderStats to GraphQL response
func mapTokenHolderStats(stats *storage.TokenHolderStats) map[string]interface{} {
	return map[string]interface{}{
		"tokenAddress":      stats.TokenAddress.Hex(),
		"holderCount":       stats.HolderCount,
		"transferCount":     stats.TransferCount,
		"lastActivityBlock": fmt.Sprintf("%d", stats.LastActivityAt),
	}
}

// WithTokenHolderQueries adds token holder queries to the schema builder
func (b *SchemaBuilder) WithTokenHolderQueries() *SchemaBuilder {
	s := b.schema

	b.queries["tokenHolders"] = &graphql.Field{
		Type:        graphql.NewNonNull(tokenHolderConnectionType),
		Description: "Get token holders sorted by balance (descending)",
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Token contract address",
			},
			"pagination": &graphql.ArgumentConfig{
				Type: paginationInputType,
			},
		},
		Resolve: s.resolveTokenHolders,
	}

	b.queries["tokenHolderCount"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Int),
		Description: "Get the number of unique holders for a token",
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Token contract address",
			},
		},
		Resolve: s.resolveTokenHolderCount,
	}

	b.queries["tokenBalance"] = &graphql.Field{
		Type:        graphql.NewNonNull(bigIntType),
		Description: "Get the balance of a specific holder for a token",
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Token contract address",
			},
			"holder": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Holder address",
			},
		},
		Resolve: s.resolveTokenBalance,
	}

	b.queries["tokenHolderStats"] = &graphql.Field{
		Type:        tokenHolderStatsType,
		Description: "Get aggregate statistics for a token",
		Args: graphql.FieldConfigArgument{
			"token": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Token contract address",
			},
		},
		Resolve: s.resolveTokenHolderStats,
	}

	return b
}
