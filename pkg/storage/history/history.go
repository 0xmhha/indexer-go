// Package history computes the statistics of port.HistoricalReader from the
// storage ports (blocks, receipts, the address transaction list), so every
// store gives the same answers (refactoring plan R4-2). The stores keep what
// depends on their layout: the time index, balance history and paging.
package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// DefaultLimit is the number of entries the top lists return when they are
// given no limit.
const DefaultLimit = 10

// Source is what the statistics read.
type Source interface {
	port.BlockReader
	GetLatestHeight(ctx context.Context) (uint64, error)
	GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*model.Receipt, error)
}

// TxGasPrice returns the gas price as go-ethereum reports it, which the
// statistics have always used: the fee cap (equal to the gas price of
// legacy and access-list transactions, whose fee cap is their gas price).
func TxGasPrice(tx *model.Transaction) *big.Int {
	if tx.GasFeeCap != nil {
		return tx.GasFeeCap
	}
	return tx.GasPrice
}

// ReceiptGasPrice returns the price a transaction paid per gas: the
// receipt's effective gas price, or the transaction's gas price when the
// receipt has none.
func ReceiptGasPrice(r *model.Receipt, tx *model.Transaction) *big.Int {
	if r != nil && r.EffectiveGasPrice != nil {
		return r.EffectiveGasPrice
	}
	return TxGasPrice(tx)
}

// checkRange rejects a block range that ends before it starts.
func checkRange(fromBlock, toBlock uint64) error {
	if fromBlock > toBlock {
		return fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}
	return nil
}

// eachBlock calls fn with every stored block in [fromBlock, toBlock] and its
// receipts; blocks or receipts that cannot be read are skipped.
func eachBlock(ctx context.Context, s Source, fromBlock, toBlock uint64, fn func(height uint64, block *model.Block, receipts []*model.Receipt)) {
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err == nil {
			receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
			if err == nil {
				fn(height, block, receipts)
			}
		}
		if height == toBlock {
			break // toBlock may be the largest height
		}
	}
}

// ========== Gas statistics ==========

// GasStatsByBlockRange implements HistoricalReader.GetGasStatsByBlockRange.
func GasStatsByBlockRange(ctx context.Context, s Source, fromBlock, toBlock uint64) (*port.GasStats, error) {
	if err := checkRange(fromBlock, toBlock); err != nil {
		return nil, err
	}

	stats := &port.GasStats{AverageGasPrice: big.NewInt(0)}
	totalGasPrice := big.NewInt(0)
	gasPriceCount := uint64(0)

	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err == nil {
			stats.BlockCount++
			stats.TotalGasLimit += block.GasLimit
			stats.TotalGasUsed += block.GasUsed

			if receipts, err := s.GetReceiptsByBlockNumber(ctx, height); err == nil {
				stats.TransactionCount += uint64(len(receipts))
				for i, tx := range block.Transactions {
					if i < len(receipts) {
						if gasPrice := TxGasPrice(tx); gasPrice != nil && gasPrice.Sign() > 0 {
							totalGasPrice.Add(totalGasPrice, gasPrice)
							gasPriceCount++
						}
					}
				}
			}
		}
		if height == toBlock {
			break
		}
	}

	if stats.BlockCount > 0 {
		stats.AverageGasUsed = stats.TotalGasUsed / stats.BlockCount
	}
	if gasPriceCount > 0 {
		stats.AverageGasPrice.Div(totalGasPrice, new(big.Int).SetUint64(gasPriceCount))
	}
	return stats, nil
}

// addGas adds a sent transaction's gas and fee (at the price it paid) to
// stats.
func addGas(stats *port.AddressGasStats, tx *model.Transaction, receipt *model.Receipt) {
	stats.TotalGasUsed += receipt.GasUsed
	stats.TransactionCount++
	if gasPrice := ReceiptGasPrice(receipt, tx); gasPrice != nil {
		fee := new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), gasPrice)
		stats.TotalFeesPaid.Add(stats.TotalFeesPaid, fee)
	}
}

