package watchlist

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// mockKVStore implements storage.KVStore for testing
type mockKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newMockKVStore() *mockKVStore {
	return &mockKVStore{
		data: make(map[string][]byte),
	}
}

func (m *mockKVStore) Put(ctx context.Context, key, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[string(key)] = append([]byte{}, value...)
	return nil
}

func (m *mockKVStore) Get(ctx context.Context, key []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if val, ok := m.data[string(key)]; ok {
		return append([]byte{}, val...), nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockKVStore) Delete(ctx context.Context, key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, string(key))
	return nil
}

func (m *mockKVStore) Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	prefixStr := string(prefix)
	for k, v := range m.data {
		if len(k) >= len(prefixStr) && k[:len(prefixStr)] == prefixStr {
			if !fn([]byte(k), v) {
				break
			}
		}
	}
	return nil
}

func (m *mockKVStore) Has(ctx context.Context, key []byte) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[string(key)]
	return ok, nil
}

// mockStorage wraps mockKVStore to implement storage.Storage interface
// Only KVStore methods are properly implemented; others are stubs
type mockStorage struct {
	*mockKVStore
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		mockKVStore: newMockKVStore(),
	}
}

// Storage core methods
func (m *mockStorage) Close() error                                         { return nil }
func (m *mockStorage) NewBatch() storage.Batch                              { return nil }
func (m *mockStorage) Compact(ctx context.Context, start, end []byte) error { return nil }

// Reader interface
func (m *mockStorage) GetLatestHeight(ctx context.Context) (uint64, error) {
	return 0, storage.ErrNotFound
}
func (m *mockStorage) GetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *storage.TxLocation, error) {
	return nil, nil, storage.ErrNotFound
}
func (m *mockStorage) GetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	return nil, nil
}
func (m *mockStorage) GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	return nil, nil
}
func (m *mockStorage) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error) {
	return nil, nil
}
func (m *mockStorage) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	return nil, nil
}
func (m *mockStorage) GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error) {
	return nil, nil
}
func (m *mockStorage) HasBlock(ctx context.Context, height uint64) (bool, error)         { return false, nil }
func (m *mockStorage) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) { return false, nil }
func (m *mockStorage) HasReceipt(ctx context.Context, hash common.Hash) (bool, error)     { return false, nil }
func (m *mockStorage) GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error) {
	return nil, nil
}

// Writer interface
func (m *mockStorage) SetLatestHeight(ctx context.Context, height uint64) error { return nil }
func (m *mockStorage) SetBlock(ctx context.Context, block *types.Block) error   { return nil }
func (m *mockStorage) SetTransaction(ctx context.Context, tx *types.Transaction, location *storage.TxLocation) error {
	return nil
}
func (m *mockStorage) SetReceipt(ctx context.Context, receipt *types.Receipt) error { return nil }
func (m *mockStorage) SetReceipts(ctx context.Context, receipts []*types.Receipt) error {
	return nil
}
func (m *mockStorage) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	return nil
}
func (m *mockStorage) SetBlocks(ctx context.Context, blocks []*types.Block) error { return nil }
func (m *mockStorage) DeleteBlock(ctx context.Context, height uint64) error       { return nil }

// LogReader interface
func (m *mockStorage) GetLogs(ctx context.Context, filter *storage.LogFilter) ([]*types.Log, error) {
	return nil, nil
}
func (m *mockStorage) GetLogsByBlock(ctx context.Context, blockNumber uint64) ([]*types.Log, error) {
	return nil, nil
}
func (m *mockStorage) GetLogsByAddress(ctx context.Context, address common.Address, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, nil
}
func (m *mockStorage) GetLogsByTopic(ctx context.Context, topic common.Hash, topicIndex int, fromBlock, toBlock uint64) ([]*types.Log, error) {
	return nil, nil
}

