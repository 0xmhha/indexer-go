package graphql

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/storage"
	consensustypes "github.com/0xmhha/indexer-go/types/consensus"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveConsensusData resolves complete consensus information for a specific block
func (s *Schema) resolveConsensusData(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	blockNumberStr, ok := p.Args["blockNumber"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid block number")
	}

	blockNumber, err := strconv.ParseUint(blockNumberStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid block number format: %w", err)
	}

	// Get PebbleStorage and create ConsensusStorage
	pebbleStorage, ok := s.storage.(*storage.PebbleStorage)
	if !ok {
		return nil, fmt.Errorf("storage does not support consensus operations")
	}

	consensusStorage := storage.NewConsensusStorage(pebbleStorage, s.logger)
	data, err := consensusStorage.GetConsensusData(ctx, blockNumber)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get consensus data",
			zap.Uint64("blockNumber", blockNumber),
			zap.Error(err))
		return nil, err
	}

	return s.consensusDataToMap(data), nil
}

// resolveValidatorStats resolves statistics for a specific validator over a block range
func (s *Schema) resolveValidatorStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
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

	address := common.HexToAddress(addressStr)
	fromBlock, err := strconv.ParseUint(fromBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromBlock format: %w", err)
	}

	toBlock, err := strconv.ParseUint(toBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toBlock format: %w", err)
	}

	pebbleStorage, ok := s.storage.(*storage.PebbleStorage)
	if !ok {
		return nil, fmt.Errorf("storage does not support consensus operations")
	}

	consensusStorage := storage.NewConsensusStorage(pebbleStorage, s.logger)
	stats, err := consensusStorage.GetValidatorStats(ctx, address, fromBlock, toBlock)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get validator stats",
			zap.String("address", addressStr),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	return s.validatorStatsToMap(stats), nil
}

// resolveValidatorParticipation resolves detailed participation information for a validator
func (s *Schema) resolveValidatorParticipation(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
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

	address := common.HexToAddress(addressStr)
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

	pebbleStorage, ok := s.storage.(*storage.PebbleStorage)
	if !ok {
		return nil, fmt.Errorf("storage does not support consensus operations")
	}

	consensusStorage := storage.NewConsensusStorage(pebbleStorage, s.logger)
	participation, err := consensusStorage.GetValidatorParticipation(ctx, address, fromBlock, toBlock, limit, offset)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get validator participation",
			zap.String("address", addressStr),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	return s.validatorParticipationToMap(participation), nil
}

// resolveAllValidatorStats resolves statistics for all validators in a block range
func (s *Schema) resolveAllValidatorStats(p graphql.ResolveParams) (interface{}, error) {
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

	pebbleStorage, ok := s.storage.(*storage.PebbleStorage)
	if !ok {
		return nil, fmt.Errorf("storage does not support consensus operations")
	}

	consensusStorage := storage.NewConsensusStorage(pebbleStorage, s.logger)
	statsMap, err := consensusStorage.GetAllValidatorStats(ctx, fromBlock, toBlock, limit, offset)
	if err != nil {
		s.logger.Error("failed to get all validator stats",
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	// Convert map to list
	statsList := make([]interface{}, 0, len(statsMap))
	for _, stats := range statsMap {
		statsList = append(statsList, s.validatorStatsToMap(stats))
	}

	return statsList, nil
}

// resolveEpochData resolves epoch information for a specific epoch
func (s *Schema) resolveEpochData(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	epochNumberStr, ok := p.Args["epochNumber"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid epoch number")
	}

	epochNumber, err := strconv.ParseUint(epochNumberStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid epoch number format: %w", err)
	}

	pebbleStorage, ok := s.storage.(*storage.PebbleStorage)
	if !ok {
		return nil, fmt.Errorf("storage does not support consensus operations")
	}

	consensusStorage := storage.NewConsensusStorage(pebbleStorage, s.logger)
	epochData, err := consensusStorage.GetEpochInfo(ctx, epochNumber)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get epoch data",
			zap.Uint64("epochNumber", epochNumber),
			zap.Error(err))
		return nil, err
	}

	return s.epochDataToMap(epochData), nil
}

// resolveLatestEpochData resolves the most recent epoch information
func (s *Schema) resolveLatestEpochData(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	pebbleStorage, ok := s.storage.(*storage.PebbleStorage)
	if !ok {
		return nil, fmt.Errorf("storage does not support consensus operations")
	}

	consensusStorage := storage.NewConsensusStorage(pebbleStorage, s.logger)
	epochData, err := consensusStorage.GetLatestEpochInfo(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		s.logger.Error("failed to get latest epoch data",
			zap.Error(err))
		return nil, err
	}

	return s.epochDataToMap(epochData), nil
}

// ========== Helper mapper functions ==========