// GasStatsByAddress implements HistoricalReader.GetGasStatsByAddress: the
// transactions addr sent in the range.
func GasStatsByAddress(ctx context.Context, s Source, addr common.Address, fromBlock, toBlock uint64) (*port.AddressGasStats, error) {
	if err := checkRange(fromBlock, toBlock); err != nil {
		return nil, err
	}
	stats := &port.AddressGasStats{Address: addr, TotalFeesPaid: big.NewInt(0)}
	eachBlock(ctx, s, fromBlock, toBlock, func(_ uint64, block *model.Block, receipts []*model.Receipt) {
		for i, tx := range block.Transactions {
			if tx.From == addr && i < len(receipts) {
				addGas(stats, tx, receipts[i])
			}
		}
	})
	if stats.TransactionCount > 0 {
		stats.AverageGasPerTx = stats.TotalGasUsed / stats.TransactionCount
	}
	return stats, nil
}

// TopAddressesByGasUsed implements HistoricalReader.GetTopAddressesByGasUsed:
// senders by gas used, most first.
func TopAddressesByGasUsed(ctx context.Context, s Source, limit int, fromBlock, toBlock uint64) ([]port.AddressGasStats, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if err := checkRange(fromBlock, toBlock); err != nil {
		return nil, err
	}

	bySender := make(map[common.Address]*port.AddressGasStats)
	eachBlock(ctx, s, fromBlock, toBlock, func(_ uint64, block *model.Block, receipts []*model.Receipt) {
		for i, tx := range block.Transactions {
			if i >= len(receipts) {
				continue
			}
			stats, ok := bySender[tx.From]
			if !ok {
				stats = &port.AddressGasStats{Address: tx.From, TotalFeesPaid: big.NewInt(0)}
				bySender[tx.From] = stats
			}
			addGas(stats, tx, receipts[i])
		}
	})

	result := make([]port.AddressGasStats, 0, len(bySender))
	for _, stats := range bySender {
		if stats.TransactionCount > 0 {
			stats.AverageGasPerTx = stats.TotalGasUsed / stats.TransactionCount
		}
		result = append(result, *stats)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].TotalGasUsed > result[j].TotalGasUsed })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// TopAddressesByTxCount implements HistoricalReader.GetTopAddressesByTxCount:
// senders by transactions sent, most first.
func TopAddressesByTxCount(ctx context.Context, s Source, limit int, fromBlock, toBlock uint64) ([]port.AddressActivityStats, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if err := checkRange(fromBlock, toBlock); err != nil {
		return nil, err
	}

	bySender := make(map[common.Address]*port.AddressActivityStats)
	eachBlock(ctx, s, fromBlock, toBlock, func(height uint64, block *model.Block, receipts []*model.Receipt) {
		for i, tx := range block.Transactions {
			if i >= len(receipts) {
				continue
			}
			stats, ok := bySender[tx.From]
			if !ok {
				stats = &port.AddressActivityStats{Address: tx.From, FirstActivityBlock: height}
				bySender[tx.From] = stats
			}
			stats.TransactionCount++
			stats.TotalGasUsed += receipts[i].GasUsed
			stats.LastActivityBlock = max(stats.LastActivityBlock, height)
			stats.FirstActivityBlock = min(stats.FirstActivityBlock, height)
		}
	})

	result := make([]port.AddressActivityStats, 0, len(bySender))
	for _, stats := range bySender {
		result = append(result, *stats)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].TransactionCount > result[j].TransactionCount })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// ========== Miners ==========

// TopMiners implements HistoricalReader.GetTopMiners: blocks per miner in
// the range (all blocks when both bounds are 0; the range stops at the
// head), their share, the last block, and the fees earned (gas used * gas
// price), most blocks first.
func TopMiners(ctx context.Context, s Source, limit int, fromBlock, toBlock uint64) ([]port.MinerStats, error) {
	latest, err := s.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return []port.MinerStats{}, nil
		}
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}
	end := toBlock
	if toBlock == 0 || toBlock > latest {
		end = latest
	}
	if fromBlock > end {
		return []port.MinerStats{}, nil
	}

	byMiner := make(map[common.Address]*port.MinerStats)
	totalBlocks := uint64(0)
	for height := fromBlock; height <= end; height++ {
		block, err := s.GetBlock(ctx, height)
		if err == nil {
			totalBlocks++
			stats, ok := byMiner[block.Miner]
			if !ok {
				stats = &port.MinerStats{Address: block.Miner, TotalRewards: big.NewInt(0)}
				byMiner[block.Miner] = stats
			}
			stats.BlockCount++
			if height > stats.LastBlockNumber {
				stats.LastBlockNumber = height
				stats.LastBlockTime = block.Time
			}
			addBlockFees(ctx, s, block, stats)
		}
		if height == end {
			break
		}
	}

	if limit <= 0 {
		limit = DefaultLimit
	}
	result := make([]port.MinerStats, 0, len(byMiner))
	for _, stats := range byMiner {
		stats.Percentage = float64(stats.BlockCount) / float64(totalBlocks) * 100.0
		result = append(result, *stats)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].BlockCount > result[j].BlockCount })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// addBlockFees adds the fees of a block's transactions (gas used * gas
