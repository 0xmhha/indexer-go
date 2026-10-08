package graphql

import (
	"errors"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/core/port"
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
		if err == port.ErrNotFound {
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
	var standard port.TokenStandard
	if standardArg, ok := p.Args["standard"].(string); ok && standardArg != "" {
		standard = port.TokenStandard(standardArg)
	}

	pagination := parseTokenPagination(p)

	// Get tokens
	tokens, next, err := s.storage.ListTokensByStandard(ctx, standard, pagination.page())
	if err != nil {
		if errors.Is(err, port.ErrInvalidCursor) {
			return nil, fmt.Errorf("invalid pagination cursor")
		}
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
		"pageInfo":   cursorPageInfo(pagination, next),
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

	var standard port.TokenStandard
	if standardArg, ok := p.Args["standard"].(string); ok && standardArg != "" {
		standard = port.TokenStandard(standardArg)
	}

	count, err := s.storage.GetTokensCount(ctx, standard)
	if err != nil {
		return nil, err
	}

	return count, nil
}

// mapTokenMetadata maps port.TokenMetadata to GraphQL response
func mapTokenMetadata(metadata *port.TokenMetadata) map[string]interface{} {
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

// ========== Token Holder Resolvers ==========

// resolveTokenHolders resolves token holders query
func (s *Schema) resolveTokenHolders(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	tokenHex, ok := p.Args["token"].(string)
	if !ok {
		return nil, fmt.Errorf("token address is required")
	}
	token := common.HexToAddress(tokenHex)

	pagination := parseTokenPagination(p)

	// Check if storage implements TokenHolderIndexReader
	holderReader, ok := s.storage.(port.TokenHolderIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support token holder queries")
	}

	// Get holders
	holders, next, err := holderReader.GetTokenHolders(ctx, token, pagination.page())
	if err != nil {
		if errors.Is(err, port.ErrInvalidCursor) {
			return nil, fmt.Errorf("invalid pagination cursor")
		}
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
		"pageInfo":   cursorPageInfo(pagination, next),
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
	holderReader, ok := s.storage.(port.TokenHolderIndexReader)
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
	holderReader, ok := s.storage.(port.TokenHolderIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support token holder queries")
	}

	balance, err := holderReader.GetTokenBalance(ctx, token, holder)
	if err != nil {
		if err == port.ErrNotFound {
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
	holderReader, ok := s.storage.(port.TokenHolderIndexReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support token holder queries")
	}

	stats, err := holderReader.GetTokenHolderStats(ctx, token)
	if err != nil {
		if err == port.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	return mapTokenHolderStats(stats), nil
}

// mapTokenHolder maps port.TokenHolder to GraphQL response
func mapTokenHolder(holder *port.TokenHolder) map[string]interface{} {
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

// mapTokenHolderStats maps port.TokenHolderStats to GraphQL response
func mapTokenHolderStats(stats *port.TokenHolderStats) map[string]interface{} {
	return map[string]interface{}{
		"tokenAddress":      stats.TokenAddress.Hex(),
		"holderCount":       stats.HolderCount,
		"transferCount":     stats.TransferCount,
		"lastActivityBlock": fmt.Sprintf("%d", stats.LastActivityAt),
	}
}

// parseTokenPagination reads the pagination argument of the token lists:
// limit defaults to 20 and is passed on as given (0 lists every item).
func parseTokenPagination(p graphql.ResolveParams) PaginationParams {
	params := PaginationParams{Limit: 20}
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok {
			params.Limit = l
		}
		if o, ok := pagination["offset"].(int); ok {
			params.Offset = o
		}
		if a, ok := pagination["after"].(string); ok {
			params.After = a
		}
	}
	return params
}
