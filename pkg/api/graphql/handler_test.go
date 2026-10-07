package graphql

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/userop"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// uint256FromBig converts a big.Int to uint256.Int
func uint256FromBig(b *big.Int) *uint256.Int {
	u, _ := uint256.FromBig(b)
	return u
}

// mockStorage is a mock implementation of storage.Storage for testing
type mockStorage struct {
	latestHeight uint64
	blocks       map[uint64]*types.Block
	blocksByHash map[common.Hash]*types.Block
	transactions map[common.Hash]*types.Transaction
	receipts     map[common.Hash]*types.Receipt
}

func (m *mockStorage) GetLatestHeight(ctx context.Context) (uint64, error) {
	return m.latestHeight, nil
}

func (m *mockStorage) gethGetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	if block, ok := m.blocks[height]; ok {
		return block, nil
	}
	return nil, port.ErrNotFound
}

func (m *mockStorage) gethGetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	if block, ok := m.blocksByHash[hash]; ok {
		return block, nil
	}
	return nil, port.ErrNotFound
}

func (m *mockStorage) gethGetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *port.TxLocation, error) {
	if tx, ok := m.transactions[hash]; ok {
		location := &port.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		return tx, location, nil
	}
	return nil, nil, port.ErrNotFound
}
func (m *mockStorage) gethGetTransactions(ctx context.Context, hashes []common.Hash) ([]*types.Transaction, []*port.TxLocation, error) {
	txs := make([]*types.Transaction, len(hashes))
	locs := make([]*port.TxLocation, len(hashes))
	for i, h := range hashes {
		txs[i], locs[i], _ = m.gethGetTransaction(ctx, h)
	}
	return txs, locs, nil
}

func (m *mockStorage) gethGetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	if receipt, ok := m.receipts[hash]; ok {
		return receipt, nil
	}
	return nil, port.ErrNotFound
}

func (m *mockStorage) offsetGetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	return []common.Hash{}, nil
}

func (m *mockStorage) gethGetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	return []*types.Receipt{}, nil
}

func (m *mockStorage) gethGetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error) {
	blocks := make([]*types.Block, 0, endHeight-startHeight+1)
	for i := startHeight; i <= endHeight; i++ {
		if block, ok := m.blocks[i]; ok {
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

func (m *mockStorage) gethGetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	return []*types.Receipt{}, nil
}

func (m *mockStorage) gethGetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error) {
	return []*types.Receipt{}, nil
}

func (m *mockStorage) HasBlock(ctx context.Context, height uint64) (bool, error) {
	_, ok := m.blocks[height]
	return ok, nil
}

func (m *mockStorage) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	_, ok := m.transactions[hash]
	return ok, nil
}

func (m *mockStorage) HasReceipt(ctx context.Context, hash common.Hash) (bool, error) {
	_, ok := m.receipts[hash]
	return ok, nil
}

func (m *mockStorage) GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error) {
	return nil, nil
}

func (m *mockStorage) SetLatestHeight(ctx context.Context, height uint64) error {
	m.latestHeight = height
	return nil
}

func (m *mockStorage) gethSetBlock(ctx context.Context, block *types.Block) error {
	m.blocks[block.NumberU64()] = block
	m.blocksByHash[block.Hash()] = block
	return nil
}

func (m *mockStorage) SetTransaction(ctx context.Context, tx *types.Transaction, location *port.TxLocation) error {
	m.transactions[tx.Hash()] = tx
	return nil
}

func (m *mockStorage) gethSetReceipt(ctx context.Context, receipt *types.Receipt) error {
	m.receipts[receipt.TxHash] = receipt
	return nil
}

func (m *mockStorage) SetReceipts(ctx context.Context, receipts []*types.Receipt) error {
	for _, receipt := range receipts {
		m.receipts[receipt.TxHash] = receipt
	}
	return nil
}

func (m *mockStorage) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	return nil
}

func (m *mockStorage) SetBlocks(ctx context.Context, blocks []*types.Block) error {
	for _, block := range blocks {
		m.blocks[block.NumberU64()] = block
		m.blocksByHash[block.Hash()] = block
	}
	return nil
}

func (m *mockStorage) DeleteBlock(ctx context.Context, height uint64) error {
	if block, ok := m.blocks[height]; ok {
		delete(m.blocksByHash, block.Hash())
		delete(m.blocks, height)
	}
	return nil
}

func (m *mockStorage) Close() error {
	return nil
}

func (m *mockStorage) Compact(ctx context.Context, start, end []byte) error {
	return nil
}

func (m *mockStorage) SetABI(ctx context.Context, address common.Address, abiJSON []byte) error {
	return nil
}

func (m *mockStorage) GetABI(ctx context.Context, address common.Address) ([]byte, error) {
	return nil, nil
}

func (m *mockStorage) DeleteABI(ctx context.Context, address common.Address) error {
	return nil
}

func (m *mockStorage) ListABIs(ctx context.Context) ([]common.Address, error) {
	return []common.Address{}, nil
}

func (m *mockStorage) HasABI(ctx context.Context, address common.Address) (bool, error) {
	return false, nil
}

func (m *mockStorage) gethGetLogs(ctx context.Context, filter *port.LogFilter) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) gethGetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) gethGetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) gethGetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) SaveLog(ctx context.Context, log *types.Log) error {
	return nil
}

func (m *mockStorage) SaveLogs(ctx context.Context, logs []*types.Log) error {
	return nil
}

func (m *mockStorage) DeleteLogsByBlock(ctx context.Context, blockNumber uint64) error {
	return nil
}

func (m *mockStorage) gethIndexLogs(ctx context.Context, logs []*types.Log) error {
	return nil
}

func (m *mockStorage) gethIndexLog(ctx context.Context, log *types.Log) error {
	return nil
}

func (m *mockStorage) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]port.SearchResult, error) {
	return []port.SearchResult{}, nil
}

// Contract verification methods
func (m *mockStorage) GetContractVerification(ctx context.Context, address common.Address) (*port.ContractVerification, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) IsContractVerified(ctx context.Context, address common.Address) (bool, error) {
	return false, nil
}

func (m *mockStorage) ListVerifiedContracts(ctx context.Context, limit, offset int) ([]common.Address, error) {
	return []common.Address{}, nil
}

func (m *mockStorage) CountVerifiedContracts(ctx context.Context) (int, error) {
	return 0, nil
}

func (m *mockStorage) SetContractVerification(ctx context.Context, verification *port.ContractVerification) error {
	return nil
}