// price) to the miner's rewards.
func addBlockFees(ctx context.Context, s Source, block *model.Block, stats *port.MinerStats) {
	// The receipts carry the hashes the chain reports, as the model does.
	prices := make(map[common.Hash]*big.Int, len(block.Transactions))
	for _, tx := range block.Transactions {
		if tx.GasPrice != nil {
			prices[tx.Hash] = tx.GasPrice
		}
	}
	receipts, err := s.GetReceiptsByBlockNumber(ctx, block.Number)
	if err != nil {
		return
	}
	for _, receipt := range receipts {
		if gasPrice, ok := prices[receipt.TxHash]; ok && receipt.GasUsed > 0 {
			fee := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(receipt.GasUsed))
			stats.TotalRewards.Add(stats.TotalRewards, fee)
		}
	}
}

// ========== Network metrics ==========

// NetworkMetrics implements HistoricalReader.GetNetworkMetrics over the
// blocks of the time range, which blocks yields in time order.
func NetworkMetrics(ctx context.Context, fromTime, toTime uint64, blocks func(yield func(*model.Block) error) error) (*port.NetworkMetrics, error) {
	if fromTime > toTime {
		return nil, fmt.Errorf("fromTime (%d) cannot be greater than toTime (%d)", fromTime, toTime)
	}

	metrics := &port.NetworkMetrics{TimePeriod: toTime - fromTime}
	totalGasUsed := uint64(0)
	var firstBlockTime, lastBlockTime uint64
	err := blocks(func(block *model.Block) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if metrics.TotalBlocks == 0 {
			firstBlockTime = block.Time
		}
		lastBlockTime = block.Time
		metrics.TotalBlocks++
		metrics.TotalTransactions += uint64(len(block.Transactions))
		totalGasUsed += block.GasUsed
		return nil
	})
	if err != nil {
		return nil, err
	}
	if metrics.TotalBlocks == 0 {
		return metrics, nil
	}

	metrics.AverageBlockSize = totalGasUsed / metrics.TotalBlocks
	if metrics.TotalBlocks > 1 {
		if timeDiff := lastBlockTime - firstBlockTime; timeDiff > 0 {
			metrics.BlockTime = float64(timeDiff) / float64(metrics.TotalBlocks-1)
			metrics.TPS = float64(metrics.TotalTransactions) / float64(timeDiff)
		}
	}
	return metrics, nil
}

// ========== Token balances ==========

// transferTopic is the topic of ERC-20 and ERC-721 Transfer events.
var transferTopic = common.HexToHash(port.ERC20TransferTopic)

// TokenBalances implements HistoricalReader.GetTokenBalances: the net amount
// of the Transfer logs to and from addr per contract over every stored
// block, positive balances only. describe fills in the token's name,
// symbol, decimals, type and metadata as the store knows them; the type
// filter applies after it (ERC20 by default).
func TokenBalances(ctx context.Context, s Source, addr common.Address, tokenType string, describe func(context.Context, *port.TokenBalance)) ([]port.TokenBalance, error) {
	latest, err := s.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return []port.TokenBalance{}, nil
		}
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	balances := make(map[common.Address]*big.Int)
	for height := uint64(0); height <= latest; height++ {
		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err == nil {
			for _, receipt := range receipts {
				addTransfers(receipt, addr, balances)
			}
		}
		if height == latest {
			break
		}
	}

	result := make([]port.TokenBalance, 0, len(balances))
	for contract, balance := range balances {
		if balance.Sign() <= 0 {
			continue
		}
		tb := port.TokenBalance{
			ContractAddress: contract,
			TokenType:       string(port.TokenStandardERC20),
			Balance:         balance,
		}
		if describe != nil {
			describe(ctx, &tb)
		}
		if tokenType == "" || tokenType == tb.TokenType {
			result = append(result, tb)
		}
	}
	return result, nil
}

