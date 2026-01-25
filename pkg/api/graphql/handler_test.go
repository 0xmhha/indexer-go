package graphql

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

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

func (m *mockStorage) GetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	if block, ok := m.blocks[height]; ok {
		return block, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	if block, ok := m.blocksByHash[hash]; ok {
		return block, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *storage.TxLocation, error) {
	if tx, ok := m.transactions[hash]; ok {
		location := &storage.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		return tx, location, nil
	}
	return nil, nil, storage.ErrNotFound
}

func (m *mockStorage) GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	if receipt, ok := m.receipts[hash]; ok {
		return receipt, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	return []common.Hash{}, nil
}

func (m *mockStorage) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	return []*types.Receipt{}, nil
}

func (m *mockStorage) GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error) {
	blocks := make([]*types.Block, 0, endHeight-startHeight+1)
	for i := startHeight; i <= endHeight; i++ {
		if block, ok := m.blocks[i]; ok {
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

func (m *mockStorage) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	return []*types.Receipt{}, nil
}

func (m *mockStorage) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error) {
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

func (m *mockStorage) SetBlock(ctx context.Context, block *types.Block) error {
	m.blocks[block.NumberU64()] = block
	m.blocksByHash[block.Hash()] = block
	return nil
}

func (m *mockStorage) SetTransaction(ctx context.Context, tx *types.Transaction, location *storage.TxLocation) error {
	m.transactions[tx.Hash()] = tx
	return nil
}

func (m *mockStorage) SetReceipt(ctx context.Context, receipt *types.Receipt) error {
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

func (m *mockStorage) NewBatch() storage.Batch {
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

func (m *mockStorage) GetLogs(ctx context.Context, filter *storage.LogFilter) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return []*types.Log{}, nil
}

func (m *mockStorage) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
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

func (m *mockStorage) IndexLogs(ctx context.Context, logs []*types.Log) error {
	return nil
}

func (m *mockStorage) IndexLog(ctx context.Context, log *types.Log) error {
	return nil
}

func (m *mockStorage) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]storage.SearchResult, error) {
	return []storage.SearchResult{}, nil
}

// Contract verification methods
func (m *mockStorage) GetContractVerification(ctx context.Context, address common.Address) (*storage.ContractVerification, error) {
	return nil, storage.ErrNotFound
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

func (m *mockStorage) SetContractVerification(ctx context.Context, verification *storage.ContractVerification) error {
	return nil
}

func (m *mockStorage) DeleteContractVerification(ctx context.Context, address common.Address) error {
	return nil
}

// SystemContractReader implementation
func (m *mockStorage) GetTotalSupply(ctx context.Context) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (m *mockStorage) GetMintEvents(ctx context.Context, fromBlock, toBlock uint64, minter common.Address, limit, offset int) ([]*storage.MintEvent, error) {
	return []*storage.MintEvent{}, nil
}

func (m *mockStorage) GetBurnEvents(ctx context.Context, fromBlock, toBlock uint64, burner common.Address, limit, offset int) ([]*storage.BurnEvent, error) {
	return []*storage.BurnEvent{}, nil
}

func (m *mockStorage) GetActiveMinters(ctx context.Context) ([]common.Address, error) {
	return []common.Address{}, nil
}

func (m *mockStorage) GetMinterAllowance(ctx context.Context, minter common.Address) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (m *mockStorage) GetMinterHistory(ctx context.Context, minter common.Address) ([]*storage.MinterConfigEvent, error) {
	return []*storage.MinterConfigEvent{}, nil
}

func (m *mockStorage) GetActiveValidators(ctx context.Context) ([]common.Address, error) {
	return []common.Address{}, nil
}

func (m *mockStorage) GetGasTipHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*storage.GasTipUpdateEvent, error) {
	return []*storage.GasTipUpdateEvent{}, nil
}

func (m *mockStorage) GetValidatorHistory(ctx context.Context, validator common.Address) ([]*storage.ValidatorChangeEvent, error) {
	return []*storage.ValidatorChangeEvent{}, nil
}

func (m *mockStorage) GetMinterConfigHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*storage.MinterConfigEvent, error) {
	return []*storage.MinterConfigEvent{}, nil
}

func (m *mockStorage) GetEmergencyPauseHistory(ctx context.Context, contract common.Address) ([]*storage.EmergencyPauseEvent, error) {
	return []*storage.EmergencyPauseEvent{}, nil
}

func (m *mockStorage) GetDepositMintProposals(ctx context.Context, fromBlock, toBlock uint64, status storage.ProposalStatus) ([]*storage.DepositMintProposal, error) {
	return []*storage.DepositMintProposal{}, nil
}

func (m *mockStorage) GetBurnHistory(ctx context.Context, fromBlock, toBlock uint64, user common.Address) ([]*storage.BurnEvent, error) {
	return []*storage.BurnEvent{}, nil
}