// LogWriter interface
func (m *mockStorage) IndexLogs(ctx context.Context, logs []*types.Log) error {
	return nil
}
func (m *mockStorage) IndexLog(ctx context.Context, log *types.Log) error {
	return nil
}

// ABIReader interface
func (m *mockStorage) GetABI(ctx context.Context, address common.Address) ([]byte, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) HasABI(ctx context.Context, address common.Address) (bool, error) {
	return false, nil
}
func (m *mockStorage) ListABIs(ctx context.Context) ([]common.Address, error) {
	return nil, nil
}

// ABIWriter interface
func (m *mockStorage) SetABI(ctx context.Context, address common.Address, abi []byte) error {
	return nil
}
func (m *mockStorage) DeleteABI(ctx context.Context, address common.Address) error { return nil }

// SearchReader interface
func (m *mockStorage) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]storage.SearchResult, error) {
	return nil, nil
}

// SystemContractReader interface
func (m *mockStorage) GetTotalSupply(ctx context.Context) (*big.Int, error) { return big.NewInt(0), nil }
func (m *mockStorage) GetMintEvents(ctx context.Context, fromBlock, toBlock uint64, minter common.Address, limit, offset int) ([]*storage.MintEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetBurnEvents(ctx context.Context, fromBlock, toBlock uint64, burner common.Address, limit, offset int) ([]*storage.BurnEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetActiveMinters(ctx context.Context) ([]common.Address, error) { return nil, nil }
func (m *mockStorage) GetMinterAllowance(ctx context.Context, minter common.Address) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (m *mockStorage) GetMinterHistory(ctx context.Context, minter common.Address) ([]*storage.MinterConfigEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetActiveValidators(ctx context.Context) ([]common.Address, error) { return nil, nil }
func (m *mockStorage) GetGasTipHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*storage.GasTipUpdateEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetValidatorHistory(ctx context.Context, validator common.Address) ([]*storage.ValidatorChangeEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetMinterConfigHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*storage.MinterConfigEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetEmergencyPauseHistory(ctx context.Context, contract common.Address) ([]*storage.EmergencyPauseEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetDepositMintProposals(ctx context.Context, fromBlock, toBlock uint64, status storage.ProposalStatus) ([]*storage.DepositMintProposal, error) {
	return nil, nil
}
func (m *mockStorage) GetBurnHistory(ctx context.Context, fromBlock, toBlock uint64, user common.Address) ([]*storage.BurnEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetBlacklistedAddresses(ctx context.Context) ([]common.Address, error) { return nil, nil }
func (m *mockStorage) GetBlacklistHistory(ctx context.Context, address common.Address) ([]*storage.BlacklistEvent, error) {
	return nil, nil
}
func (m *mockStorage) GetAuthorizedAccounts(ctx context.Context) ([]common.Address, error) { return nil, nil }
func (m *mockStorage) GetProposals(ctx context.Context, contract common.Address, status storage.ProposalStatus, limit, offset int) ([]*storage.Proposal, error) {
	return nil, nil
}
func (m *mockStorage) GetProposalById(ctx context.Context, contract common.Address, proposalId *big.Int) (*storage.Proposal, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetProposalVotes(ctx context.Context, contract common.Address, proposalId *big.Int) ([]*storage.ProposalVote, error) {
	return nil, nil
}
func (m *mockStorage) GetMemberHistory(ctx context.Context, contract common.Address) ([]*storage.MemberChangeEvent, error) {
	return nil, nil
}

// ContractVerificationReader interface
func (m *mockStorage) GetContractVerification(ctx context.Context, address common.Address) (*storage.ContractVerification, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) IsContractVerified(ctx context.Context, address common.Address) (bool, error) {
	return false, nil
}
func (m *mockStorage) ListVerifiedContracts(ctx context.Context, limit, offset int) ([]common.Address, error) {
	return nil, nil
}
func (m *mockStorage) CountVerifiedContracts(ctx context.Context) (int, error) { return 0, nil }

// ContractVerificationWriter interface
func (m *mockStorage) SetContractVerification(ctx context.Context, verification *storage.ContractVerification) error {
	return nil
}
func (m *mockStorage) DeleteContractVerification(ctx context.Context, address common.Address) error {
	return nil
}

// WBFTReader interface
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
	return nil, nil
}
func (m *mockStorage) GetValidatorSigningActivity(ctx context.Context, validatorAddress common.Address, fromBlock, toBlock uint64, limit, offset int) ([]*storage.ValidatorSigningActivity, error) {
	return nil, nil
}
func (m *mockStorage) GetBlockSigners(ctx context.Context, blockNumber uint64) (preparers []common.Address, committers []common.Address, err error) {
	return nil, nil, nil
}

// WBFTWriter interface
func (m *mockStorage) SaveWBFTBlockExtra(ctx context.Context, extra *storage.WBFTBlockExtra) error { return nil }
func (m *mockStorage) SaveEpochInfo(ctx context.Context, epochInfo *storage.EpochInfo) error       { return nil }
func (m *mockStorage) UpdateValidatorSigningStats(ctx context.Context, blockNumber uint64, signingActivities []*storage.ValidatorSigningActivity) error {
	return nil
}

// FeeDelegationReader interface
func (m *mockStorage) GetFeeDelegationStats(ctx context.Context, fromBlock, toBlock uint64) (*storage.FeeDelegationStats, error) {
	return nil, nil
}
func (m *mockStorage) GetTopFeePayers(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.FeePayerStats, uint64, error) {
	return nil, 0, nil
}
func (m *mockStorage) GetFeePayerStats(ctx context.Context, feePayer common.Address, fromBlock, toBlock uint64) (*storage.FeePayerStats, error) {
	return nil, nil
}
func (m *mockStorage) GetFeeDelegationTxMeta(ctx context.Context, txHash common.Hash) (*storage.FeeDelegationTxMeta, error) {
	return nil, nil
}
func (m *mockStorage) GetFeeDelegationTxsByFeePayer(ctx context.Context, feePayer common.Address, limit, offset int) ([]common.Hash, error) {
	return nil, nil
}

// FeeDelegationWriter interface
func (m *mockStorage) SetFeeDelegationTxMeta(ctx context.Context, meta *storage.FeeDelegationTxMeta) error {
	return nil
}

// HistoricalReader interface
func (m *mockStorage) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	return nil, nil
}
func (m *mockStorage) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *storage.TransactionFilter, limit, offset int) ([]*storage.TransactionWithReceipt, error) {
	return nil, nil
}
func (m *mockStorage) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (m *mockStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]storage.BalanceSnapshot, error) {
	return nil, nil
}
func (m *mockStorage) GetBlockCount(ctx context.Context) (uint64, error)       { return 0, nil }
func (m *mockStorage) GetTransactionCount(ctx context.Context) (uint64, error) { return 0, nil }
func (m *mockStorage) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.MinerStats, error) {
	return nil, nil
}
func (m *mockStorage) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]storage.TokenBalance, error) {
	return nil, nil
}
func (m *mockStorage) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*storage.GasStats, error) {
	return nil, nil
}
func (m *mockStorage) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*storage.AddressGasStats, error) {
	return nil, nil
}
func (m *mockStorage) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.AddressGasStats, error) {
	return nil, nil
}
func (m *mockStorage) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]storage.AddressActivityStats, error) {
	return nil, nil
}
func (m *mockStorage) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*storage.NetworkMetrics, error) {
	return nil, nil
}