// addTransfers applies a receipt's Transfer logs to and from addr to the
// balances per contract.
func addTransfers(receipt *model.Receipt, addr common.Address, balances map[common.Address]*big.Int) {
	for _, log := range receipt.Logs {
		if len(log.Topics) < 3 || log.Topics[0] != transferTopic || len(log.Data) < 32 {
			continue
		}
		from := common.BytesToAddress(log.Topics[1].Bytes())
		to := common.BytesToAddress(log.Topics[2].Bytes())
		if from != addr && to != addr {
			continue
		}
		value := new(big.Int).SetBytes(log.Data[:32])
		balance, ok := balances[log.Address]
		if !ok {
			balance = big.NewInt(0)
			balances[log.Address] = balance
		}
		if to == addr {
			balance.Add(balance, value)
		} else {
			balance.Sub(balance, value)
		}
	}
}

// ========== Address transactions ==========

// AddressTransaction loads the transaction txHash of addr's transaction list
// with its receipt and location, and reports whether it matches filter
// (port.TransactionFilter.MatchTransaction and the fee delegation filter).
// A transaction that is no longer stored does not match.
func AddressTransaction(ctx context.Context, s port.BlockReader, addr common.Address, filter *port.TransactionFilter, txHash common.Hash) (*port.TransactionWithReceipt, bool, error) {
	tx, location, err := s.GetTransaction(ctx, txHash)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get transaction: %w", err)
	}
	receipt, err := s.GetReceipt(ctx, txHash)
	if err != nil {
		if !errors.Is(err, port.ErrNotFound) {
			return nil, false, fmt.Errorf("failed to get receipt: %w", err)
		}
		receipt = nil // the filter decides what a missing receipt means
	}
	if !filter.MatchTransaction(tx, receipt, location, addr) {
		return nil, false, nil
	}
	if filter.IsFeeDelegated != nil {
		// The fee payer as the chain profile decoded it.
		if _, ok := chains.FeeDelegationOf(tx); ok != *filter.IsFeeDelegated {
			return nil, false, nil
		}
	}
	return &port.TransactionWithReceipt{Transaction: tx, Receipt: receipt, Location: location}, true, nil
}