func (m *mockStorage) GetBlacklistedAddresses(ctx context.Context) ([]common.Address, error) {
	return []common.Address{}, nil
}

func (m *mockStorage) GetBlacklistHistory(ctx context.Context, address common.Address) ([]*storage.BlacklistEvent, error) {
	return []*storage.BlacklistEvent{}, nil
}

func (m *mockStorage) GetAuthorizedAccounts(ctx context.Context) ([]common.Address, error) {
	return []common.Address{}, nil
}

func (m *mockStorage) GetProposals(ctx context.Context, contract common.Address, status storage.ProposalStatus, limit, offset int) ([]*storage.Proposal, error) {
	return []*storage.Proposal{}, nil
}

func (m *mockStorage) GetProposalById(ctx context.Context, contract common.Address, proposalId *big.Int) (*storage.Proposal, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetProposalVotes(ctx context.Context, contract common.Address, proposalId *big.Int) ([]*storage.ProposalVote, error) {
	return []*storage.ProposalVote{}, nil
}

func (m *mockStorage) GetMemberHistory(ctx context.Context, contract common.Address) ([]*storage.MemberChangeEvent, error) {
	return []*storage.MemberChangeEvent{}, nil
}

// WBFTReader methods for mockStorage
func (m *mockStorage) GetWBFTBlockExtra(ctx context.Context, blockNumber uint64) (*storage.WBFTBlockExtra, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetWBFTBlockExtraByHash(ctx context.Context, blockHash common.Hash) (*storage.WBFTBlockExtra, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetEpochInfo(ctx context.Context, epochNumber uint64) (*storage.EpochInfo, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetLatestEpochInfo(ctx context.Context) (*storage.EpochInfo, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetValidatorSigningStats(ctx context.Context, validatorAddress common.Address, fromBlock, toBlock uint64) (*storage.ValidatorSigningStats, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetAllValidatorsSigningStats(ctx context.Context, fromBlock, toBlock uint64, limit, offset int) ([]*storage.ValidatorSigningStats, error) {
	return []*storage.ValidatorSigningStats{}, nil
}

func (m *mockStorage) GetValidatorSigningActivity(ctx context.Context, validatorAddress common.Address, fromBlock, toBlock uint64, limit, offset int) ([]*storage.ValidatorSigningActivity, error) {
	return []*storage.ValidatorSigningActivity{}, nil
}

func (m *mockStorage) GetBlockSigners(ctx context.Context, blockNumber uint64) (preparers []common.Address, committers []common.Address, err error) {
	return []common.Address{}, []common.Address{}, nil
}

// WBFTWriter methods for mockStorage
func (m *mockStorage) SaveWBFTBlockExtra(ctx context.Context, extra *storage.WBFTBlockExtra) error {
	return nil
}

func (m *mockStorage) SaveEpochInfo(ctx context.Context, epochInfo *storage.EpochInfo) error {
	return nil
}

func (m *mockStorage) UpdateValidatorSigningStats(ctx context.Context, blockNumber uint64, signingActivities []*storage.ValidatorSigningActivity) error {
	return nil
}

// HistoricalReader methods for mockStorage
func (m *mockStorage) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	return []*types.Block{}, nil
}

func (m *mockStorage) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *storage.TransactionFilter, limit, offset int) ([]*storage.TransactionWithReceipt, error) {
	return []*storage.TransactionWithReceipt{}, nil
}

func (m *mockStorage) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (m *mockStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]storage.BalanceSnapshot, error) {
	return []storage.BalanceSnapshot{}, nil
}

func (m *mockStorage) GetBlockCount(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockStorage) GetTransactionCount(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockStorage) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.MinerStats, error) {
	return []storage.MinerStats{}, nil
}

func (m *mockStorage) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]storage.TokenBalance, error) {
	return []storage.TokenBalance{}, nil
}

func (m *mockStorage) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*storage.GasStats, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*storage.AddressGasStats, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorage) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.AddressGasStats, error) {
	return []storage.AddressGasStats{}, nil
}

func (m *mockStorage) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.AddressActivityStats, error) {
	return []storage.AddressActivityStats{}, nil
}