// HistoricalWriter interface
func (m *mockStorage) SetBlockTimestamp(ctx context.Context, timestamp uint64, height uint64) error { return nil }
func (m *mockStorage) UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error {
	return nil
}
func (m *mockStorage) SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error {
	return nil
}

// TokenMetadataReader interface
func (m *mockStorage) GetTokenMetadata(ctx context.Context, address common.Address) (*storage.TokenMetadata, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStorage) ListTokensByStandard(ctx context.Context, standard storage.TokenStandard, limit, offset int) ([]*storage.TokenMetadata, error) {
	return nil, nil
}
func (m *mockStorage) GetTokensCount(ctx context.Context, standard storage.TokenStandard) (int, error) {
	return 0, nil
}
func (m *mockStorage) SearchTokens(ctx context.Context, query string, limit int) ([]*storage.TokenMetadata, error) {
	return nil, nil
}

// TokenMetadataWriter interface
func (m *mockStorage) SaveTokenMetadata(ctx context.Context, metadata *storage.TokenMetadata) error {
	return nil
}
func (m *mockStorage) DeleteTokenMetadata(ctx context.Context, address common.Address) error {
	return nil
}

func (m *mockStorage) SetTokenMetadataFetcher(fetcher storage.TokenMetadataFetcher) {
}

