package graphql

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveWBFTBlockExtra resolves WBFT consensus metadata for a block by number
func (s *Schema) resolveWBFTBlockExtra(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	numberStr, ok := p.Args["blockNumber"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid block number")
	}

	number, err := strconv.ParseUint(numberStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid block number format: %w", err)
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := s.storage.(storage.WBFTReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support WBFT metadata")
	}

	extra, err := wbftReader.GetWBFTBlockExtra(ctx, number)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get WBFT block extra",
			zap.Uint64("blockNumber", number),
			zap.Error(err))
		return nil, err
	}

	return s.wbftBlockExtraToMap(extra), nil
}

// resolveWBFTBlockExtraByHash resolves WBFT consensus metadata for a block by hash
func (s *Schema) resolveWBFTBlockExtraByHash(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	hashStr, ok := p.Args["blockHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid block hash")
	}

	hash := common.HexToHash(hashStr)

	// Check if storage implements WBFTReader
	wbftReader, ok := s.storage.(storage.WBFTReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support WBFT metadata")
	}

	extra, err := wbftReader.GetWBFTBlockExtraByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get WBFT block extra by hash",
			zap.String("blockHash", hashStr),
			zap.Error(err))
		return nil, err
	}

	return s.wbftBlockExtraToMap(extra), nil
}

// resolveEpochInfo resolves epoch information for a specific epoch
func (s *Schema) resolveEpochInfo(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	epochNumberStr, ok := p.Args["epochNumber"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid epoch number")
	}

	epochNumber, err := strconv.ParseUint(epochNumberStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid epoch number format: %w", err)
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := s.storage.(storage.WBFTReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support WBFT metadata")
	}

	epochInfo, err := wbftReader.GetEpochInfo(ctx, epochNumber)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get epoch info",
			zap.Uint64("epochNumber", epochNumber),
			zap.Error(err))
		return nil, err
	}

	return s.epochInfoToMap(epochInfo), nil
}

// resolveLatestEpochInfo resolves the most recent epoch information
func (s *Schema) resolveLatestEpochInfo(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Check if storage implements WBFTReader
	wbftReader, ok := s.storage.(storage.WBFTReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support WBFT metadata")
	}

	epochInfo, err := wbftReader.GetLatestEpochInfo(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get latest epoch info",
			zap.Error(err))
		return nil, err
	}

	return s.epochInfoToMap(epochInfo), nil
}

// resolveValidatorSigningStats resolves signing statistics for a specific validator
func (s *Schema) resolveValidatorSigningStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	validatorAddrStr, ok := p.Args["validatorAddress"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid validator address")
	}

	fromBlockStr, ok := p.Args["fromBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid fromBlock")
	}

	toBlockStr, ok := p.Args["toBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid toBlock")
	}

	validatorAddr := common.HexToAddress(validatorAddrStr)
	fromBlock, err := strconv.ParseUint(fromBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromBlock format: %w", err)
	}

	toBlock, err := strconv.ParseUint(toBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toBlock format: %w", err)
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := s.storage.(storage.WBFTReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support WBFT metadata")
	}

	stats, err := wbftReader.GetValidatorSigningStats(ctx, validatorAddr, fromBlock, toBlock)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get validator signing stats",
			zap.String("validatorAddress", validatorAddrStr),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	return s.validatorSigningStatsToMap(stats), nil
}

// resolveAllValidatorsSigningStats resolves signing statistics for all validators in a block range
func (s *Schema) resolveAllValidatorsSigningStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	fromBlockStr, ok := p.Args["fromBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid fromBlock")
	}

	toBlockStr, ok := p.Args["toBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid toBlock")
	}

	fromBlock, err := strconv.ParseUint(fromBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromBlock format: %w", err)
	}

	toBlock, err := strconv.ParseUint(toBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toBlock format: %w", err)
	}

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := s.storage.(storage.WBFTReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support WBFT metadata")
	}

	statsList, err := wbftReader.GetAllValidatorsSigningStats(ctx, fromBlock, toBlock, limit, offset)
	if err != nil {
		s.logger.Error("failed to get all validators signing stats",
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	// Convert to maps
	nodes := make([]interface{}, len(statsList))
	for i, stats := range statsList {
		nodes[i] = s.validatorSigningStatsToMap(stats)
	}

	// For simplicity, return total count as length of nodes
	// In production, you might want to query the actual total count
	totalCount := len(nodes)

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
		},
	}, nil
}

// resolveValidatorSigningActivity resolves detailed signing activity for a specific validator
func (s *Schema) resolveValidatorSigningActivity(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	validatorAddrStr, ok := p.Args["validatorAddress"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid validator address")
	}

	fromBlockStr, ok := p.Args["fromBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid fromBlock")
	}

	toBlockStr, ok := p.Args["toBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid toBlock")
	}

	validatorAddr := common.HexToAddress(validatorAddrStr)
	fromBlock, err := strconv.ParseUint(fromBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromBlock format: %w", err)
	}

	toBlock, err := strconv.ParseUint(toBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toBlock format: %w", err)
	}

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := s.storage.(storage.WBFTReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support WBFT metadata")
	}

	activities, err := wbftReader.GetValidatorSigningActivity(ctx, validatorAddr, fromBlock, toBlock, limit, offset)
	if err != nil {
		s.logger.Error("failed to get validator signing activity",
			zap.String("validatorAddress", validatorAddrStr),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	// Convert to maps
	nodes := make([]interface{}, len(activities))
	for i, activity := range activities {
		nodes[i] = s.validatorSigningActivityToMap(activity)
	}

	totalCount := len(nodes)

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) == limit,
			"hasPreviousPage": offset > 0,
		},
	}, nil
}