func (m *mockStorage) DeleteContractVerification(ctx context.Context, address common.Address) error {
	return nil
}

func (m *mockStorage) GetAddressStats(ctx context.Context, addr common.Address) (*port.AddressStats, error) {
	return nil, nil
}

// HistoricalReader methods for mockStorage
func (m *mockStorage) gethGetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	return []*types.Block{}, nil
}

func (m *mockStorage) gethGetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *port.TransactionFilter, limit, offset int) ([]*port.TransactionWithReceipt, error) {
	return []*port.TransactionWithReceipt{}, nil
}

func (m *mockStorage) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (m *mockStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]port.BalanceSnapshot, error) {
	return []port.BalanceSnapshot{}, nil
}

func (m *mockStorage) GetBlockCount(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockStorage) GetTransactionCount(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockStorage) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.MinerStats, error) {
	return []port.MinerStats{}, nil
}

func (m *mockStorage) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]port.TokenBalance, error) {
	return []port.TokenBalance{}, nil
}

func (m *mockStorage) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*port.GasStats, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*port.AddressGasStats, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressGasStats, error) {
	return []port.AddressGasStats{}, nil
}

func (m *mockStorage) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressActivityStats, error) {
	return []port.AddressActivityStats{}, nil
}

func (m *mockStorage) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*port.NetworkMetrics, error) {
	return nil, port.ErrNotFound
}

// HistoricalWriter methods for mockStorage
func (m *mockStorage) SetBlockTimestamp(ctx context.Context, timestamp uint64, height uint64) error {
	return nil
}

func (m *mockStorage) UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error {
	return nil
}

func (m *mockStorage) SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error {
	return nil
}

// KVStore methods for mockStorage
func (m *mockStorage) Put(ctx context.Context, key, value []byte) error {
	return nil
}

func (m *mockStorage) Get(ctx context.Context, key []byte) ([]byte, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) Delete(ctx context.Context, key []byte) error {
	return nil
}

func (m *mockStorage) Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	return nil
}

func (m *mockStorage) Has(ctx context.Context, key []byte) (bool, error) {
	return false, nil
}

// TokenMetadataReader methods for mockStorage
func (m *mockStorage) GetTokenMetadata(ctx context.Context, address common.Address) (*port.TokenMetadata, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorage) ListTokensByStandard(ctx context.Context, standard port.TokenStandard, limit, offset int) ([]*port.TokenMetadata, error) {
	return []*port.TokenMetadata{}, nil
}

func (m *mockStorage) GetTokensCount(ctx context.Context, standard port.TokenStandard) (int, error) {
	return 0, nil
}

func (m *mockStorage) SearchTokens(ctx context.Context, query string, limit int) ([]*port.TokenMetadata, error) {
	return []*port.TokenMetadata{}, nil
}

// TokenMetadataWriter methods for mockStorage
func (m *mockStorage) SaveTokenMetadata(ctx context.Context, metadata *port.TokenMetadata) error {
	return nil
}

func (m *mockStorage) DeleteTokenMetadata(ctx context.Context, address common.Address) error {
	return nil
}

func (m *mockStorage) SetTokenMetadataFetcher(fetcher port.TokenMetadataFetcher) {
}

// SetCodeIndexWriter stubs (Reader stubs are in resolvers_extended_test.go)
func (m *mockStorage) SaveSetCodeAuthorization(ctx context.Context, record *port.SetCodeAuthorizationRecord) error {
	return nil
}
func (m *mockStorage) SaveSetCodeAuthorizations(ctx context.Context, records []*port.SetCodeAuthorizationRecord) error {
	return nil
}
func (m *mockStorage) UpdateAddressDelegationState(ctx context.Context, state *port.AddressDelegationState) error {
	return nil
}
func (m *mockStorage) IncrementSetCodeStats(ctx context.Context, address common.Address, asTarget, asAuthority bool, blockNumber uint64) error {
	return nil
}

