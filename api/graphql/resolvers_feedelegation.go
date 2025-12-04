package graphql

import (
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveFeeDelegationStats handles the feeDelegationStats query
func (s *Schema) resolveFeeDelegationStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse optional block range parameters
	var fromBlock, toBlock uint64

	if fromBlockArg, ok := p.Args["fromBlock"].(string); ok && fromBlockArg != "" {
		if fb, success := new(big.Int).SetString(fromBlockArg, 10); success {
			fromBlock = fb.Uint64()
		}
	}

	if toBlockArg, ok := p.Args["toBlock"].(string); ok && toBlockArg != "" {
		if tb, success := new(big.Int).SetString(toBlockArg, 10); success {
			toBlock = tb.Uint64()
		}
	}

	// Cast storage to FeeDelegationReader
	fdReader, ok := s.storage.(storage.FeeDelegationReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement FeeDelegationReader")
	}

	// Get fee delegation stats
	stats, err := fdReader.GetFeeDelegationStats(ctx, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get fee delegation stats",
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get fee delegation stats: %w", err)
	}

	return map[string]interface{}{
		"totalFeeDelegatedTxs": fmt.Sprintf("%d", stats.TotalFeeDelegatedTxs),
		"totalFeesSaved":       stats.TotalFeesSaved.String(),
		"adoptionRate":         stats.AdoptionRate,
		"avgFeeSaved":          stats.AvgFeeSaved.String(),
	}, nil
}

// resolveTopFeePayers handles the topFeePayers query
func (s *Schema) resolveTopFeePayers(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse parameters
	limit := 10
	if limitArg, ok := p.Args["limit"].(int); ok && limitArg > 0 {
		limit = limitArg
	}

	var fromBlock, toBlock uint64

	if fromBlockArg, ok := p.Args["fromBlock"].(string); ok && fromBlockArg != "" {
		if fb, success := new(big.Int).SetString(fromBlockArg, 10); success {
			fromBlock = fb.Uint64()
		}
	}

	if toBlockArg, ok := p.Args["toBlock"].(string); ok && toBlockArg != "" {
		if tb, success := new(big.Int).SetString(toBlockArg, 10); success {
			toBlock = tb.Uint64()
		}
	}

	// Cast storage to FeeDelegationReader
	fdReader, ok := s.storage.(storage.FeeDelegationReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement FeeDelegationReader")
	}

	// Get top fee payers
	feePayers, totalCount, err := fdReader.GetTopFeePayers(ctx, limit, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get top fee payers",
			zap.Int("limit", limit),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get top fee payers: %w", err)
	}

	// Convert to GraphQL format
	nodes := make([]interface{}, len(feePayers))
	for i, fp := range feePayers {
		nodes[i] = map[string]interface{}{
			"address":       fp.Address.Hex(),
			"txCount":       fmt.Sprintf("%d", fp.TxCount),
			"totalFeesPaid": fp.TotalFeesPaid.String(),
			"percentage":    fp.Percentage,
		}
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": fmt.Sprintf("%d", totalCount),
	}, nil
}

// resolveFeePayerStats handles the feePayerStats query
func (s *Schema) resolveFeePayerStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse address parameter
	addressStr, ok := p.Args["address"].(string)
	if !ok || addressStr == "" {
		return nil, fmt.Errorf("address is required")
	}
	address := common.HexToAddress(addressStr)

	// Parse optional block range parameters
	var fromBlock, toBlock uint64

	if fromBlockArg, ok := p.Args["fromBlock"].(string); ok && fromBlockArg != "" {
		if fb, success := new(big.Int).SetString(fromBlockArg, 10); success {
			fromBlock = fb.Uint64()
		}
	}

	if toBlockArg, ok := p.Args["toBlock"].(string); ok && toBlockArg != "" {
		if tb, success := new(big.Int).SetString(toBlockArg, 10); success {
			toBlock = tb.Uint64()
		}
	}

	// Cast storage to FeeDelegationReader
	fdReader, ok := s.storage.(storage.FeeDelegationReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement FeeDelegationReader")
	}

	// Get fee payer stats
	stats, err := fdReader.GetFeePayerStats(ctx, address, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get fee payer stats",
			zap.String("address", addressStr),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get fee payer stats: %w", err)
	}

	return map[string]interface{}{
		"address":       stats.Address.Hex(),
		"txCount":       fmt.Sprintf("%d", stats.TxCount),
		"totalFeesPaid": stats.TotalFeesPaid.String(),
		"percentage":    stats.Percentage,
	}, nil
}

