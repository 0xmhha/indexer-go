package storage

import (
	"context"
	"math/big"
	"sort"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// ============================================================================
// Token Balance Helpers
// ============================================================================

// ERC20 Transfer event signature
var transferEventTopic = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

// scanTransferEvents scans receipts for Transfer events involving the address
// and returns a map of contract addresses to balances
func (s *PebbleStorage) scanTransferEvents(ctx context.Context, addr common.Address, latestHeight uint64) (map[common.Address]*big.Int, error) {
	balanceMap := make(map[common.Address]*big.Int)

	for height := uint64(0); height <= latestHeight; height++ {
		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err != nil {
			continue
		}

		for _, receipt := range receipts {
			s.processReceiptTransfers(receipt, addr, balanceMap)
		}
	}

	return balanceMap, nil
}

// processReceiptTransfers processes a single receipt for Transfer events
func (s *PebbleStorage) processReceiptTransfers(receipt *types.Receipt, addr common.Address, balanceMap map[common.Address]*big.Int) {
	for _, log := range receipt.Logs {
		// Check if this is a Transfer event
		if len(log.Topics) < 3 || log.Topics[0] != transferEventTopic {
			continue
		}

		// Extract from and to addresses from topics
		from := common.BytesToAddress(log.Topics[1].Bytes())
		to := common.BytesToAddress(log.Topics[2].Bytes())

		// Check if this transfer involves our address
		if from != addr && to != addr {
			continue
		}

		// Extract value from data
		if len(log.Data) < 32 {
			continue
		}
		value := new(big.Int).SetBytes(log.Data[:32])

		// Get or create balance entry for this contract
		contract := log.Address
		if _, exists := balanceMap[contract]; !exists {
			balanceMap[contract] = big.NewInt(0)
		}

		// Update balance
		if to == addr {
			balanceMap[contract].Add(balanceMap[contract], value)
		} else if from == addr {
			balanceMap[contract].Sub(balanceMap[contract], value)
		}
	}
}

// applyTokenMetadata applies metadata to a TokenBalance from various sources
func (s *PebbleStorage) applyTokenMetadata(ctx context.Context, tb *TokenBalance, contract common.Address) {
	// Priority: 1) System contract metadata, 2) Database, 3) On-demand fetch from chain
	if metadata := GetSystemContractTokenMetadata(contract); metadata != nil {
		// 1. System contract token metadata (hardcoded)
		tb.Name = metadata.Name
		tb.Symbol = metadata.Symbol
		decimals := metadata.Decimals
		tb.Decimals = &decimals
		return
	}

	if dbMetadata, err := s.GetTokenMetadata(ctx, contract); err == nil && dbMetadata != nil {
		// 2. Database token metadata
		tb.Name = dbMetadata.Name
		tb.Symbol = dbMetadata.Symbol
		decimals := int(dbMetadata.Decimals)
		tb.Decimals = &decimals
		if dbMetadata.Standard != "" {
			tb.TokenType = string(dbMetadata.Standard)
		}
		tb.Metadata = buildTokenMetadataJSON(dbMetadata)
		return
	}

	// 3. On-demand fetch from chain and cache
	if s.tokenMetadataFetcher != nil {
		s.fetchAndCacheTokenMetadata(ctx, tb, contract)
	}
}

// fetchAndCacheTokenMetadata fetches token metadata from chain and caches it
func (s *PebbleStorage) fetchAndCacheTokenMetadata(ctx context.Context, tb *TokenBalance, contract common.Address) {
	fetchedMetadata, err := s.tokenMetadataFetcher.FetchTokenMetadata(ctx, contract)
	if err != nil || fetchedMetadata == nil {
		return
	}

	tb.Name = fetchedMetadata.Name
	tb.Symbol = fetchedMetadata.Symbol
	decimals := int(fetchedMetadata.Decimals)
	tb.Decimals = &decimals
	if fetchedMetadata.Standard != "" {
		tb.TokenType = string(fetchedMetadata.Standard)
	}
	tb.Metadata = buildTokenMetadataJSON(fetchedMetadata)

	// Cache the fetched metadata
	if saveErr := s.SaveTokenMetadata(ctx, fetchedMetadata); saveErr != nil {
		s.logger.Warn("Failed to cache fetched token metadata",
			zap.String("contract", contract.Hex()),
			zap.Error(saveErr),
		)
	} else {
		s.logger.Info("Cached on-demand fetched token metadata",
			zap.String("contract", contract.Hex()),
			zap.String("name", fetchedMetadata.Name),
			zap.String("symbol", fetchedMetadata.Symbol),
			zap.Uint8("decimals", fetchedMetadata.Decimals),
		)
	}
}

// buildTokenBalanceResult builds the result slice from balance map with filtering
func (s *PebbleStorage) buildTokenBalanceResult(ctx context.Context, balanceMap map[common.Address]*big.Int, tokenType string) []TokenBalance {
	result := make([]TokenBalance, 0, len(balanceMap))

	for contract, balance := range balanceMap {
		if balance.Sign() <= 0 {
			continue
		}

		tb := TokenBalance{
			ContractAddress: contract,
			TokenType:       string(TokenStandardERC20),
			Balance:         balance,
			TokenID:         "",
			Name:            "",
			Symbol:          "",
			Decimals:        nil,
			Metadata:        "",
		}

		s.applyTokenMetadata(ctx, &tb, contract)

		// Apply tokenType filter if specified
		if tokenType == "" || tokenType == tb.TokenType {
			result = append(result, tb)
		}
	}

	return result
}

// ============================================================================
// Miner Stats Helpers
// ============================================================================

// determineBlockRange calculates the actual block range to scan
func determineBlockRange(fromBlock, toBlock, latestHeight uint64) (start, end uint64, valid bool) {
	start = fromBlock
	end = toBlock

	if toBlock == 0 || toBlock > latestHeight {
		end = latestHeight
	}

	if fromBlock > end {
		return 0, 0, false
	}

	return start, end, true
}

// aggregateMinerStats scans blocks and aggregates miner statistics
func (s *PebbleStorage) aggregateMinerStats(ctx context.Context, startBlock, endBlock uint64) (map[common.Address]*MinerStats, uint64) {
	minerMap := make(map[common.Address]*MinerStats)
	totalBlocks := uint64(0)

	for height := startBlock; height <= endBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue
		}

		totalBlocks++
		miner := block.Coinbase()

		stats := s.getOrCreateMinerStats(minerMap, miner)
		stats.BlockCount++

		if height > stats.LastBlockNumber {
			stats.LastBlockNumber = height
			stats.LastBlockTime = block.Time()
		}

		s.addBlockRewardsToStats(ctx, block, stats)
	}

	return minerMap, totalBlocks
}