// Verify that mockStorage implements storage.Storage
var _ storage.Storage = (*mockStorage)(nil)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("expected config to not be nil")
	}

	if !config.Enabled {
		t.Error("expected Enabled=true by default")
	}

	if config.BloomFilter == nil {
		t.Error("expected BloomFilter config to not be nil")
	}

	if config.HistoryRetention != 720*time.Hour {
		t.Errorf("expected HistoryRetention 720h, got %v", config.HistoryRetention)
	}

	if config.MaxAddressesTotal != 100000 {
		t.Errorf("expected MaxAddressesTotal 100000, got %d", config.MaxAddressesTotal)
	}
}

func TestNewService(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Test with nil config
	service := NewService(nil, nil, nil, logger)

	if service == nil {
		t.Fatal("expected service to not be nil")
	}

	if service.config == nil {
		t.Error("expected default config to be set")
	}

	if service.matcher == nil {
		t.Error("expected matcher to be initialized")
	}

	if service.subscribers == nil {
		t.Error("expected subscribers map to be initialized")
	}

	if service.addrSubs == nil {
		t.Error("expected addrSubs map to be initialized")
	}
}

func TestNewServiceWithConfig(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &Config{
		Enabled:           false,
		BloomFilter:       DefaultBloomConfig(),
		HistoryRetention:  48 * time.Hour,
		MaxAddressesTotal: 1000,
	}

	service := NewService(config, nil, nil, logger)

	if service == nil {
		t.Fatal("expected service to not be nil")
	}

	if service.config.Enabled {
		t.Error("expected Enabled=false")
	}

	if service.config.HistoryRetention != 48*time.Hour {
		t.Errorf("expected HistoryRetention 48h, got %v", service.config.HistoryRetention)
	}
}

func TestServiceStartStop(t *testing.T) {
	// Note: Service Start requires storage for loadWatchedAddresses
	// This test verifies Stop without Start works correctly
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	service := NewService(nil, nil, nil, logger)

	// Stop without starting should succeed (idempotent)
	err := service.Stop(ctx)
	if err != nil {
		t.Errorf("stop failed: %v", err)
	}
}

func TestServiceDoubleStop(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	service := NewService(nil, nil, nil, logger)

	// Stop without starting (should be idempotent)
	err := service.Stop(ctx)
	if err != nil {
		t.Errorf("first stop without start should succeed: %v", err)
	}

	err = service.Stop(ctx)
	if err != nil {
		t.Errorf("second stop should be idempotent: %v", err)
	}
}

func TestWatchRequestStruct(t *testing.T) {
	// Test WatchRequest struct creation (validation requires storage)
	req := &WatchRequest{
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
		Label:   "Test Label",
		Filter:  DefaultWatchFilter(),
	}

	if req.Address == (common.Address{}) {
		t.Error("address should not be zero")
	}

	if req.ChainID != "chain-1" {
		t.Errorf("expected ChainID 'chain-1', got '%s'", req.ChainID)
	}

	if req.Label != "Test Label" {
		t.Errorf("expected Label 'Test Label', got '%s'", req.Label)
	}

	if req.Filter == nil {
		t.Error("filter should not be nil")
	}
}

