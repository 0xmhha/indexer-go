package api

import (
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/ethereum/go-ethereum/common"
	gql "github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveFeeDelegationStats handles the feeDelegationStats query
func (s *Schema) resolveFeeDelegationStats(p gql.ResolveParams) (interface{}, error) {
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

	// Convert fromTime/toTime to block numbers (overrides fromBlock/toBlock)
	if histStorage := s.history; histStorage != nil {
		if fromTimeArg, ok := p.Args["fromTime"].(string); ok && fromTimeArg != "" {
			if ft, success := new(big.Int).SetString(fromTimeArg, 10); success {
				block, err := histStorage.GetBlockByTimestamp(ctx, ft.Uint64())
				if err == nil && block != nil {
					fromBlock = block.Number
				}
			}
		}
		if toTimeArg, ok := p.Args["toTime"].(string); ok && toTimeArg != "" {
			if tt, success := new(big.Int).SetString(toTimeArg, 10); success {
				block, err := histStorage.GetBlockByTimestamp(ctx, tt.Uint64())
				if err == nil && block != nil {
					toBlock = block.Number
				}
			}
		}
	}

	// Fee delegation statistics
	fdReader := s.stats
	if fdReader == nil {
		return nil, fmt.Errorf("storage does not support fee delegation statistics")
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
func (s *Schema) resolveTopFeePayers(p gql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse parameters
	limit := constants.DefaultPaginationLimit
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

	// Convert fromTime/toTime to block numbers (overrides fromBlock/toBlock)
	if histStorage := s.history; histStorage != nil {
		if fromTimeArg, ok := p.Args["fromTime"].(string); ok && fromTimeArg != "" {
			if ft, success := new(big.Int).SetString(fromTimeArg, 10); success {
				block, err := histStorage.GetBlockByTimestamp(ctx, ft.Uint64())
				if err == nil && block != nil {
					fromBlock = block.Number
				}
			}
		}
		if toTimeArg, ok := p.Args["toTime"].(string); ok && toTimeArg != "" {
			if tt, success := new(big.Int).SetString(toTimeArg, 10); success {
				block, err := histStorage.GetBlockByTimestamp(ctx, tt.Uint64())
				if err == nil && block != nil {
					toBlock = block.Number
				}
			}
		}
	}

	// Fee delegation statistics
	fdReader := s.stats
	if fdReader == nil {
		return nil, fmt.Errorf("storage does not support fee delegation statistics")
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
func (s *Schema) resolveFeePayerStats(p gql.ResolveParams) (interface{}, error) {
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

	// Convert fromTime/toTime to block numbers (overrides fromBlock/toBlock)
	if histStorage := s.history; histStorage != nil {
		if fromTimeArg, ok := p.Args["fromTime"].(string); ok && fromTimeArg != "" {
			if ft, success := new(big.Int).SetString(fromTimeArg, 10); success {
				block, err := histStorage.GetBlockByTimestamp(ctx, ft.Uint64())
				if err == nil && block != nil {
					fromBlock = block.Number
				}
			}
		}
		if toTimeArg, ok := p.Args["toTime"].(string); ok && toTimeArg != "" {
			if tt, success := new(big.Int).SetString(toTimeArg, 10); success {
				block, err := histStorage.GetBlockByTimestamp(ctx, tt.Uint64())
				if err == nil && block != nil {
					toBlock = block.Number
				}
			}
		}
	}

	// Fee delegation statistics
	fdReader := s.stats
	if fdReader == nil {
		return nil, fmt.Errorf("storage does not support fee delegation statistics")
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

// addQueries adds the fee delegation queries.
func addQueries(e *graphql.Extension, s *Schema) {
	// FeeDelegationStats type
	feeDelegationStatsType := gql.NewObject(gql.ObjectConfig{
		Name:        "FeeDelegationStats",
		Description: "Overall fee delegation statistics",
		Fields: gql.Fields{
			"totalFeeDelegatedTxs": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Total number of fee delegation transactions",
			},
			"totalFeesSaved": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Total fees saved by users (paid by fee payers) in wei",
			},
			"adoptionRate": &gql.Field{
				Type:        gql.NewNonNull(gql.Float),
				Description: "Percentage of fee delegation transactions vs total transactions",
			},
			"avgFeeSaved": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Average fee saved per fee delegation transaction in wei",
			},
		},
	})

	// FeePayerStats type
	feePayerStatsType := gql.NewObject(gql.ObjectConfig{
		Name:        "FeePayerStats",
		Description: "Statistics for a single fee payer",
		Fields: gql.Fields{
			"address": &gql.Field{
				Type:        gql.NewNonNull(graphql.AddressType),
				Description: "Fee payer address",
			},
			"txCount": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Number of transactions sponsored by this fee payer",
			},
			"totalFeesPaid": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Total fees paid by this fee payer in wei",
			},
			"percentage": &gql.Field{
				Type:        gql.NewNonNull(gql.Float),
				Description: "Percentage of total fee delegation transactions",
			},
		},
	})

	// TopFeePayersResult type
	topFeePayersResultType := gql.NewObject(gql.ObjectConfig{
		Name:        "TopFeePayersResult",
		Description: "Top fee payers result with pagination info",
		Fields: gql.Fields{
			"nodes": &gql.Field{
				Type:        gql.NewNonNull(gql.NewList(gql.NewNonNull(feePayerStatsType))),
				Description: "List of fee payer statistics",
			},
			"totalCount": &gql.Field{
				Type:        gql.NewNonNull(graphql.BigIntType),
				Description: "Total count of unique fee payers",
			},
		},
	})

	// Add queries
	e.AddQuery("feeDelegationStats", &gql.Field{
		Type:        feeDelegationStatsType,
		Description: "Get overall fee delegation statistics",
		Args: gql.FieldConfigArgument{
			"fromBlock": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "Starting block number (optional)",
			},
			"toBlock": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "Ending block number (optional)",
			},
			"fromTime": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "Start time filter (Unix timestamp, overrides fromBlock)",
			},
			"toTime": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "End time filter (Unix timestamp, overrides toBlock)",
			},
		},
		Resolve: s.resolveFeeDelegationStats,
	})

	e.AddQuery("topFeePayers", &gql.Field{
		Type:        topFeePayersResultType,
		Description: "Get top fee payers by transaction count",
		Args: gql.FieldConfigArgument{
			"limit": &gql.ArgumentConfig{
				Type:         gql.Int,
				DefaultValue: 10,
				Description:  "Maximum number of fee payers to return (default: 10)",
			},
			"fromBlock": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "Starting block number (optional)",
			},
			"toBlock": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "Ending block number (optional)",
			},
			"fromTime": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "Start time filter (Unix timestamp, overrides fromBlock)",
			},
			"toTime": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "End time filter (Unix timestamp, overrides toBlock)",
			},
		},
		Resolve: s.resolveTopFeePayers,
	})

	e.AddQuery("feePayerStats", &gql.Field{
		Type:        feePayerStatsType,
		Description: "Get statistics for a specific fee payer",
		Args: gql.FieldConfigArgument{
			"address": &gql.ArgumentConfig{
				Type:        gql.NewNonNull(graphql.AddressType),
				Description: "Fee payer address",
			},
			"fromBlock": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "Starting block number (optional)",
			},
			"toBlock": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "Ending block number (optional)",
			},
			"fromTime": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "Start time filter (Unix timestamp, overrides fromBlock)",
			},
			"toTime": &gql.ArgumentConfig{
				Type:        graphql.BigIntType,
				Description: "End time filter (Unix timestamp, overrides toBlock)",
			},
		},
		Resolve: s.resolveFeePayerStats,
	})
}