// getOrCreateMinerStats gets or creates a MinerStats entry in the map
func (s *PebbleStorage) getOrCreateMinerStats(minerMap map[common.Address]*MinerStats, miner common.Address) *MinerStats {
	stats, exists := minerMap[miner]
	if !exists {
		stats = &MinerStats{
			Address:         miner,
			BlockCount:      0,
			LastBlockNumber: 0,
			LastBlockTime:   0,
			Percentage:      0,
			TotalRewards:    big.NewInt(0),
		}
		minerMap[miner] = stats
	}
	return stats
}

// addBlockRewardsToStats calculates and adds transaction fees to miner stats
func (s *PebbleStorage) addBlockRewardsToStats(ctx context.Context, block *types.Block, stats *MinerStats) {
	// Create transaction map for O(1) lookup
	txMap := make(map[common.Hash]*types.Transaction)
	for _, tx := range block.Transactions() {
		txMap[tx.Hash()] = tx
	}

	// Get receipts and calculate fees
	receipts, err := s.GetReceiptsByBlockNumber(ctx, block.NumberU64())
	if err != nil {
		return
	}

	for _, receipt := range receipts {
		if receipt.GasUsed > 0 {
			if tx, found := txMap[receipt.TxHash]; found {
				fee := new(big.Int).Mul(tx.GasPrice(), big.NewInt(int64(receipt.GasUsed)))
				stats.TotalRewards.Add(stats.TotalRewards, fee)
			}
		}
	}
}

// calculateMinerPercentages calculates the percentage for each miner
func calculateMinerPercentages(minerMap map[common.Address]*MinerStats, totalBlocks uint64) {
	if totalBlocks == 0 {
		return
	}

	for _, stats := range minerMap {
		stats.Percentage = float64(stats.BlockCount) / float64(totalBlocks) * 100.0
	}
}

// sortAndLimitMinerStats converts map to sorted slice and applies limit
func sortAndLimitMinerStats(minerMap map[common.Address]*MinerStats, limit int) []MinerStats {
	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}

	result := make([]MinerStats, 0, len(minerMap))
	for _, stats := range minerMap {
		result = append(result, *stats)
	}

	// Sort by block count (descending)
	sort.Slice(result, func(i, j int) bool {
		return result[i].BlockCount > result[j].BlockCount
	})

	// Apply limit
	if len(result) > limit {
		result = result[:limit]
	}

	return result
}
