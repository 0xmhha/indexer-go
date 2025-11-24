package graphql

import (
	"encoding/json"
	"fmt"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveSearch handles the search query
func (s *Schema) resolveSearch(params graphql.ResolveParams) (interface{}, error) {
	ctx := params.Context

	// Extract query parameter
	query, ok := params.Args["query"].(string)
	if !ok || query == "" {
		return []interface{}{}, nil
	}

	// Extract optional types filter
	var resultTypes []string
	if typesArg, ok := params.Args["types"].([]interface{}); ok {
		resultTypes = make([]string, len(typesArg))
		for i, t := range typesArg {
			if typeStr, ok := t.(string); ok {
				resultTypes[i] = typeStr
			}
		}
	}

	// Extract limit (default: 10)
	limit := 10
	if limitArg, ok := params.Args["limit"].(int); ok && limitArg > 0 {
		limit = limitArg
	}

	// Cast storage to SearchReader
	searchReader, ok := s.storage.(storage.SearchReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support search queries")
	}

	// Perform search
	results, err := searchReader.Search(ctx, query, resultTypes, limit)
	if err != nil {
		s.logger.Error("failed to perform search",
			zap.String("query", query),
			zap.Strings("types", resultTypes),
			zap.Int("limit", limit),
			zap.Error(err))
		return nil, err
	}

	// Convert storage.SearchResult to GraphQL format
	graphqlResults := make([]map[string]interface{}, len(results))
	for i, result := range results {
		// Convert metadata map to JSON string
		metadataJSON := ""
		if result.Metadata != nil {
			metadataBytes, err := json.Marshal(result.Metadata)
			if err == nil {
				metadataJSON = string(metadataBytes)
			}
		}

		graphqlResults[i] = map[string]interface{}{
			"type":     result.Type,
			"value":    result.Value,
			"label":    result.Label,
			"metadata": metadataJSON,
		}
	}

	return graphqlResults, nil
}