func TestServiceMatcherIntegration(t *testing.T) {
	// Test that service has a properly initialized matcher
	logger, _ := zap.NewDevelopment()

	service := NewService(nil, nil, nil, logger)

	// Service should have a matcher
	if service.matcher == nil {
		t.Error("expected matcher to be initialized")
	}

	// Matcher should have no watched addresses initially
	if service.matcher.HasWatchedAddresses("any-chain") {
		t.Error("expected no watched addresses initially")
	}
}

func TestListFilterStruct(t *testing.T) {
	// Test ListFilter struct creation
	filter := &ListFilter{
		ChainID: "chain-1",
		Limit:   10,
		Offset:  5,
	}

	if filter.ChainID != "chain-1" {
		t.Error("ChainID not set correctly")
	}

	if filter.Limit != 10 {
		t.Error("Limit not set correctly")
	}

	if filter.Offset != 5 {
		t.Error("Offset not set correctly")
	}
}

func TestServiceUnsubscribeNotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	service := NewService(nil, nil, nil, logger)

	err := service.Unsubscribe(ctx, "nonexistent-sub")
	if err != ErrSubscriberNotFound {
		t.Errorf("expected ErrSubscriberNotFound, got %v", err)
	}
}

func TestWatchEventBusEvent(t *testing.T) {
	watchEvent := &WatchEvent{
		ID:          "event-1",
		AddressID:   "addr-1",
		ChainID:     "chain-1",
		EventType:   WatchEventTypeTxFrom,
		BlockNumber: 100,
		Timestamp:   time.Now(),
	}

	busEvent := &WatchEventBusEvent{
		WatchEvent: watchEvent,
	}

	if busEvent.Type() != "watch_event" {
		t.Errorf("expected type 'watch_event', got '%s'", busEvent.Type())
	}

	if busEvent.Timestamp().IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestWatchEventBusEventNilWatchEvent(t *testing.T) {
	busEvent := &WatchEventBusEvent{
		WatchEvent: nil,
	}

	if busEvent.Type() != "watch_event" {
		t.Errorf("expected type 'watch_event', got '%s'", busEvent.Type())
	}

	// Timestamp should be zero when WatchEvent is nil
	if !busEvent.Timestamp().IsZero() {
		t.Error("expected zero timestamp for nil WatchEvent")
	}
}

func TestServiceStartWithStorage(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)

	err := service.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Double start should be idempotent
	err = service.Start(ctx)
	if err != nil {
		t.Fatalf("second start should be idempotent: %v", err)
	}

	// Cleanup
	service.Stop(ctx)
}

func TestServiceWatchAddress(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Watch an address
	req := &WatchRequest{
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
		Label:   "Test Address",
		Filter:  DefaultWatchFilter(),
	}

	watched, err := service.WatchAddress(ctx, req)
	if err != nil {
		t.Fatalf("watch address failed: %v", err)
	}

	if watched == nil {
		t.Fatal("expected watched address to not be nil")
	}

	if watched.ID == "" {
		t.Error("expected ID to be generated")
	}

	if watched.Address != req.Address {
		t.Errorf("address mismatch: expected %s, got %s", req.Address.Hex(), watched.Address.Hex())
	}

	if watched.ChainID != req.ChainID {
		t.Errorf("chain ID mismatch: expected %s, got %s", req.ChainID, watched.ChainID)
	}

	if watched.Label != req.Label {
		t.Errorf("label mismatch: expected %s, got %s", req.Label, watched.Label)
	}
}

func TestServiceWatchAddressInvalidAddress(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Try to watch a zero address
	req := &WatchRequest{
		Address: common.Address{},
		ChainID: "chain-1",
	}

	_, err := service.WatchAddress(ctx, req)
	if err != ErrInvalidAddress {
		t.Errorf("expected ErrInvalidAddress, got %v", err)
	}
}