// AddressStats implements HistoricalReader.GetAddressStats over addr's
// transaction list, which hashes yields in order.
func AddressStats(ctx context.Context, s port.BlockReader, addr common.Address, hashes func(yield func(common.Hash) error) error) (*port.AddressStats, error) {
	stats := &port.AddressStats{
		Address:            addr,
		TotalGasCost:       big.NewInt(0),
		TotalValueSent:     big.NewInt(0),
		TotalValueReceived: big.NewInt(0),
	}
	counterparties := make(map[common.Address]bool)

	err := hashes(func(txHash common.Hash) error {
		// The model keeps the sender and hash the chain reports, and the
		// fee payer of fee delegation transactions.
		tx, location, err := s.GetTransaction(ctx, txHash)
		if err != nil {
			return nil
		}
		receipt, _ := s.GetReceipt(ctx, txHash)
		value := tx.Value
		if value == nil {
			value = new(big.Int)
		}
		succeeded := receipt != nil && receipt.Status == model.ReceiptStatusSuccessful

		stats.TotalTransactions++

		// Sent vs received. A failed transaction moves no value.
		if tx.From == addr {
			stats.SentCount++
			if succeeded {
				stats.TotalValueSent.Add(stats.TotalValueSent, value)
			}
			if tx.To != nil {
				counterparties[*tx.To] = true
			}
		}
		if tx.To != nil && *tx.To == addr {
			stats.ReceivedCount++
			if succeeded {
				stats.TotalValueReceived.Add(stats.TotalValueReceived, value)
			}
			counterparties[tx.From] = true
		}

		if receipt != nil {
			if succeeded {
				stats.SuccessCount++
			} else {
				stats.FailedCount++
			}
			// Gas is counted for the account that paid it: the sender, or
			// the fee payer of a fee delegation transaction.
			if chains.GasPayer(tx) == addr {
				stats.TotalGasUsed += receipt.GasUsed
				if receipt.EffectiveGasPrice != nil {
					cost := new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), receipt.EffectiveGasPrice)
					stats.TotalGasCost.Add(stats.TotalGasCost, cost)
				}
			}
		}

		// Contract interaction: input data and a target address.
		if tx.To != nil && len(tx.Input) > 0 {
			stats.ContractInteractionCount++
		}

		if location != nil {
			if block, err := s.GetBlock(ctx, location.BlockHeight); err == nil && block != nil {
				ts := block.Time
				if stats.FirstTransactionTimestamp == 0 || ts < stats.FirstTransactionTimestamp {
					stats.FirstTransactionTimestamp = ts
				}
				if ts > stats.LastTransactionTimestamp {
					stats.LastTransactionTimestamp = ts
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	stats.UniqueAddressCount = uint64(len(counterparties))
	return stats, nil
}

// TokenMetadataStore is the token metadata a store keeps.
type TokenMetadataStore interface {
	port.TokenMetadataReader
	port.TokenMetadataWriter
}

// DescribeToken fills in a token balance's name, symbol, decimals, type and
// metadata, from the first source that has them: the metadata a chain
// registered (chains.RegisterKnownToken), the stored metadata, or the node
// through fetcher (when not nil), whose answer is stored for later reads.
func DescribeToken(ctx context.Context, tb *port.TokenBalance, store TokenMetadataStore, fetcher port.TokenMetadataFetcher, logger *zap.Logger) {
	contract := tb.ContractAddress
	if known, ok := chains.KnownTokenOf(contract); ok {
		tb.Name = known.Name
		tb.Symbol = known.Symbol
		decimals := known.Decimals
		tb.Decimals = &decimals
		return
	}
	if md, err := store.GetTokenMetadata(ctx, contract); err == nil && md != nil {
		applyMetadata(tb, md)
		return
	}
	if fetcher == nil {
		return
	}
	md, err := fetcher.FetchTokenMetadata(ctx, contract)
	if err != nil || md == nil {
		return
	}
	applyMetadata(tb, md)
	if err := store.SaveTokenMetadata(ctx, md); err != nil {
		logger.Warn("Failed to cache fetched token metadata", zap.String("contract", contract.Hex()), zap.Error(err))
		return
	}
	logger.Info("Cached on-demand fetched token metadata",
		zap.String("contract", contract.Hex()),
		zap.String("name", md.Name),
		zap.String("symbol", md.Symbol),
		zap.Uint8("decimals", md.Decimals),
	)
}

// applyMetadata copies stored or fetched token metadata into tb.
func applyMetadata(tb *port.TokenBalance, md *port.TokenMetadata) {
	tb.Name = md.Name
	tb.Symbol = md.Symbol
	decimals := int(md.Decimals)
	tb.Decimals = &decimals
	if md.Standard != "" {
		tb.TokenType = string(md.Standard)
	}
	tb.Metadata = TokenMetadataJSON(md)
}

// TokenMetadataJSON returns the metadata beyond name, symbol and decimals as
// a JSON object, "" when there is none.
func TokenMetadataJSON(md *port.TokenMetadata) string {
	if md == nil {
		return ""
	}
	m := make(map[string]interface{})
	if md.BaseURI != "" {
		m["baseURI"] = md.BaseURI
	}
	if md.TotalSupply != nil && md.TotalSupply.Sign() > 0 {
		m["totalSupply"] = md.TotalSupply.String()
	}
	if md.SupportsERC165 {
		m["supportsERC165"] = true
	}
	if md.SupportsMetadata {
		m["supportsMetadata"] = true
	}
	if md.SupportsEnumerable {
		m["supportsEnumerable"] = true
	}
	if !md.CreatedAt.IsZero() {
		m["createdAt"] = md.CreatedAt.Format(time.RFC3339)
	}
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}
