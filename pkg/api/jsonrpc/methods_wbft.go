package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// getWBFTBlockExtra returns WBFT consensus metadata for a block by number
func (h *Handler) getWBFTBlockExtra(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		BlockNumber interface{} `json:"blockNumber"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.BlockNumber == nil {
		return nil, NewError(InvalidParams, "missing required parameter: blockNumber", nil)
	}

	// Parse block number
	var blockNumber uint64
	switch v := p.BlockNumber.(type) {
	case float64:
		blockNumber = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid block number format", err.Error())
		}
		blockNumber = num
	default:
		return nil, NewError(InvalidParams, "block number must be string or number", nil)
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := h.storage.(storage.WBFTReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support WBFT metadata", nil)
	}

	extra, err := wbftReader.GetWBFTBlockExtra(ctx, blockNumber)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		h.logger.Error("failed to get WBFT block extra",
			zap.Uint64("blockNumber", blockNumber),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get WBFT block extra", err.Error())
	}

	return wbftBlockExtraToMap(extra), nil
}

// getWBFTBlockExtraByHash returns WBFT consensus metadata for a block by hash
func (h *Handler) getWBFTBlockExtraByHash(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		BlockHash string `json:"blockHash"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.BlockHash == "" {
		return nil, NewError(InvalidParams, "missing required parameter: blockHash", nil)
	}

	hash := common.HexToHash(p.BlockHash)

	// Check if storage implements WBFTReader
	wbftReader, ok := h.storage.(storage.WBFTReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support WBFT metadata", nil)
	}

	extra, err := wbftReader.GetWBFTBlockExtraByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		h.logger.Error("failed to get WBFT block extra by hash",
			zap.String("blockHash", p.BlockHash),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get WBFT block extra", err.Error())
	}

	return wbftBlockExtraToMap(extra), nil
}

// getEpochInfo returns epoch information for a specific epoch
func (h *Handler) getEpochInfo(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		EpochNumber interface{} `json:"epochNumber"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.EpochNumber == nil {
		return nil, NewError(InvalidParams, "missing required parameter: epochNumber", nil)
	}

	// Parse epoch number
	var epochNumber uint64
	switch v := p.EpochNumber.(type) {
	case float64:
		epochNumber = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid epoch number format", err.Error())
		}
		epochNumber = num
	default:
		return nil, NewError(InvalidParams, "epoch number must be string or number", nil)
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := h.storage.(storage.WBFTReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support WBFT metadata", nil)
	}

	epochInfo, err := wbftReader.GetEpochInfo(ctx, epochNumber)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		h.logger.Error("failed to get epoch info",
			zap.Uint64("epochNumber", epochNumber),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get epoch info", err.Error())
	}

	return epochInfoToMap(epochInfo), nil
}

// getLatestEpochInfo returns the most recent epoch information
func (h *Handler) getLatestEpochInfo(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	// No parameters needed

	// Check if storage implements WBFTReader
	wbftReader, ok := h.storage.(storage.WBFTReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support WBFT metadata", nil)
	}

	epochInfo, err := wbftReader.GetLatestEpochInfo(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		h.logger.Error("failed to get latest epoch info", zap.Error(err))
		return nil, NewError(InternalError, "failed to get latest epoch info", err.Error())
	}

	return epochInfoToMap(epochInfo), nil
}

// getValidatorSigningStats returns signing statistics for a specific validator
func (h *Handler) getValidatorSigningStats(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		ValidatorAddress string      `json:"validatorAddress"`
		FromBlock        interface{} `json:"fromBlock"`
		ToBlock          interface{} `json:"toBlock"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.ValidatorAddress == "" {
		return nil, NewError(InvalidParams, "missing required parameter: validatorAddress", nil)
	}
	if p.FromBlock == nil {
		return nil, NewError(InvalidParams, "missing required parameter: fromBlock", nil)
	}
	if p.ToBlock == nil {
		return nil, NewError(InvalidParams, "missing required parameter: toBlock", nil)
	}

	validatorAddr := common.HexToAddress(p.ValidatorAddress)

	// Parse fromBlock
	var fromBlock uint64
	switch v := p.FromBlock.(type) {
	case float64:
		fromBlock = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid fromBlock format", err.Error())
		}
		fromBlock = num
	default:
		return nil, NewError(InvalidParams, "fromBlock must be string or number", nil)
	}

	// Parse toBlock
	var toBlock uint64
	switch v := p.ToBlock.(type) {
	case float64:
		toBlock = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid toBlock format", err.Error())
		}
		toBlock = num
	default:
		return nil, NewError(InvalidParams, "toBlock must be string or number", nil)
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := h.storage.(storage.WBFTReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support WBFT metadata", nil)
	}

	stats, err := wbftReader.GetValidatorSigningStats(ctx, validatorAddr, fromBlock, toBlock)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		h.logger.Error("failed to get validator signing stats",
			zap.String("validatorAddress", p.ValidatorAddress),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get validator signing stats", err.Error())
	}

	return validatorSigningStatsToMap(stats), nil
}