// buildFeeDelegationQueries builds the fee delegation related GraphQL queries
func (b *schemaBuilder) buildFeeDelegationQueries() {
	// FeeDelegationStats type
	feeDelegationStatsType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "FeeDelegationStats",
		Description: "Overall fee delegation statistics",
		Fields: graphql.Fields{
			"totalFeeDelegatedTxs": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total number of fee delegation transactions",
			},
			"totalFeesSaved": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total fees saved by users (paid by fee payers) in wei",
			},
			"adoptionRate": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Float),
				Description: "Percentage of fee delegation transactions vs total transactions",
			},
			"avgFeeSaved": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Average fee saved per fee delegation transaction in wei",
			},
		},
	})

	// FeePayerStats type
	feePayerStatsType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "FeePayerStats",
		Description: "Statistics for a single fee payer",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type:        graphql.NewNonNull(addressType),
				Description: "Fee payer address",
			},
			"txCount": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Number of transactions sponsored by this fee payer",
			},
			"totalFeesPaid": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total fees paid by this fee payer in wei",
			},
			"percentage": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Float),
				Description: "Percentage of total fee delegation transactions",
			},
		},
	})

	// TopFeePayersResult type
	topFeePayersResultType := graphql.NewObject(graphql.ObjectConfig{
		Name:        "TopFeePayersResult",
		Description: "Top fee payers result with pagination info",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(feePayerStatsType))),
				Description: "List of fee payer statistics",
			},
			"totalCount": &graphql.Field{
				Type:        graphql.NewNonNull(bigIntType),
				Description: "Total count of unique fee payers",
			},
		},
	})

	// Add queries
	b.queries["feeDelegationStats"] = &graphql.Field{
		Type:        feeDelegationStatsType,
		Description: "Get overall fee delegation statistics",
		Args: graphql.FieldConfigArgument{
			"fromBlock": &graphql.ArgumentConfig{
				Type:        bigIntType,
				Description: "Starting block number (optional)",
			},
			"toBlock": &graphql.ArgumentConfig{
				Type:        bigIntType,
				Description: "Ending block number (optional)",
			},
		},
		Resolve: b.schema.resolveFeeDelegationStats,
	}

	b.queries["topFeePayers"] = &graphql.Field{
		Type:        topFeePayersResultType,
		Description: "Get top fee payers by transaction count",
		Args: graphql.FieldConfigArgument{
			"limit": &graphql.ArgumentConfig{
				Type:         graphql.Int,
				DefaultValue: 10,
				Description:  "Maximum number of fee payers to return (default: 10)",
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type:        bigIntType,
				Description: "Starting block number (optional)",
			},
			"toBlock": &graphql.ArgumentConfig{
				Type:        bigIntType,
				Description: "Ending block number (optional)",
			},
		},
		Resolve: b.schema.resolveTopFeePayers,
	}

	b.queries["feePayerStats"] = &graphql.Field{
		Type:        feePayerStatsType,
		Description: "Get statistics for a specific fee payer",
		Args: graphql.FieldConfigArgument{
			"address": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(addressType),
				Description: "Fee payer address",
			},
			"fromBlock": &graphql.ArgumentConfig{
				Type:        bigIntType,
				Description: "Starting block number (optional)",
			},
			"toBlock": &graphql.ArgumentConfig{
				Type:        bigIntType,
				Description: "Ending block number (optional)",
			},
		},
		Resolve: b.schema.resolveFeePayerStats,
	}
}
