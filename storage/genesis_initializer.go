package storage

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// RPCClient interface for querying balance from RPC
// This allows using any client implementation that provides BalanceAt method
type RPCClient interface {
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
}

// GenesisInitializingStorage wraps a Storage implementation and provides
// automatic genesis allocation initialization via lazy loading from RPC.
// This wrapper is transparent to the rest of the codebase and only activates
// when querying addresses with no balance history in early blocks.
type GenesisInitializingStorage struct {
	Storage              // Embedded storage (delegates all methods)
	client  RPCClient    // RPC client for querying genesis balances
	logger  *zap.Logger

	// Mutex to prevent concurrent initialization of the same address
	initMutex sync.Mutex
	// Track which addresses we've already tried to initialize (prevent repeated RPC calls)
	initialized map[common.Address]bool
}

// NewGenesisInitializingStorage creates a storage wrapper that automatically
// initializes genesis allocation balances on first query.
//
// Parameters:
//   - storage: The underlying storage implementation (e.g., PebbleStorage)
//   - client: RPC client for querying genesis balances (any client with BalanceAt method)
//   - logger: Logger instance
//
// Returns a wrapped storage that transparently handles genesis initialization.
func NewGenesisInitializingStorage(storage Storage, client RPCClient, logger *zap.Logger) Storage {
	return &GenesisInitializingStorage{
		Storage:     storage,
		client:      client,
		logger:      logger,
		initialized: make(map[common.Address]bool),
	}
}

// GetAddressBalance wraps the underlying GetAddressBalance and adds lazy
// genesis allocation initialization.
//
// Logic:
// 1. Query storage for balance
// 2. If balance is 0 and no history exists and querying early blocks:
//    - Check RPC for genesis balance
//    - Initialize in storage if non-zero
// 3. Return the balance (either from storage or newly initialized)
func (g *GenesisInitializingStorage) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	// First, try to get balance from underlying storage
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}

	balance, err := histReader.GetAddressBalance(ctx, addr, blockNumber)
	if err != nil {
		return nil, err
	}

	// If balance is non-zero, we're done
	if balance.Sign() != 0 {
		return balance, nil
	}

	// Check if we should attempt genesis initialization
	// Only for early blocks (< 1000) to avoid unnecessary checks
	if blockNumber >= 1000 {
		return balance, nil
	}

	// Check if we've already tried to initialize this address
	g.initMutex.Lock()
	alreadyTried := g.initialized[addr]
	if alreadyTried {
		g.initMutex.Unlock()
		return balance, nil
	}
	// Mark as being processed to prevent concurrent attempts
	g.initialized[addr] = true
	g.initMutex.Unlock()

	// Check if there's any balance history for this address
	history, err := histReader.GetBalanceHistory(ctx, addr, 0, blockNumber, 1, 0)
	if err != nil {
		g.logger.Debug("failed to check balance history",
			zap.String("address", addr.Hex()),
			zap.Error(err))
		return balance, nil
	}

	// If there's already history, no need to initialize
	if len(history) > 0 {
		return balance, nil
	}

	// No balance and no history - might be a genesis allocation
	// Query RPC for balance at block 0
	if g.client == nil {
		// No RPC client available, return storage balance
		return balance, nil
	}

	g.logger.Debug("checking genesis balance from RPC",
		zap.String("address", addr.Hex()),
		zap.Uint64("blockNumber", blockNumber))

	// Query RPC for genesis balance (block 0)
	rpcBalance, err := g.client.BalanceAt(ctx, addr, big.NewInt(0))
	if err != nil {
		g.logger.Debug("failed to fetch genesis balance from RPC",
			zap.String("address", addr.Hex()),
			zap.Error(err))
		return balance, nil
	}

	// If RPC also returns 0, this address has no genesis allocation
	if rpcBalance.Sign() == 0 {
		return balance, nil
	}

	// Found a genesis allocation! Initialize in storage
	histWriter, ok := g.Storage.(HistoricalWriter)
	if !ok {
		g.logger.Warn("storage does not implement HistoricalWriter, cannot initialize genesis balance",
			zap.String("address", addr.Hex()))
		return rpcBalance, nil // Return RPC balance even if we can't store it
	}

	g.logger.Info("auto-initializing genesis allocation balance",
		zap.String("address", addr.Hex()),
		zap.String("balance", rpcBalance.String()))

	// Store the genesis balance at block 0
	if err := histWriter.SetBalance(ctx, addr, 0, rpcBalance); err != nil {
		g.logger.Error("failed to initialize genesis balance in storage",
			zap.String("address", addr.Hex()),
			zap.Error(err))
		return rpcBalance, nil // Return RPC balance even if storage write failed
	}

	// Successfully initialized, return the balance
	return rpcBalance, nil
}

// GetBalanceHistory delegates to underlying storage
func (g *GenesisInitializingStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]BalanceSnapshot, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetBalanceHistory(ctx, addr, fromBlock, toBlock, limit, offset)
}

// SetBalance delegates to underlying storage
func (g *GenesisInitializingStorage) SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error {
	histWriter, ok := g.Storage.(HistoricalWriter)
	if !ok {
		return fmt.Errorf("storage does not implement HistoricalWriter")
	}
	return histWriter.SetBalance(ctx, addr, blockNumber, balance)
}

// Delegate all other HistoricalReader methods to underlying storage

func (g *GenesisInitializingStorage) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetBlocksByTimeRange(ctx, fromTime, toTime, limit, offset)
}

func (g *GenesisInitializingStorage) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetBlockByTimestamp(ctx, timestamp)
}

func (g *GenesisInitializingStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *TransactionFilter, limit, offset int) ([]*TransactionWithReceipt, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetTransactionsByAddressFiltered(ctx, addr, filter, limit, offset)
}

func (g *GenesisInitializingStorage) GetBlockCount(ctx context.Context) (uint64, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return 0, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetBlockCount(ctx)
}

func (g *GenesisInitializingStorage) GetTransactionCount(ctx context.Context) (uint64, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return 0, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetTransactionCount(ctx)
}

func (g *GenesisInitializingStorage) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]MinerStats, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetTopMiners(ctx, limit, fromBlock, toBlock)
}

func (g *GenesisInitializingStorage) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]TokenBalance, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetTokenBalances(ctx, addr, tokenType)
}

func (g *GenesisInitializingStorage) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*GasStats, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetGasStatsByBlockRange(ctx, fromBlock, toBlock)
}

func (g *GenesisInitializingStorage) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*AddressGasStats, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetGasStatsByAddress(ctx, addr, fromBlock, toBlock)
}

func (g *GenesisInitializingStorage) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]AddressGasStats, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetTopAddressesByGasUsed(ctx, limit, fromBlock, toBlock)
}

func (g *GenesisInitializingStorage) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]AddressActivityStats, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetTopAddressesByTxCount(ctx, limit, fromBlock, toBlock)
}

func (g *GenesisInitializingStorage) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*NetworkMetrics, error) {
	histReader, ok := g.Storage.(HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement HistoricalReader")
	}
	return histReader.GetNetworkMetrics(ctx, fromTime, toTime)
}

// Note: All Storage interface methods are automatically delegated
// to the embedded Storage field via Go's embedding mechanism. This includes:
// - GetBlock, SetBlock, GetBlockByHash
// - GetTransaction, SetTransaction
// - GetReceipt, SetReceipt
// - GetLatestHeight, SetLatestHeight
// - GetLog, GetLogs
// - GetContract, SetContract
// - GetContractCreation, SetContractCreation
// - Close
// - All other storage operations
//
// The wrapper only intercepts GetAddressBalance to add genesis initialization logic.