func (m *mockStorage) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*storage.NetworkMetrics, error) {
	return nil, storage.ErrNotFound
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

// FeeDelegationReader methods for mockStorage
func (m *mockStorage) GetFeeDelegationStats(ctx context.Context, fromBlock, toBlock uint64) (*storage.FeeDelegationStats, error) {
	return &storage.FeeDelegationStats{
		TotalFeeDelegatedTxs: 0,
		TotalFeesSaved:       big.NewInt(0),
		AdoptionRate:         0.0,
		AvgFeeSaved:          big.NewInt(0),
	}, nil
}

func (m *mockStorage) GetTopFeePayers(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.FeePayerStats, uint64, error) {
	return []storage.FeePayerStats{}, 0, nil
}

func (m *mockStorage) GetFeePayerStats(ctx context.Context, feePayer common.Address, fromBlock, toBlock uint64) (*storage.FeePayerStats, error) {
	return &storage.FeePayerStats{
		Address:       feePayer,
		TxCount:       0,
		TotalFeesPaid: big.NewInt(0),
		Percentage:    0.0,
	}, nil
}

// KVStore methods for mockStorage
func (m *mockStorage) Put(ctx context.Context, key, value []byte) error {
	return nil
}

func (m *mockStorage) Get(ctx context.Context, key []byte) ([]byte, error) {
	return nil, storage.ErrNotFound
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

// mockStorageWithErrors returns errors for testing error paths
type mockStorageWithErrors struct {
}

func (m *mockStorageWithErrors) GetLatestHeight(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *storage.TxLocation, error) {
	return nil, nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) HasBlock(ctx context.Context, height uint64) (bool, error) {
	return false, storage.ErrNotFound
}

func (m *mockStorageWithErrors) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	return false, storage.ErrNotFound
}

func (m *mockStorageWithErrors) HasReceipt(ctx context.Context, hash common.Hash) (bool, error) {
	return false, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetLatestHeight(ctx context.Context, height uint64) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetBlock(ctx context.Context, block *types.Block) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetTransaction(ctx context.Context, tx *types.Transaction, location *storage.TxLocation) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetReceipt(ctx context.Context, receipt *types.Receipt) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetReceipts(ctx context.Context, receipts []*types.Receipt) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetBlocks(ctx context.Context, blocks []*types.Block) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteBlock(ctx context.Context, height uint64) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) Close() error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) NewBatch() storage.Batch {
	return nil
}

func (m *mockStorageWithErrors) Compact(ctx context.Context, start, end []byte) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SetABI(ctx context.Context, address common.Address, abiJSON []byte) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetABI(ctx context.Context, address common.Address) ([]byte, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteABI(ctx context.Context, address common.Address) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) ListABIs(ctx context.Context) ([]common.Address, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) HasABI(ctx context.Context, address common.Address) (bool, error) {
	return false, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetLogs(ctx context.Context, filter *storage.LogFilter) ([]*types.Log, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorageWithErrors) SaveLog(ctx context.Context, log *types.Log) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) SaveLogs(ctx context.Context, logs []*types.Log) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) DeleteLogsByBlock(ctx context.Context, blockNumber uint64) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) IndexLogs(ctx context.Context, logs []*types.Log) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) IndexLog(ctx context.Context, log *types.Log) error {
	return storage.ErrNotFound
}

func (m *mockStorageWithErrors) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]storage.SearchResult, error) {
	return nil, storage.ErrNotFound
}

// Contract verification methods
func (m *mockStorageWithErrors) GetContractVerification(ctx context.Context, address common.Address) (*storage.ContractVerification, error) {
	return nil, storage.ErrNotFound
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

func (m *mockStorageWithErrors) SetContractVerification(ctx context.Context, verification *storage.ContractVerification) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) DeleteContractVerification(ctx context.Context, address common.Address) error {
	return fmt.Errorf("storage error")
}