func TestServiceWatchAddressNilFilter(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Watch with nil filter (should use default)
	req := &WatchRequest{
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
		Filter:  nil,
	}

	watched, err := service.WatchAddress(ctx, req)
	if err != nil {
		t.Fatalf("watch address failed: %v", err)
	}

	if watched.Filter == nil {
		t.Error("expected default filter to be set")
	}
}

func TestServiceWatchAddressDuplicate(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	req := &WatchRequest{
		Address: addr,
		ChainID: "chain-1",
	}

	// First watch
	_, err := service.WatchAddress(ctx, req)
	if err != nil {
		t.Fatalf("first watch failed: %v", err)
	}

	// Duplicate watch
	_, err = service.WatchAddress(ctx, req)
	if err != ErrAddressAlreadyExists {
		t.Errorf("expected ErrAddressAlreadyExists, got %v", err)
	}
}

func TestServiceGetWatchedAddress(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Watch an address
	req := &WatchRequest{
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
		Label:   "Test",
	}

	watched, _ := service.WatchAddress(ctx, req)

	// Get by ID
	retrieved, err := service.GetWatchedAddress(ctx, watched.ID)
	if err != nil {
		t.Fatalf("get watched address failed: %v", err)
	}

	if retrieved.ID != watched.ID {
		t.Errorf("ID mismatch: expected %s, got %s", watched.ID, retrieved.ID)
	}
}

func TestServiceGetWatchedAddressNotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	_, err := service.GetWatchedAddress(ctx, "nonexistent")
	if err != ErrAddressNotFound {
		t.Errorf("expected ErrAddressNotFound, got %v", err)
	}
}

func TestServiceGetWatchedAddressByEthAddress(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	req := &WatchRequest{
		Address: addr,
		ChainID: "chain-1",
	}

	watched, _ := service.WatchAddress(ctx, req)

	// Get by ethereum address
	retrieved, err := service.GetWatchedAddressByEthAddress(ctx, "chain-1", addr)
	if err != nil {
		t.Fatalf("get by eth address failed: %v", err)
	}

	if retrieved.ID != watched.ID {
		t.Errorf("ID mismatch: expected %s, got %s", watched.ID, retrieved.ID)
	}
}

func TestServiceUnwatchAddress(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Watch an address
	req := &WatchRequest{
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
	}

	watched, _ := service.WatchAddress(ctx, req)

	// Unwatch
	err := service.UnwatchAddress(ctx, watched.ID)
	if err != nil {
		t.Fatalf("unwatch failed: %v", err)
	}

	// Verify it's gone
	_, err = service.GetWatchedAddress(ctx, watched.ID)
	if err != ErrAddressNotFound {
		t.Errorf("expected ErrAddressNotFound after unwatch, got %v", err)
	}
}

func TestServiceUnwatchAddressNotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	err := service.UnwatchAddress(ctx, "nonexistent")
	if err != ErrAddressNotFound {
		t.Errorf("expected ErrAddressNotFound, got %v", err)
	}
}

func TestServiceListWatchedAddresses(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Watch multiple addresses
	for i := 0; i < 3; i++ {
		addr := common.BigToAddress(big.NewInt(int64(i + 1)))
		req := &WatchRequest{
			Address: addr,
			ChainID: "chain-1",
		}
		_, err := service.WatchAddress(ctx, req)
		if err != nil {
			t.Fatalf("watch address %d failed: %v", i, err)
		}
	}

	// List all
	addresses, err := service.ListWatchedAddresses(ctx, nil)
	if err != nil {
		t.Fatalf("list addresses failed: %v", err)
	}

	if len(addresses) != 3 {
		t.Errorf("expected 3 addresses, got %d", len(addresses))
	}
}