// resolveBlockSigners resolves list of validators who signed a specific block
func (s *Schema) resolveBlockSigners(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	numberStr, ok := p.Args["blockNumber"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid block number")
	}

	number, err := strconv.ParseUint(numberStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid block number format: %w", err)
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := s.storage.(storage.WBFTReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support WBFT metadata")
	}

	preparers, committers, err := wbftReader.GetBlockSigners(ctx, number)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get block signers",
			zap.Uint64("blockNumber", number),
			zap.Error(err))
		return nil, err
	}

	// Convert addresses to hex strings
	preparersHex := make([]string, len(preparers))
	for i, addr := range preparers {
		preparersHex[i] = addr.Hex()
	}

	committersHex := make([]string, len(committers))
	for i, addr := range committers {
		committersHex[i] = addr.Hex()
	}

	return map[string]interface{}{
		"blockNumber": fmt.Sprintf("%d", number),
		"preparers":   preparersHex,
		"committers":  committersHex,
	}, nil
}

// ========== Helper mapper functions ==========

// wbftBlockExtraToMap converts WBFTBlockExtra to a map
func (s *Schema) wbftBlockExtraToMap(extra *storage.WBFTBlockExtra) map[string]interface{} {
	m := map[string]interface{}{
		"blockNumber":  fmt.Sprintf("%d", extra.BlockNumber),
		"blockHash":    extra.BlockHash.Hex(),
		"randaoReveal": fmt.Sprintf("0x%x", extra.RandaoReveal),
		"prevRound":    int(extra.PrevRound),
		"round":        int(extra.Round),
		"timestamp":    fmt.Sprintf("%d", extra.Timestamp),
	}

	if extra.PrevPreparedSeal != nil {
		m["prevPreparedSeal"] = s.wbftAggregatedSealToMap(extra.PrevPreparedSeal)
	}

	if extra.PrevCommittedSeal != nil {
		m["prevCommittedSeal"] = s.wbftAggregatedSealToMap(extra.PrevCommittedSeal)
	}

	if extra.PreparedSeal != nil {
		m["preparedSeal"] = s.wbftAggregatedSealToMap(extra.PreparedSeal)
	}

	if extra.CommittedSeal != nil {
		m["committedSeal"] = s.wbftAggregatedSealToMap(extra.CommittedSeal)
	}

	if extra.GasTip != nil {
		m["gasTip"] = extra.GasTip.String()
	}

	if extra.EpochInfo != nil {
		m["epochInfo"] = s.epochInfoToMap(extra.EpochInfo)
	}

	return m
}

// wbftAggregatedSealToMap converts WBFTAggregatedSeal to a map
func (s *Schema) wbftAggregatedSealToMap(seal *storage.WBFTAggregatedSeal) map[string]interface{} {
	return map[string]interface{}{
		"sealers":   fmt.Sprintf("0x%x", seal.Sealers),
		"signature": fmt.Sprintf("0x%x", seal.Signature),
	}
}

// epochInfoToMap converts EpochInfo to a map
func (s *Schema) epochInfoToMap(info *storage.EpochInfo) map[string]interface{} {
	candidates := make([]interface{}, len(info.Candidates))
	for i, c := range info.Candidates {
		candidates[i] = map[string]interface{}{
			"address":   c.Address.Hex(),
			"diligence": fmt.Sprintf("%d", c.Diligence),
		}
	}

	validators := make([]int, len(info.Validators))
	for i, v := range info.Validators {
		validators[i] = int(v)
	}

	blsPublicKeys := make([]string, len(info.BLSPublicKeys))
	for i, key := range info.BLSPublicKeys {
		blsPublicKeys[i] = fmt.Sprintf("0x%x", key)
	}

	return map[string]interface{}{
		"epochNumber":   fmt.Sprintf("%d", info.EpochNumber),
		"blockNumber":   fmt.Sprintf("%d", info.BlockNumber),
		"candidates":    candidates,
		"validators":    validators,
		"blsPublicKeys": blsPublicKeys,
	}
}

// validatorSigningStatsToMap converts ValidatorSigningStats to a map
func (s *Schema) validatorSigningStatsToMap(stats *storage.ValidatorSigningStats) map[string]interface{} {
	return map[string]interface{}{
		"validatorAddress": stats.ValidatorAddress.Hex(),
		"validatorIndex":   int(stats.ValidatorIndex),
		"prepareSignCount": fmt.Sprintf("%d", stats.PrepareSignCount),
		"prepareMissCount": fmt.Sprintf("%d", stats.PrepareMissCount),
		"commitSignCount":  fmt.Sprintf("%d", stats.CommitSignCount),
		"commitMissCount":  fmt.Sprintf("%d", stats.CommitMissCount),
		"fromBlock":        fmt.Sprintf("%d", stats.FromBlock),
		"toBlock":          fmt.Sprintf("%d", stats.ToBlock),
		"signingRate":      stats.SigningRate,
	}
}

// validatorSigningActivityToMap converts ValidatorSigningActivity to a map
func (s *Schema) validatorSigningActivityToMap(activity *storage.ValidatorSigningActivity) map[string]interface{} {
	return map[string]interface{}{
		"blockNumber":      fmt.Sprintf("%d", activity.BlockNumber),
		"blockHash":        activity.BlockHash.Hex(),
		"validatorAddress": activity.ValidatorAddress.Hex(),
		"validatorIndex":   int(activity.ValidatorIndex),
		"signedPrepare":    activity.SignedPrepare,
		"signedCommit":     activity.SignedCommit,
		"round":            int(activity.Round),
		"timestamp":        fmt.Sprintf("%d", activity.Timestamp),
	}
}