// UserOpIndexReader stubs
func (m *mockStorage) GetUserOp(ctx context.Context, opHash common.Hash) (*userop.UserOperation, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetUserOpsByTx(ctx context.Context, txHash common.Hash) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpsBySender(ctx context.Context, sender common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpsByBundler(ctx context.Context, bundler common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpsByBlock(ctx context.Context, blockNumber uint64) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpsByFactory(ctx context.Context, factory common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetBundlerStats(ctx context.Context, bundler common.Address) (*userop.BundlerStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetFactoryStats(ctx context.Context, factory common.Address) (*userop.FactoryStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetPaymasterStats(ctx context.Context, paymaster common.Address) (*userop.PaymasterStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetSmartAccount(ctx context.Context, address common.Address) (*userop.SmartAccount, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetRecentUserOps(ctx context.Context, limit int) ([]*userop.UserOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUserOpCount(ctx context.Context) (int, error) {
	return 0, nil
}
func (m *mockStorage) ListBundlers(ctx context.Context, limit, offset int) ([]*userop.BundlerStats, error) {
	return nil, nil
}
func (m *mockStorage) ListFactories(ctx context.Context, limit, offset int) ([]*userop.FactoryStats, error) {
	return nil, nil
}
func (m *mockStorage) ListPaymasters(ctx context.Context, limit, offset int) ([]*userop.PaymasterStats, error) {
	return nil, nil
}
func (m *mockStorage) ListSmartAccounts(ctx context.Context, limit, offset int) ([]*userop.SmartAccount, error) {
	return nil, nil
}

// UserOpIndexWriter stubs
func (m *mockStorage) SaveUserOp(ctx context.Context, op *userop.UserOperation) error {
	return nil
}
func (m *mockStorage) SaveUserOps(ctx context.Context, ops []*userop.UserOperation) error {
	return nil
}
func (m *mockStorage) UpdateBundlerStats(ctx context.Context, stats *userop.BundlerStats) error {
	return nil
}
func (m *mockStorage) UpdateFactoryStats(ctx context.Context, stats *userop.FactoryStats) error {
	return nil
}
func (m *mockStorage) UpdatePaymasterStats(ctx context.Context, stats *userop.PaymasterStats) error {
	return nil
}
func (m *mockStorage) SaveSmartAccount(ctx context.Context, account *userop.SmartAccount) error {
	return nil
}

// ModuleIndexReader stubs
func (m *mockStorage) GetInstalledModule(ctx context.Context, account, module common.Address) (*port.InstalledModule, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetModulesByAccount(ctx context.Context, account common.Address, limit, offset int) ([]*port.InstalledModule, error) {
	return nil, nil
}
func (m *mockStorage) GetModulesByType(ctx context.Context, moduleType port.ModuleType, limit, offset int) ([]*port.InstalledModule, error) {
	return nil, nil
}
func (m *mockStorage) GetModuleStats(ctx context.Context, module common.Address) (*port.ModuleStats, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetAccountModules(ctx context.Context, account common.Address) (*port.AccountModules, error) {
	return nil, port.ErrNotFound
}
func (m *mockStorage) GetRecentModuleEvents(ctx context.Context, limit int) ([]*port.InstalledModule, error) {
	return nil, nil
}
func (m *mockStorage) GetModuleEventCount(ctx context.Context) (int, error) {
	return 0, nil
}
func (m *mockStorage) ListModuleStats(ctx context.Context, limit, offset int) ([]*port.ModuleStats, error) {
	return nil, nil
}

// ModuleIndexWriter stubs
func (m *mockStorage) SaveInstalledModule(ctx context.Context, record *port.InstalledModule) error {
	return nil
}
func (m *mockStorage) RemoveModule(ctx context.Context, account, module common.Address, blockNumber uint64, txHash common.Hash) error {
	return nil
}
func (m *mockStorage) UpdateModuleStats(ctx context.Context, stats *port.ModuleStats) error {
	return nil
}

// mockStorageWithErrors returns errors for testing error paths
type mockStorageWithErrors struct {
}

func (m *mockStorageWithErrors) GetLatestHeight(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) gethGetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *port.TxLocation, error) {
	return nil, nil, port.ErrNotFound
}
func (m *mockStorageWithErrors) gethGetTransactions(ctx context.Context, hashes []common.Hash) ([]*types.Transaction, []*port.TxLocation, error) {
	return nil, nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) offsetGetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) HasBlock(ctx context.Context, height uint64) (bool, error) {
	return false, port.ErrNotFound
}

func (m *mockStorageWithErrors) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	return false, port.ErrNotFound
}

func (m *mockStorageWithErrors) HasReceipt(ctx context.Context, hash common.Hash) (bool, error) {
	return false, port.ErrNotFound
}

func (m *mockStorageWithErrors) GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) SetLatestHeight(ctx context.Context, height uint64) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) gethSetBlock(ctx context.Context, block *types.Block) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SetTransaction(ctx context.Context, tx *types.Transaction, location *port.TxLocation) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) gethSetReceipt(ctx context.Context, receipt *types.Receipt) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SetReceipts(ctx context.Context, receipts []*types.Receipt) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SetBlocks(ctx context.Context, blocks []*types.Block) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteBlock(ctx context.Context, height uint64) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) Close() error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) Compact(ctx context.Context, start, end []byte) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SetABI(ctx context.Context, address common.Address, abiJSON []byte) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) GetABI(ctx context.Context, address common.Address) ([]byte, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteABI(ctx context.Context, address common.Address) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) ListABIs(ctx context.Context) ([]common.Address, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) HasABI(ctx context.Context, address common.Address) (bool, error) {
	return false, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetLogs(ctx context.Context, filter *port.LogFilter) ([]*types.Log, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) gethGetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) SaveLog(ctx context.Context, log *types.Log) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) SaveLogs(ctx context.Context, logs []*types.Log) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteLogsByBlock(ctx context.Context, blockNumber uint64) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) gethIndexLogs(ctx context.Context, logs []*types.Log) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) gethIndexLog(ctx context.Context, log *types.Log) error {
	return port.ErrNotFound
}

func (m *mockStorageWithErrors) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]port.SearchResult, error) {
	return nil, port.ErrNotFound
}

// Contract verification methods
func (m *mockStorageWithErrors) GetContractVerification(ctx context.Context, address common.Address) (*port.ContractVerification, error) {
	return nil, port.ErrNotFound
}

func (m *mockStorageWithErrors) IsContractVerified(ctx context.Context, address common.Address) (bool, error) {
	return false, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) ListVerifiedContracts(ctx context.Context, limit, offset int) ([]common.Address, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) CountVerifiedContracts(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) SetContractVerification(ctx context.Context, verification *port.ContractVerification) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) DeleteContractVerification(ctx context.Context, address common.Address) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetAddressStats(ctx context.Context, addr common.Address) (*port.AddressStats, error) {
	return nil, fmt.Errorf("storage error")
}

// HistoricalReader methods for mockStorageWithErrors
func (m *mockStorageWithErrors) gethGetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) gethGetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *port.TransactionFilter, limit, offset int) ([]*port.TransactionWithReceipt, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]port.BalanceSnapshot, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBlockCount(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTransactionCount(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.MinerStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]port.TokenBalance, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*port.GasStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*port.AddressGasStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressGasStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]port.AddressActivityStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*port.NetworkMetrics, error) {
	return nil, fmt.Errorf("storage error")
}

// HistoricalWriter methods for mockStorageWithErrors
func (m *mockStorageWithErrors) SetBlockTimestamp(ctx context.Context, timestamp uint64, height uint64) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error {
	return fmt.Errorf("storage error")
}

// KVStore methods for mockStorageWithErrors
func (m *mockStorageWithErrors) Put(ctx context.Context, key, value []byte) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) Get(ctx context.Context, key []byte) ([]byte, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) Delete(ctx context.Context, key []byte) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) Has(ctx context.Context, key []byte) (bool, error) {
	return false, fmt.Errorf("storage error")
}

// TokenMetadataReader methods for mockStorageWithErrors
func (m *mockStorageWithErrors) GetTokenMetadata(ctx context.Context, address common.Address) (*port.TokenMetadata, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) ListTokensByStandard(ctx context.Context, standard port.TokenStandard, limit, offset int) ([]*port.TokenMetadata, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTokensCount(ctx context.Context, standard port.TokenStandard) (int, error) {
	return 0, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) SearchTokens(ctx context.Context, query string, limit int) ([]*port.TokenMetadata, error) {
	return nil, fmt.Errorf("storage error")
}

// TokenMetadataWriter methods for mockStorageWithErrors
func (m *mockStorageWithErrors) SaveTokenMetadata(ctx context.Context, metadata *port.TokenMetadata) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) DeleteTokenMetadata(ctx context.Context, address common.Address) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) SetTokenMetadataFetcher(fetcher port.TokenMetadataFetcher) {
}