func TestServiceListWatchedAddressesWithLimit(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Watch multiple addresses
	for i := 0; i < 5; i++ {
		addr := common.BigToAddress(big.NewInt(int64(i + 1)))
		req := &WatchRequest{
			Address: addr,
			ChainID: "chain-1",
		}
		service.WatchAddress(ctx, req)
	}

	// List with limit
	filter := &ListFilter{
		Limit: 2,
	}
	addresses, err := service.ListWatchedAddresses(ctx, filter)
	if err != nil {
		t.Fatalf("list addresses failed: %v", err)
	}

	if len(addresses) > 2 {
		t.Errorf("expected at most 2 addresses, got %d", len(addresses))
	}
}

func TestServiceSubscribe(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Watch an address
	req := &WatchRequest{
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
	}
	watched, _ := service.WatchAddress(ctx, req)

	// Subscribe
	sub := &Subscriber{}
	subID, err := service.Subscribe(ctx, watched.ID, sub)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	if subID == "" {
		t.Error("expected subscription ID to be generated")
	}
}

func TestServiceSubscribeToNonexistent(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	sub := &Subscriber{}
	_, err := service.Subscribe(ctx, "nonexistent", sub)
	if err != ErrAddressNotFound {
		t.Errorf("expected ErrAddressNotFound, got %v", err)
	}
}

func TestServiceUnsubscribe(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Watch and subscribe
	req := &WatchRequest{
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
	}
	watched, _ := service.WatchAddress(ctx, req)

	sub := &Subscriber{}
	subID, _ := service.Subscribe(ctx, watched.ID, sub)

	// Unsubscribe
	err := service.Unsubscribe(ctx, subID)
	if err != nil {
		t.Fatalf("unsubscribe failed: %v", err)
	}

	// Double unsubscribe should fail
	err = service.Unsubscribe(ctx, subID)
	if err != ErrSubscriberNotFound {
		t.Errorf("expected ErrSubscriberNotFound, got %v", err)
	}
}

func TestServiceProcessBlockNotRunning(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	// Don't start service

	err := service.ProcessBlock(ctx, "chain-1", nil, nil)
	if err != ErrServiceNotRunning {
		t.Errorf("expected ErrServiceNotRunning, got %v", err)
	}
}

func TestServiceProcessBlockNoWatchedAddresses(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Process block with no watched addresses - should return nil immediately
	err := service.ProcessBlock(ctx, "chain-1", nil, nil)
	if err != nil {
		t.Errorf("expected nil error for no watched addresses, got %v", err)
	}
}

func TestServiceGetRecentEvents(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Get recent events (empty)
	events, err := service.GetRecentEvents(ctx, "addr-1", 10)
	if err != nil {
		t.Fatalf("get recent events failed: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestServiceGetRecentEventsDefaultLimit(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Get with invalid limit (should use default)
	events, err := service.GetRecentEvents(ctx, "addr-1", 0)
	if err != nil {
		t.Fatalf("get recent events failed: %v", err)
	}

	// Should not error with limit 0 (defaults to 50)
	// Note: nil or empty slice both represent "no events", so both are acceptable
	if len(events) != 0 {
		t.Error("expected 0 events")
	}
}

func TestServiceUnwatchAddressWithSubscribers(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	mockStore := newMockStorage()

	service := NewService(nil, mockStore, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer service.Stop(ctx)

	// Watch and subscribe
	req := &WatchRequest{
		Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		ChainID: "chain-1",
	}
	watched, _ := service.WatchAddress(ctx, req)

	sub := &Subscriber{}
	subID, _ := service.Subscribe(ctx, watched.ID, sub)

	// Unwatch should also remove subscribers
	err := service.UnwatchAddress(ctx, watched.ID)
	if err != nil {
		t.Fatalf("unwatch failed: %v", err)
	}

	// Subscription should be removed
	err = service.Unsubscribe(ctx, subID)
	if err != ErrSubscriberNotFound {
		t.Errorf("expected subscription to be removed with address, got %v", err)
	}
}