// getAllValidatorsSigningStats returns signing statistics for all validators in a block range
func (h *Handler) getAllValidatorsSigningStats(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		FromBlock  interface{}            `json:"fromBlock"`
		ToBlock    interface{}            `json:"toBlock"`
		Pagination map[string]interface{} `json:"pagination"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.FromBlock == nil {
		return nil, NewError(InvalidParams, "missing required parameter: fromBlock", nil)
	}
	if p.ToBlock == nil {
		return nil, NewError(InvalidParams, "missing required parameter: toBlock", nil)
	}

	// Parse fromBlock
	var fromBlock uint64
	switch v := p.FromBlock.(type) {
	case float64:
		fromBlock = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid fromBlock format", err.Error())
		}
		fromBlock = num
	default:
		return nil, NewError(InvalidParams, "fromBlock must be string or number", nil)
	}

	// Parse toBlock
	var toBlock uint64
	switch v := p.ToBlock.(type) {
	case float64:
		toBlock = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid toBlock format", err.Error())
		}
		toBlock = num
	default:
		return nil, NewError(InvalidParams, "toBlock must be string or number", nil)
	}

	// Parse pagination
	limit := constants.DefaultPaginationLimit
	offset := 0
	if p.Pagination != nil {
		if l, ok := p.Pagination["limit"].(float64); ok && l > 0 {
			if int(l) > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = int(l)
			}
		}
		if o, ok := p.Pagination["offset"].(float64); ok && o >= 0 {
			offset = int(o)
		}
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := h.storage.(storage.WBFTReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support WBFT metadata", nil)
	}

	statsList, err := wbftReader.GetAllValidatorsSigningStats(ctx, fromBlock, toBlock, limit, offset)
	if err != nil {
		h.logger.Error("failed to get all validators signing stats",
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get all validators signing stats", err.Error())
	}

	// Convert to maps
	nodes := make([]interface{}, len(statsList))
	for i, stats := range statsList {
		nodes[i] = validatorSigningStatsToMap(stats)
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

// getValidatorSigningActivity returns detailed signing activity for a specific validator
func (h *Handler) getValidatorSigningActivity(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		ValidatorAddress string                 `json:"validatorAddress"`
		FromBlock        interface{}            `json:"fromBlock"`
		ToBlock          interface{}            `json:"toBlock"`
		Pagination       map[string]interface{} `json:"pagination"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.ValidatorAddress == "" {
		return nil, NewError(InvalidParams, "missing required parameter: validatorAddress", nil)
	}
	if p.FromBlock == nil {
		return nil, NewError(InvalidParams, "missing required parameter: fromBlock", nil)
	}
	if p.ToBlock == nil {
		return nil, NewError(InvalidParams, "missing required parameter: toBlock", nil)
	}

	validatorAddr := common.HexToAddress(p.ValidatorAddress)

	// Parse fromBlock
	var fromBlock uint64
	switch v := p.FromBlock.(type) {
	case float64:
		fromBlock = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid fromBlock format", err.Error())
		}
		fromBlock = num
	default:
		return nil, NewError(InvalidParams, "fromBlock must be string or number", nil)
	}

	// Parse toBlock
	var toBlock uint64
	switch v := p.ToBlock.(type) {
	case float64:
		toBlock = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid toBlock format", err.Error())
		}
		toBlock = num
	default:
		return nil, NewError(InvalidParams, "toBlock must be string or number", nil)
	}

	// Parse pagination
	limit := constants.DefaultPaginationLimit
	offset := 0
	if p.Pagination != nil {
		if l, ok := p.Pagination["limit"].(float64); ok && l > 0 {
			if int(l) > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = int(l)
			}
		}
		if o, ok := p.Pagination["offset"].(float64); ok && o >= 0 {
			offset = int(o)
		}
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := h.storage.(storage.WBFTReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support WBFT metadata", nil)
	}

	activities, err := wbftReader.GetValidatorSigningActivity(ctx, validatorAddr, fromBlock, toBlock, limit, offset)
	if err != nil {
		h.logger.Error("failed to get validator signing activity",
			zap.String("validatorAddress", p.ValidatorAddress),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get validator signing activity", err.Error())
	}

	// Convert to maps
	nodes := make([]interface{}, len(activities))
	for i, activity := range activities {
		nodes[i] = validatorSigningActivityToMap(activity)
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

// getBlockSigners returns list of validators who signed a specific block
func (h *Handler) getBlockSigners(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		BlockNumber interface{} `json:"blockNumber"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.BlockNumber == nil {
		return nil, NewError(InvalidParams, "missing required parameter: blockNumber", nil)
	}

	// Parse block number
	var blockNumber uint64
	switch v := p.BlockNumber.(type) {
	case float64:
		blockNumber = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid block number format", err.Error())
		}
		blockNumber = num
	default:
		return nil, NewError(InvalidParams, "block number must be string or number", nil)
	}

	// Check if storage implements WBFTReader
	wbftReader, ok := h.storage.(storage.WBFTReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support WBFT metadata", nil)
	}

	preparers, committers, err := wbftReader.GetBlockSigners(ctx, blockNumber)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		h.logger.Error("failed to get block signers",
			zap.Uint64("blockNumber", blockNumber),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get block signers", err.Error())
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
		"blockNumber": fmt.Sprintf("%d", blockNumber),
		"preparers":   preparersHex,
		"committers":  committersHex,
	}, nil
}

// ========== Helper mapper functions ==========

// wbftBlockExtraToMap converts WBFTBlockExtra to a map
func wbftBlockExtraToMap(extra *storage.WBFTBlockExtra) map[string]interface{} {
	m := map[string]interface{}{
		"blockNumber":  fmt.Sprintf("%d", extra.BlockNumber),
		"blockHash":    extra.BlockHash.Hex(),
		"randaoReveal": fmt.Sprintf("0x%x", extra.RandaoReveal),
		"prevRound":    int(extra.PrevRound),
		"round":        int(extra.Round),
		"timestamp":    fmt.Sprintf("%d", extra.Timestamp),
	}

	if extra.PrevPreparedSeal != nil {
		m["prevPreparedSeal"] = wbftAggregatedSealToMap(extra.PrevPreparedSeal)
	}

	if extra.PrevCommittedSeal != nil {
		m["prevCommittedSeal"] = wbftAggregatedSealToMap(extra.PrevCommittedSeal)
	}

	if extra.PreparedSeal != nil {
		m["preparedSeal"] = wbftAggregatedSealToMap(extra.PreparedSeal)
	}

	if extra.CommittedSeal != nil {
		m["committedSeal"] = wbftAggregatedSealToMap(extra.CommittedSeal)
	}

	if extra.GasTip != nil {
		m["gasTip"] = extra.GasTip.String()
	}

	if extra.EpochInfo != nil {
		m["epochInfo"] = epochInfoToMap(extra.EpochInfo)
	}

	return m
}

// wbftAggregatedSealToMap converts WBFTAggregatedSeal to a map
func wbftAggregatedSealToMap(seal *storage.WBFTAggregatedSeal) map[string]interface{} {
	return map[string]interface{}{
		"sealers":   fmt.Sprintf("0x%x", seal.Sealers),
		"signature": fmt.Sprintf("0x%x", seal.Signature),
	}
}

// epochInfoToMap converts EpochInfo to a map
func epochInfoToMap(info *storage.EpochInfo) map[string]interface{} {
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
func validatorSigningStatsToMap(stats *storage.ValidatorSigningStats) map[string]interface{} {
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
func validatorSigningActivityToMap(activity *storage.ValidatorSigningActivity) map[string]interface{} {
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