// consensusDataToMap converts ConsensusData to a map for GraphQL
func (s *Schema) consensusDataToMap(data *consensustypes.ConsensusData) map[string]interface{} {
	m := map[string]interface{}{
		"blockNumber":       fmt.Sprintf("%d", data.BlockNumber),
		"blockHash":         data.BlockHash.Hex(),
		"round":             int(data.Round),
		"prevRound":         int(data.PrevRound),
		"roundChanged":      data.RoundChanged,
		"proposer":          data.Proposer.Hex(),
		"prepareCount":      data.PrepareCount,
		"commitCount":       data.CommitCount,
		"timestamp":         fmt.Sprintf("%d", data.Timestamp),
		"participationRate": data.ParticipationRate(),
		"isHealthy":         data.IsHealthy(),
		"isEpochBoundary":   data.IsEpochBoundary,
	}

	// Convert validator addresses
	validators := make([]string, len(data.Validators))
	for i, addr := range data.Validators {
		validators[i] = addr.Hex()
	}
	m["validators"] = validators

	// Convert prepare signers
	prepareSigners := make([]string, len(data.PrepareSigners))
	for i, addr := range data.PrepareSigners {
		prepareSigners[i] = addr.Hex()
	}
	m["prepareSigners"] = prepareSigners

	// Convert commit signers
	commitSigners := make([]string, len(data.CommitSigners))
	for i, addr := range data.CommitSigners {
		commitSigners[i] = addr.Hex()
	}
	m["commitSigners"] = commitSigners

	// Convert missed prepare
	missedPrepare := make([]string, len(data.MissedPrepare))
	for i, addr := range data.MissedPrepare {
		missedPrepare[i] = addr.Hex()
	}
	m["missedPrepare"] = missedPrepare

	// Convert missed commit
	missedCommit := make([]string, len(data.MissedCommit))
	for i, addr := range data.MissedCommit {
		missedCommit[i] = addr.Hex()
	}
	m["missedCommit"] = missedCommit

	// Optional fields
	if data.VanityData != nil {
		m["vanityData"] = fmt.Sprintf("0x%x", data.VanityData)
	}

	if data.RandaoReveal != nil {
		m["randaoReveal"] = fmt.Sprintf("0x%x", data.RandaoReveal)
	}

	if data.GasTip != nil {
		m["gasTip"] = data.GasTip.String()
	}

	if data.EpochInfo != nil {
		m["epochInfo"] = s.epochDataToMap(data.EpochInfo)
	}

	return m
}

// validatorStatsToMap converts ValidatorStats to a map for GraphQL
func (s *Schema) validatorStatsToMap(stats *consensustypes.ValidatorStats) map[string]interface{} {
	m := map[string]interface{}{
		"address":           stats.Address.Hex(),
		"totalBlocks":       fmt.Sprintf("%d", stats.TotalBlocks),
		"blocksProposed":    fmt.Sprintf("%d", stats.BlocksProposed),
		"preparesSigned":    fmt.Sprintf("%d", stats.PreparesSigned),
		"commitsSigned":     fmt.Sprintf("%d", stats.CommitsSigned),
		"preparesMissed":    fmt.Sprintf("%d", stats.PreparesMissed),
		"commitsMissed":     fmt.Sprintf("%d", stats.CommitsMissed),
		"participationRate": stats.ParticipationRate,
	}

	if stats.LastProposedBlock > 0 {
		m["lastProposedBlock"] = fmt.Sprintf("%d", stats.LastProposedBlock)
	}

	if stats.LastCommittedBlock > 0 {
		m["lastCommittedBlock"] = fmt.Sprintf("%d", stats.LastCommittedBlock)
	}

	if stats.LastSeenBlock > 0 {
		m["lastSeenBlock"] = fmt.Sprintf("%d", stats.LastSeenBlock)
	}

	return m
}

// validatorParticipationToMap converts ValidatorParticipation to a map for GraphQL
func (s *Schema) validatorParticipationToMap(participation *consensustypes.ValidatorParticipation) map[string]interface{} {
	blocks := make([]interface{}, len(participation.Blocks))
	for i, block := range participation.Blocks {
		blocks[i] = map[string]interface{}{
			"blockNumber":   fmt.Sprintf("%d", block.BlockNumber),
			"wasProposer":   block.WasProposer,
			"signedPrepare": block.SignedPrepare,
			"signedCommit":  block.SignedCommit,
			"round":         int(block.Round),
		}
	}

	return map[string]interface{}{
		"address":           participation.Address.Hex(),
		"startBlock":        fmt.Sprintf("%d", participation.StartBlock),
		"endBlock":          fmt.Sprintf("%d", participation.EndBlock),
		"totalBlocks":       fmt.Sprintf("%d", participation.TotalBlocks),
		"blocksProposed":    fmt.Sprintf("%d", participation.BlocksProposed),
		"blocksCommitted":   fmt.Sprintf("%d", participation.BlocksCommitted),
		"blocksMissed":      fmt.Sprintf("%d", participation.BlocksMissed),
		"participationRate": participation.ParticipationRate,
		"blocks":            blocks,
	}
}

// epochDataToMap converts EpochData to a map for GraphQL
func (s *Schema) epochDataToMap(epoch *consensustypes.EpochData) map[string]interface{} {
	validators := make([]interface{}, len(epoch.Validators))
	for i, v := range epoch.Validators {
		validators[i] = map[string]interface{}{
			"address":   v.Address.Hex(),
			"index":     int(v.Index),
			"blsPubKey": fmt.Sprintf("0x%x", v.BLSPubKey),
		}
	}

	candidates := make([]interface{}, len(epoch.Candidates))
	for i, c := range epoch.Candidates {
		candidates[i] = map[string]interface{}{
			"address":   c.Address.Hex(),
			"diligence": fmt.Sprintf("%d", c.Diligence),
		}
	}

	return map[string]interface{}{
		"epochNumber":    fmt.Sprintf("%d", epoch.EpochNumber),
		"validatorCount": epoch.ValidatorCount,
		"candidateCount": epoch.CandidateCount,
		"validators":     validators,
		"candidates":     candidates,
	}
}