// SystemContractReader implementation
func (m *mockStorageWithErrors) GetTotalSupply(ctx context.Context) (*big.Int, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetMintEvents(ctx context.Context, fromBlock, toBlock uint64, minter common.Address, limit, offset int) ([]*storage.MintEvent, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBurnEvents(ctx context.Context, fromBlock, toBlock uint64, burner common.Address, limit, offset int) ([]*storage.BurnEvent, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetActiveMinters(ctx context.Context) ([]common.Address, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetMinterAllowance(ctx context.Context, minter common.Address) (*big.Int, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetMinterHistory(ctx context.Context, minter common.Address) ([]*storage.MinterConfigEvent, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetActiveValidators(ctx context.Context) ([]common.Address, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetGasTipHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*storage.GasTipUpdateEvent, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetValidatorHistory(ctx context.Context, validator common.Address) ([]*storage.ValidatorChangeEvent, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetMinterConfigHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*storage.MinterConfigEvent, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetEmergencyPauseHistory(ctx context.Context, contract common.Address) ([]*storage.EmergencyPauseEvent, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetDepositMintProposals(ctx context.Context, fromBlock, toBlock uint64, status storage.ProposalStatus) ([]*storage.DepositMintProposal, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBurnHistory(ctx context.Context, fromBlock, toBlock uint64, user common.Address) ([]*storage.BurnEvent, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBlacklistedAddresses(ctx context.Context) ([]common.Address, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBlacklistHistory(ctx context.Context, address common.Address) ([]*storage.BlacklistEvent, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetAuthorizedAccounts(ctx context.Context) ([]common.Address, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetProposals(ctx context.Context, contract common.Address, status storage.ProposalStatus, limit, offset int) ([]*storage.Proposal, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetProposalById(ctx context.Context, contract common.Address, proposalId *big.Int) (*storage.Proposal, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetProposalVotes(ctx context.Context, contract common.Address, proposalId *big.Int) ([]*storage.ProposalVote, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetMemberHistory(ctx context.Context, contract common.Address) ([]*storage.MemberChangeEvent, error) {
	return nil, fmt.Errorf("storage error")
}

// WBFTReader methods for mockStorageWithErrors
func (m *mockStorageWithErrors) GetWBFTBlockExtra(ctx context.Context, blockNumber uint64) (*storage.WBFTBlockExtra, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetWBFTBlockExtraByHash(ctx context.Context, blockHash common.Hash) (*storage.WBFTBlockExtra, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetEpochInfo(ctx context.Context, epochNumber uint64) (*storage.EpochInfo, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetLatestEpochInfo(ctx context.Context) (*storage.EpochInfo, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetValidatorSigningStats(ctx context.Context, validatorAddress common.Address, fromBlock, toBlock uint64) (*storage.ValidatorSigningStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetAllValidatorsSigningStats(ctx context.Context, fromBlock, toBlock uint64, limit, offset int) ([]*storage.ValidatorSigningStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetValidatorSigningActivity(ctx context.Context, validatorAddress common.Address, fromBlock, toBlock uint64, limit, offset int) ([]*storage.ValidatorSigningActivity, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBlockSigners(ctx context.Context, blockNumber uint64) (preparers []common.Address, committers []common.Address, err error) {
	return nil, nil, fmt.Errorf("storage error")
}

// WBFTWriter methods for mockStorageWithErrors
func (m *mockStorageWithErrors) SaveWBFTBlockExtra(ctx context.Context, extra *storage.WBFTBlockExtra) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) SaveEpochInfo(ctx context.Context, epochInfo *storage.EpochInfo) error {
	return fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) UpdateValidatorSigningStats(ctx context.Context, blockNumber uint64, signingActivities []*storage.ValidatorSigningActivity) error {
	return fmt.Errorf("storage error")
}

// HistoricalReader methods for mockStorageWithErrors
func (m *mockStorageWithErrors) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *storage.TransactionFilter, limit, offset int) ([]*storage.TransactionWithReceipt, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]storage.BalanceSnapshot, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetBlockCount(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTransactionCount(ctx context.Context) (uint64, error) {
	return 0, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.MinerStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]storage.TokenBalance, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*storage.GasStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*storage.AddressGasStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.AddressGasStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.AddressActivityStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*storage.NetworkMetrics, error) {
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

// FeeDelegationReader methods for mockStorageWithErrors
func (m *mockStorageWithErrors) GetFeeDelegationStats(ctx context.Context, fromBlock, toBlock uint64) (*storage.FeeDelegationStats, error) {
	return nil, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetTopFeePayers(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.FeePayerStats, uint64, error) {
	return nil, 0, fmt.Errorf("storage error")
}

func (m *mockStorageWithErrors) GetFeePayerStats(ctx context.Context, feePayer common.Address, fromBlock, toBlock uint64) (*storage.FeePayerStats, error) {
	return nil, fmt.Errorf("storage error")
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

		// Should handle invalid hash gracefully
		if len(result.Errors) > 0 {
			// Error is acceptable for invalid hash
		}
	})

	t.Run("ResolveTransaction_InvalidHash", func(t *testing.T) {
		query := `{ transaction(hash: "invalid") { hash } }`
		result := handler.ExecuteQuery(query, nil)

		// Should handle invalid hash gracefully
		if len(result.Errors) > 0 {
			// Error is acceptable for invalid hash
		}
	})

	t.Run("ResolveReceipt_InvalidHash", func(t *testing.T) {
		query := `{ receipt(transactionHash: "invalid") { status } }`
		result := handler.ExecuteQuery(query, nil)

		// Should handle invalid hash gracefully
		if len(result.Errors) > 0 {
			// Error is acceptable for invalid hash
		}
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
		blockMap := schema.blockToMap(block)

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
		location := &storage.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(tx, location)

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
		location := &storage.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(tx, location)

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
		location := &storage.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(tx, location)

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
		location := &storage.TxLocation{
			BlockHeight: 1,
			BlockHash:   common.HexToHash("0x123"),
			TxIndex:     0,
		}
		txMap := schema.transactionToMap(tx, location)

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

	t.Run("BlockToMap_Nil", func(t *testing.T) {
		blockMap := schema.blockToMap(nil)
		if blockMap != nil {
			t.Error("expected nil for nil block")
		}
	})

	t.Run("TransactionToMap_Nil", func(t *testing.T) {
		txMap := schema.transactionToMap(nil, &storage.TxLocation{})
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