// SetCodeIndexReader stubs
func (m *mockStorageWithErrors) GetSetCodeAuthorization(ctx context.Context, txHash common.Hash, authIndex int) (*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetSetCodeAuthorizationsByTx(ctx context.Context, txHash common.Hash) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, limit, offset int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, limit, offset int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetSetCodeAuthorizationsByBlock(ctx context.Context, blockNumber uint64) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetAddressSetCodeStats(ctx context.Context, address common.Address) (*port.AddressSetCodeStats, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetAddressDelegationState(ctx context.Context, address common.Address) (*port.AddressDelegationState, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetSetCodeAuthorizationsCountByTarget(ctx context.Context, target common.Address) (int, error) {
	return 0, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetSetCodeAuthorizationsCountByAuthority(ctx context.Context, authority common.Address) (int, error) {
	return 0, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetSetCodeTransactionCount(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetRecentSetCodeAuthorizations(ctx context.Context, limit int) ([]*port.SetCodeAuthorizationRecord, error) {
	return nil, fmt.Errorf("storage error")
}

// SetCodeIndexWriter stubs
func (m *mockStorageWithErrors) SaveSetCodeAuthorization(ctx context.Context, record *port.SetCodeAuthorizationRecord) error {
	return fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) SaveSetCodeAuthorizations(ctx context.Context, records []*port.SetCodeAuthorizationRecord) error {
	return fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) UpdateAddressDelegationState(ctx context.Context, state *port.AddressDelegationState) error {
	return fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) IncrementSetCodeStats(ctx context.Context, address common.Address, asTarget, asAuthority bool, blockNumber uint64) error {
	return fmt.Errorf("storage error")
}

// UserOpIndexReader stubs for mockStorageWithErrors
func (m *mockStorageWithErrors) GetUserOp(ctx context.Context, opHash common.Hash) (*userop.UserOperation, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetUserOpsByTx(ctx context.Context, txHash common.Hash) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetUserOpsBySender(ctx context.Context, sender common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetUserOpsByBundler(ctx context.Context, bundler common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetUserOpsByBlock(ctx context.Context, blockNumber uint64) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetUserOpsByFactory(ctx context.Context, factory common.Address, limit, offset int) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetBundlerStats(ctx context.Context, bundler common.Address) (*userop.BundlerStats, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetFactoryStats(ctx context.Context, factory common.Address) (*userop.FactoryStats, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetPaymasterStats(ctx context.Context, paymaster common.Address) (*userop.PaymasterStats, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetSmartAccount(ctx context.Context, address common.Address) (*userop.SmartAccount, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetRecentUserOps(ctx context.Context, limit int) ([]*userop.UserOperation, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetUserOpCount(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) ListBundlers(ctx context.Context, limit, offset int) ([]*userop.BundlerStats, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) ListFactories(ctx context.Context, limit, offset int) ([]*userop.FactoryStats, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) ListPaymasters(ctx context.Context, limit, offset int) ([]*userop.PaymasterStats, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) ListSmartAccounts(ctx context.Context, limit, offset int) ([]*userop.SmartAccount, error) {
	return nil, fmt.Errorf("storage error")
}

// UserOpIndexWriter stubs for mockStorageWithErrors
func (m *mockStorageWithErrors) SaveUserOp(ctx context.Context, op *userop.UserOperation) error {
	return fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) SaveUserOps(ctx context.Context, ops []*userop.UserOperation) error {
	return fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) UpdateBundlerStats(ctx context.Context, stats *userop.BundlerStats) error {
	return fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) UpdateFactoryStats(ctx context.Context, stats *userop.FactoryStats) error {
	return fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) UpdatePaymasterStats(ctx context.Context, stats *userop.PaymasterStats) error {
	return fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) SaveSmartAccount(ctx context.Context, account *userop.SmartAccount) error {
	return fmt.Errorf("storage error")
}

// ModuleIndexReader stubs for mockStorageWithErrors
func (m *mockStorageWithErrors) GetInstalledModule(ctx context.Context, account, module common.Address) (*port.InstalledModule, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetModulesByAccount(ctx context.Context, account common.Address, limit, offset int) ([]*port.InstalledModule, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetModulesByType(ctx context.Context, moduleType port.ModuleType, limit, offset int) ([]*port.InstalledModule, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetModuleStats(ctx context.Context, module common.Address) (*port.ModuleStats, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetAccountModules(ctx context.Context, account common.Address) (*port.AccountModules, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetRecentModuleEvents(ctx context.Context, limit int) ([]*port.InstalledModule, error) {
	return nil, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) GetModuleEventCount(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) ListModuleStats(ctx context.Context, limit, offset int) ([]*port.ModuleStats, error) {
	return nil, fmt.Errorf("storage error")
}

// ModuleIndexWriter stubs for mockStorageWithErrors
func (m *mockStorageWithErrors) SaveInstalledModule(ctx context.Context, record *port.InstalledModule) error {
	return fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) RemoveModule(ctx context.Context, account, module common.Address, blockNumber uint64, txHash common.Hash) error {
	return fmt.Errorf("storage error")
}
func (m *mockStorageWithErrors) UpdateModuleStats(ctx context.Context, stats *port.ModuleStats) error {
	return fmt.Errorf("storage error")
}

func TestGraphQLHandler(t *testing.T) {
	logger := zap.NewNop()

	// Create test block
	header := &types.Header{
		Number:     common.Big1,
		ParentHash: common.HexToHash("0x123"),
		Time:       123456,
		GasLimit:   8000000,
		GasUsed:    5000000,
	}
	testBlock := types.NewBlockWithHeader(header)

	store := &mockStorage{
		latestHeight: 100,
		blocks:       map[uint64]*types.Block{1: testBlock},
		blocksByHash: map[common.Hash]*types.Block{testBlock.Hash(): testBlock},
		transactions: make(map[common.Hash]*types.Transaction),
		receipts:     make(map[common.Hash]*types.Receipt),
	}

	handler, err := NewHandler(store, logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	t.Run("GraphQLEndpoint", func(t *testing.T) {
		query := `{"query":"{ latestHeight }"}`
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(query))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status OK, got %v", w.Code)
		}
	})

	t.Run("GraphQLEndpoint_InvalidJSON", func(t *testing.T) {
		query := `invalid json`
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(query))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status OK even with invalid JSON, got %v", w.Code)
		}
	})

	t.Run("GraphQLEndpoint_GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// GraphQL handler should handle GET requests too
		if w.Code != http.StatusOK {
			t.Errorf("expected status OK, got %v", w.Code)
		}
	})

	t.Run("PlaygroundEndpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/playground", nil)
		w := httptest.NewRecorder()

		playgroundHandler := handler.PlaygroundHandler()
		playgroundHandler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status OK, got %v", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "GraphQL Playground") {
			t.Error("expected GraphQL Playground HTML")
		}
	})

	t.Run("ExecuteQuery_LatestHeight", func(t *testing.T) {
		query := `{ latestHeight }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}

		if result.Data == nil {
			t.Error("expected data in result")
		}
	})

	t.Run("ExecuteQueryJSON", func(t *testing.T) {
		query := `{ latestHeight }`
		jsonBytes, err := handler.ExecuteQueryJSON(query, nil)
		if err != nil {
			t.Fatalf("failed to execute query JSON: %v", err)
		}

		if len(jsonBytes) == 0 {
			t.Error("expected JSON response")
		}
	})
}

func TestGraphQLSchema(t *testing.T) {
	logger := zap.NewNop()
	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	schema, err := NewSchema(store, logger)
	if err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	s := schema.Schema()
	if s.QueryType() == nil {
		t.Error("expected query type in schema")
	}

	// Test that schema has all expected query fields
	queryFields := s.QueryType().Fields()
	expectedFields := []string{
		"latestHeight", "block", "blockByHash", "blocks",
		"transaction", "transactions", "transactionsByAddress",
		"receipt", "receiptsByBlock", "logs",
	}
	for _, field := range expectedFields {
		if _, exists := queryFields[field]; !exists {
			t.Errorf("expected query field %s to exist", field)
		}
	}
}

func TestGraphQLTypes(t *testing.T) {
	// Test type initialization
	if blockType == nil {
		t.Error("blockType should be initialized")
	}
	if transactionType == nil {
		t.Error("transactionType should be initialized")
	}
	if receiptType == nil {
		t.Error("receiptType should be initialized")
	}
	if logType == nil {
		t.Error("logType should be initialized")
	}
	if pageInfoType == nil {
		t.Error("pageInfoType should be initialized")
	}
	if blockConnectionType == nil {
		t.Error("blockConnectionType should be initialized")
	}
	if transactionConnectionType == nil {
		t.Error("transactionConnectionType should be initialized")
	}
	if logConnectionType == nil {
		t.Error("logConnectionType should be initialized")
	}
	if bigIntType == nil {
		t.Error("bigIntType should be initialized")
	}
	if hashType == nil {
		t.Error("hashType should be initialized")
	}
	if addressType == nil {
		t.Error("addressType should be initialized")
	}
	if bytesType == nil {
		t.Error("bytesType should be initialized")
	}
}

func TestGraphQLResolvers(t *testing.T) {
	logger := zap.NewNop()

	// Create test block
	header := &types.Header{
		Number:     common.Big1,
		ParentHash: common.HexToHash("0x123"),
		Time:       123456,
		GasLimit:   8000000,
		GasUsed:    5000000,
	}
	testBlock := types.NewBlockWithHeader(header)

	// Create test transaction
	testTx := types.NewTransaction(
		0,
		common.HexToAddress("0x456"),
		common.Big1,
		21000,
		common.Big1,
		nil,
	)

	// Create test receipt
	testReceipt := &types.Receipt{
		TxHash:            testTx.Hash(),
		Status:            1,
		CumulativeGasUsed: 21000,
		GasUsed:           21000,
		Logs:              []*types.Log{},
		BlockNumber:       common.Big1,
		BlockHash:         testBlock.Hash(),
		EffectiveGasPrice: common.Big1,
	}

	store := &mockStorage{
		latestHeight: 100,
		blocks:       map[uint64]*types.Block{1: testBlock},
		blocksByHash: map[common.Hash]*types.Block{testBlock.Hash(): testBlock},
		transactions: map[common.Hash]*types.Transaction{testTx.Hash(): testTx},
		receipts:     map[common.Hash]*types.Receipt{testTx.Hash(): testReceipt},
	}

	handler, err := NewHandler(store, logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	t.Run("ResolveBlock_Success", func(t *testing.T) {
		query := `{ block(number: "1") { number hash } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
		if result.Data == nil {
			t.Error("expected data in result")
		}
	})

	t.Run("ResolveBlock_NotFound", func(t *testing.T) {
		query := `{ block(number: "999") { number } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for non-existent block")
		}
	})

	t.Run("ResolveBlockByHash_Success", func(t *testing.T) {
		query := `{ blockByHash(hash: "` + testBlock.Hash().Hex() + `") { number hash } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveBlockByHash_NotFound", func(t *testing.T) {
		query := `{ blockByHash(hash: "0x0000000000000000000000000000000000000000000000000000000000000000") { number } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for non-existent block")
		}
	})

	t.Run("ResolveTransaction_Success", func(t *testing.T) {
		query := `{ transaction(hash: "` + testTx.Hash().Hex() + `") { hash } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveTransaction_NotFound", func(t *testing.T) {
		query := `{ transaction(hash: "0x0000000000000000000000000000000000000000000000000000000000000000") { hash } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for non-existent transaction")
		}
	})

	t.Run("ResolveReceipt_Success", func(t *testing.T) {
		query := `{ receipt(transactionHash: "` + testReceipt.TxHash.Hex() + `") { status } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveReceiptsByBlock", func(t *testing.T) {
		query := `{ receiptsByBlock(blockNumber: "1") { status } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveTransactionsByAddress", func(t *testing.T) {
		query := `{ transactionsByAddress(address: "0x456") { nodes { hash } pageInfo { hasNextPage } } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("InvalidQuery", func(t *testing.T) {
		query := `{ invalid }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for invalid query")
		}
	})

	t.Run("ComplexQuery_BlockWithTransactions", func(t *testing.T) {
		query := `{
			block(number: "1") {
				number
				hash
				transactions {
					hash
					from
				}
			}
		}`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveBlocks", func(t *testing.T) {
		query := `{ blocks { nodes { number hash } totalCount pageInfo { hasNextPage } } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveTransactions", func(t *testing.T) {
		query := `{ transactions { nodes { hash } totalCount pageInfo { hasNextPage } } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveLogs", func(t *testing.T) {
		query := `{ logs(filter: {}) { nodes { address } totalCount pageInfo { hasNextPage } } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("ResolveBlock_InvalidNumber", func(t *testing.T) {
		query := `{ block(number: "invalid") { number } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for invalid block number")
		}
	})

	t.Run("ResolveBlockByHash_InvalidHash", func(t *testing.T) {
		query := `{ blockByHash(hash: "invalid") { number } }`
		result := handler.ExecuteQuery(query, nil)

		// Should handle invalid hash gracefully - error is acceptable
		_ = result.Errors
	})

	t.Run("ResolveTransaction_InvalidHash", func(t *testing.T) {
		query := `{ transaction(hash: "invalid") { hash } }`
		result := handler.ExecuteQuery(query, nil)

		// Should handle invalid hash gracefully - error is acceptable
		_ = result.Errors
	})

	t.Run("ResolveReceipt_InvalidHash", func(t *testing.T) {
		query := `{ receipt(transactionHash: "invalid") { status } }`
		result := handler.ExecuteQuery(query, nil)

		// Should handle invalid hash gracefully - error is acceptable
		_ = result.Errors
	})

	t.Run("ResolveReceiptsByBlock_InvalidNumber", func(t *testing.T) {
		query := `{ receiptsByBlock(blockNumber: "invalid") { status } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error for invalid block number")
		}
	})

	t.Run("ResolveTransactionsByAddress_WithPagination", func(t *testing.T) {
		query := `{ transactionsByAddress(address: "0x456", pagination: {limit: 5, offset: 0}) { nodes { hash } totalCount } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) > 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})
}

func TestGraphQLErrorPaths(t *testing.T) {
	logger := zap.NewNop()
	errorStore := &mockStorageWithErrors{}

	handler, err := NewHandler(errorStore, logger)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	t.Run("ResolveLatestHeight_Error", func(t *testing.T) {
		query := `{ latestHeight }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveBlock_Error", func(t *testing.T) {
		query := `{ block(number: "1") { number } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveBlockByHash_Error", func(t *testing.T) {
		query := `{ blockByHash(hash: "0x123") { number } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveTransaction_Error", func(t *testing.T) {
		query := `{ transaction(hash: "0x123") { hash } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveReceipt_Error", func(t *testing.T) {
		query := `{ receipt(transactionHash: "0x123") { status } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveReceiptsByBlock_Error", func(t *testing.T) {
		query := `{ receiptsByBlock(blockNumber: "1") { status } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})

	t.Run("ResolveTransactionsByAddress_Error", func(t *testing.T) {
		query := `{ transactionsByAddress(address: "0x456") { nodes { hash } totalCount } }`
		result := handler.ExecuteQuery(query, nil)

		if len(result.Errors) == 0 {
			t.Error("expected error when storage fails")
		}
	})
}

func TestGraphQLMappers(t *testing.T) {
	logger := zap.NewNop()
	store := &mockStorage{
		latestHeight: 100,
		blocks:       make(map[uint64]*types.Block),
		blocksByHash: make(map[common.Hash]*types.Block),
	}

	schema, err := NewSchema(store, logger)
	if err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	t.Run("BlockToMap", func(t *testing.T) {
		header := &types.Header{
			Number:     common.Big1,
			ParentHash: common.HexToHash("0x123"),
			Time:       123456,
			GasLimit:   8000000,
			GasUsed:    5000000,
		}
		block := types.NewBlockWithHeader(header)
		blockMap := schema.blockToMap(blockModel(t, block))

		if blockMap == nil {
			t.Error("expected blockMap to be non-nil")
		}
		if blockMap["number"] == nil {
			t.Error("expected number field")
		}
		if blockMap["hash"] == nil {
			t.Error("expected hash field")
		}
	})

	t.Run("TransactionToMap", func(t *testing.T) {
		tx := types.NewTransaction(
			0,
			common.HexToAddress("0x456"),
			common.Big1,
			21000,
			common.Big1,
			nil,
		)
		location := &port.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(txModel(t, tx), location)

		if txMap == nil {
			t.Error("expected txMap to be non-nil")
		}
		if txMap["hash"] == nil {
			t.Error("expected hash field")
		}
		if txMap["blockNumber"] == nil {
			t.Error("expected blockNumber field")
		}
	})

	t.Run("ReceiptToMap", func(t *testing.T) {
		receipt := &types.Receipt{
			TxHash:            common.HexToHash("0xabc"),
			Status:            1,
			CumulativeGasUsed: 21000,
			GasUsed:           21000,
			Logs:              []*types.Log{},
			BlockNumber:       common.Big1,
			BlockHash:         common.HexToHash("0x123"),
			EffectiveGasPrice: common.Big1,
		}
		receiptMap := schema.receiptToMap(receipt)

		if receiptMap == nil {
			t.Error("expected receiptMap to be non-nil")
		}
		if receiptMap["status"] == nil {
			t.Error("expected status field")
		}
		if receiptMap["gasUsed"] == nil {
			t.Error("expected gasUsed field")
		}
	})

	t.Run("LogToMap", func(t *testing.T) {
		log := &types.Log{
			Address: common.HexToAddress("0x789"),
			Topics:  []common.Hash{common.HexToHash("0xabc")},
			Data:    []byte{1, 2, 3},
		}
		logMap := schema.logToMap(log)

		if logMap == nil {
			t.Error("expected logMap to be non-nil")
		}
		if logMap["address"] == nil {
			t.Error("expected address field")
		}
		if logMap["topics"] == nil {
			t.Error("expected topics field")
		}
	})

	t.Run("TransactionToMap_WithToAddress", func(t *testing.T) {
		to := common.HexToAddress("0x789")
		tx := types.NewTx(&types.LegacyTx{
			Nonce:    0,
			GasPrice: common.Big1,
			Gas:      21000,
			To:       &to,
			Value:    common.Big1,
			Data:     []byte{1, 2, 3},
		})
		location := &port.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(txModel(t, tx), location)

		if txMap == nil {
			t.Error("expected txMap to be non-nil")
		}
		if txMap["to"] == nil {
			t.Error("expected to field for contract call")
		}
		if txMap["gasPrice"] == nil {
			t.Error("expected gasPrice for legacy tx")
		}
	})

	t.Run("TransactionToMap_DynamicFeeTx", func(t *testing.T) {
		to := common.HexToAddress("0x789")
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   common.Big1,
			Nonce:     0,
			GasTipCap: common.Big1,
			GasFeeCap: common.Big2,
			Gas:       21000,
			To:        &to,
			Value:     common.Big1,
			Data:      []byte{},
		})
		location := &port.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(txModel(t, tx), location)

		if txMap == nil {
			t.Error("expected txMap to be non-nil")
		}
		if txMap["maxFeePerGas"] == nil {
			t.Error("expected maxFeePerGas for EIP-1559 tx")
		}
		if txMap["maxPriorityFeePerGas"] == nil {
			t.Error("expected maxPriorityFeePerGas for EIP-1559 tx")
		}
		if txMap["chainId"] == nil {
			t.Error("expected chainId")
		}
	})

	t.Run("TransactionToMap_AccessListTx", func(t *testing.T) {
		to := common.HexToAddress("0x789")
		accessList := types.AccessList{
			types.AccessTuple{
				Address: common.HexToAddress("0xabc"),
				StorageKeys: []common.Hash{
					common.HexToHash("0x123"),
					common.HexToHash("0x456"),
				},
			},
		}
		tx := types.NewTx(&types.AccessListTx{
			ChainID:    common.Big1,
			Nonce:      0,
			GasPrice:   common.Big1,
			Gas:        21000,
			To:         &to,
			Value:      common.Big1,
			Data:       []byte{},
			AccessList: accessList,
		})
		location := &port.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(txModel(t, tx), location)

		if txMap == nil {
			t.Error("expected txMap to be non-nil")
		}
		if txMap["accessList"] == nil {
			t.Error("expected accessList for EIP-2930 tx")
		}
		accessListResult, ok := txMap["accessList"].([]interface{})
		if !ok {
			t.Error("expected accessList to be an array")
		}
		if len(accessListResult) != 1 {
			t.Errorf("expected 1 access list entry, got %d", len(accessListResult))
		}
	})

	t.Run("TransactionToMap_SetCodeTx", func(t *testing.T) {
		to := common.HexToAddress("0x789")
		authList := []types.SetCodeAuthorization{
			{
				ChainID: *uint256FromBig(common.Big1),
				Address: common.HexToAddress("0xdead"),
				Nonce:   1,
				V:       0,
				R:       *uint256FromBig(common.Big1),
				S:       *uint256FromBig(common.Big2),
			},
		}
		tx := types.NewTx(&types.SetCodeTx{
			ChainID:    uint256FromBig(common.Big1),
			Nonce:      0,
			GasTipCap:  uint256FromBig(common.Big1),
			GasFeeCap:  uint256FromBig(common.Big2),
			Gas:        21000,
			To:         to,
			Value:      uint256FromBig(common.Big1),
			Data:       []byte{},
			AccessList: nil,
			AuthList:   authList,
		})
		location := &port.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(txModel(t, tx), location)

		if txMap == nil {
			t.Error("expected txMap to be non-nil")
		}
		if txMap["type"].(int) != 4 {
			t.Errorf("expected type 4 (SetCodeTx), got %v", txMap["type"])
		}
		if txMap["authorizationList"] == nil {
			t.Error("expected authorizationList for EIP-7702 tx")
		}
		authListResult, ok := txMap["authorizationList"].([]interface{})
		if !ok {
			t.Error("expected authorizationList to be an array")
		}
		if len(authListResult) != 1 {
			t.Errorf("expected 1 authorization entry, got %d", len(authListResult))
		}
		// Check the authorization entry fields
		authEntry, ok := authListResult[0].(map[string]interface{})
		if !ok {
			t.Error("expected authorization entry to be a map")
		}
		if authEntry["address"] == nil {
			t.Error("expected address field in authorization")
		}
		if authEntry["nonce"] == nil {
			t.Error("expected nonce field in authorization")
		}
		if authEntry["yParity"] == nil {
			t.Error("expected yParity field in authorization")
		}
	})

	t.Run("BlockToMap_Nil", func(t *testing.T) {
		blockMap := schema.blockToMap(nil)
		if blockMap != nil {
			t.Error("expected nil for nil block")
		}
	})

	t.Run("TransactionToMap_Nil", func(t *testing.T) {
		txMap := schema.transactionToMap(nil, &port.TxLocation{})
		if txMap != nil {
			t.Error("expected nil for nil transaction")
		}
	})

	t.Run("ReceiptToMap_Nil", func(t *testing.T) {
		receiptMap := schema.receiptToMap(nil)
		if receiptMap != nil {
			t.Error("expected nil for nil receipt")
		}
	})

	t.Run("LogToMap_Nil", func(t *testing.T) {
		logMap := schema.logToMap(nil)
		if logMap != nil {
			t.Error("expected nil for nil log")
		}
	})

	t.Run("ReceiptToMap_WithLogs", func(t *testing.T) {
		log := &types.Log{
			Address: common.HexToAddress("0x789"),
			Topics:  []common.Hash{common.HexToHash("0xabc")},
			Data:    []byte{1, 2, 3},
		}
		receipt := &types.Receipt{
			TxHash:            common.HexToHash("0xabc"),
			Status:            1,
			CumulativeGasUsed: 21000,
			GasUsed:           21000,
			Logs:              []*types.Log{log},
			BlockNumber:       common.Big1,
			BlockHash:         common.HexToHash("0x123"),
			EffectiveGasPrice: common.Big1,
		}
		receiptMap := schema.receiptToMap(receipt)

		if receiptMap == nil {
			t.Error("expected receiptMap to be non-nil")
		}
		logs, ok := receiptMap["logs"].([]interface{})
		if !ok {
			t.Error("expected logs to be an array")
		}
		if len(logs) != 1 {
			t.Errorf("expected 1 log, got %d", len(logs))
		}
	})

	t.Run("ReceiptToMap_WithContractAddress", func(t *testing.T) {
		receipt := &types.Receipt{
			TxHash:            common.HexToHash("0xabc"),
			Status:            1,
			CumulativeGasUsed: 21000,
			GasUsed:           21000,
			Logs:              []*types.Log{},
			BlockNumber:       common.Big1,
			BlockHash:         common.HexToHash("0x123"),
			EffectiveGasPrice: common.Big1,
			ContractAddress:   common.HexToAddress("0x999"),
		}
		receiptMap := schema.receiptToMap(receipt)

		if receiptMap == nil {
			t.Error("expected receiptMap to be non-nil")
		}
		if receiptMap["contractAddress"] == nil {
			t.Error("expected contractAddress field")
		}
	})
}

// blockModel and txModel convert test fixtures to the model the mappers take.
func blockModel(t *testing.T, b *types.Block) *model.Block {
	t.Helper()
	m, err := gethconv.BlockFromGeth(b)
	require.NoError(t, err)
	return m
}

func txModel(t *testing.T, tx *types.Transaction) *model.Transaction {
	t.Helper()
	m, err := gethconv.TxFromGeth(tx)
	require.NoError(t, err)
	return m
}

func (m *mockStorage) GetBlock(ctx context.Context, height uint64) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlock(ctx, height))
}

func (m *mockStorage) GetBlockByHash(ctx context.Context, hash common.Hash) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlockByHash(ctx, hash))
}

func (m *mockStorage) GetTransaction(ctx context.Context, hash common.Hash) (*model.Transaction, *port.TxLocation, error) {
	return modelTxOf(m.gethGetTransaction(ctx, hash))
}

func (m *mockStorage) GetTransactions(ctx context.Context, hashes []common.Hash) ([]*model.Transaction, []*port.TxLocation, error) {
	return modelTxsOf(m.gethGetTransactions(ctx, hashes))
}

func (m *mockStorage) GetReceipt(ctx context.Context, hash common.Hash) (*model.Receipt, error) {
	return modelReceiptOf(m.gethGetReceipt(ctx, hash))
}

func (m *mockStorage) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceiptsByBlockNumber(ctx, blockNumber))
}

func (m *mockStorage) GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*model.Block, error) {
	return modelBlocksOf(m.gethGetBlocks(ctx, startHeight, endHeight))
}

func (m *mockStorage) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceipts(ctx, hashes))
}

func (m *mockStorage) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceiptsByBlockHash(ctx, blockHash))
}

func (m *mockStorage) SetBlock(ctx context.Context, b *model.Block) error {
	gb, err := gethconv.BlockToGeth(b)
	if err != nil {
		return err
	}
	return m.gethSetBlock(ctx, gb)
}

func (m *mockStorage) SetReceipt(ctx context.Context, r *model.Receipt) error {
	return m.gethSetReceipt(ctx, gethconv.ReceiptToGeth(r))
}

func (m *mockStorage) GetLogs(ctx context.Context, filter *port.LogFilter) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogs(ctx, filter))
}

func (m *mockStorage) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByBlock(ctx, blockNumber))
}

func (m *mockStorage) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByAddress(ctx, address, fromBlock, toBlock))
}

func (m *mockStorage) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByTopic(ctx, topic, topicIndex, fromBlock, toBlock))
}

func (m *mockStorage) IndexLogs(ctx context.Context, logs []*model.Log) error {
	return m.gethIndexLogs(ctx, gethconv.LogsToGeth(logs))
}

func (m *mockStorage) IndexLog(ctx context.Context, log *model.Log) error {
	return m.gethIndexLog(ctx, gethconv.LogToGeth(log))
}

func (m *mockStorage) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*model.Block, error) {
	return modelBlocksOf(m.gethGetBlocksByTimeRange(ctx, fromTime, toTime, limit, offset))
}

func (m *mockStorage) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlockByTimestamp(ctx, timestamp))
}

func (m *mockStorageWithErrors) GetBlock(ctx context.Context, height uint64) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlock(ctx, height))
}

func (m *mockStorageWithErrors) GetBlockByHash(ctx context.Context, hash common.Hash) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlockByHash(ctx, hash))
}

func (m *mockStorageWithErrors) GetTransaction(ctx context.Context, hash common.Hash) (*model.Transaction, *port.TxLocation, error) {
	return modelTxOf(m.gethGetTransaction(ctx, hash))
}

func (m *mockStorageWithErrors) GetTransactions(ctx context.Context, hashes []common.Hash) ([]*model.Transaction, []*port.TxLocation, error) {
	return modelTxsOf(m.gethGetTransactions(ctx, hashes))
}

func (m *mockStorageWithErrors) GetReceipt(ctx context.Context, hash common.Hash) (*model.Receipt, error) {
	return modelReceiptOf(m.gethGetReceipt(ctx, hash))
}

func (m *mockStorageWithErrors) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceiptsByBlockNumber(ctx, blockNumber))
}

func (m *mockStorageWithErrors) GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*model.Block, error) {
	return modelBlocksOf(m.gethGetBlocks(ctx, startHeight, endHeight))
}

func (m *mockStorageWithErrors) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceipts(ctx, hashes))
}

func (m *mockStorageWithErrors) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*model.Receipt, error) {
	return modelReceiptsOf(m.gethGetReceiptsByBlockHash(ctx, blockHash))
}

func (m *mockStorageWithErrors) SetBlock(ctx context.Context, b *model.Block) error {
	gb, err := gethconv.BlockToGeth(b)
	if err != nil {
		return err
	}
	return m.gethSetBlock(ctx, gb)
}

func (m *mockStorageWithErrors) SetReceipt(ctx context.Context, r *model.Receipt) error {
	return m.gethSetReceipt(ctx, gethconv.ReceiptToGeth(r))
}

func (m *mockStorageWithErrors) GetLogs(ctx context.Context, filter *port.LogFilter) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogs(ctx, filter))
}

func (m *mockStorageWithErrors) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByBlock(ctx, blockNumber))
}

func (m *mockStorageWithErrors) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByAddress(ctx, address, fromBlock, toBlock))
}

func (m *mockStorageWithErrors) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*model.Log, error) {
	return modelLogsOf(m.gethGetLogsByTopic(ctx, topic, topicIndex, fromBlock, toBlock))
}

func (m *mockStorageWithErrors) IndexLogs(ctx context.Context, logs []*model.Log) error {
	return m.gethIndexLogs(ctx, gethconv.LogsToGeth(logs))
}

func (m *mockStorageWithErrors) IndexLog(ctx context.Context, log *model.Log) error {
	return m.gethIndexLog(ctx, gethconv.LogToGeth(log))
}

func (m *mockStorageWithErrors) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*model.Block, error) {
	return modelBlocksOf(m.gethGetBlocksByTimeRange(ctx, fromTime, toTime, limit, offset))
}

func (m *mockStorageWithErrors) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*model.Block, error) {
	return modelBlockOf(m.gethGetBlockByTimestamp(ctx, timestamp))
}

func (m *mockStorageWithErrors) GetTransactionsByAddress(ctx context.Context, addr common.Address, page port.Page) ([]common.Hash, string, error) {
	items, err := m.offsetGetTransactionsByAddress(ctx, addr, page.Limit, page.Offset)
	return items, "", err
}

func (m *mockStorage) GetTransactionsByAddress(ctx context.Context, addr common.Address, page port.Page) ([]common.Hash, string, error) {
	items, err := m.offsetGetTransactionsByAddress(ctx, addr, page.Limit, page.Offset)
	return items, "", err
}
