package storage

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestSetCodeStorage(t *testing.T) (*PebbleStorage, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "setcode-test-*")
	require.NoError(t, err)

	cfg := &Config{
		Path:                  filepath.Join(tmpDir, "test.db"),
		Cache:                 64,
		CompactionConcurrency: 1,
		MaxOpenFiles:          100,
		WriteBuffer:           64,
	}

	storage, err := NewPebbleStorage(cfg)
	require.NoError(t, err)

	cleanup := func() {
		storage.Close()
		os.RemoveAll(tmpDir)
	}

	return storage, cleanup
}

func TestSaveSetCodeAuthorization(t *testing.T) {
	storage, cleanup := setupTestSetCodeStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create test record
	record := &SetCodeAuthorizationRecord{
		TxHash:           common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		BlockNumber:      100,
		BlockHash:        common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
		TxIndex:          5,
		AuthIndex:        0,
		TargetAddress:    common.HexToAddress("0xaaaa000000000000000000000000000000000001"),
		AuthorityAddress: common.HexToAddress("0xbbbb000000000000000000000000000000000002"),
		ChainID:          big.NewInt(1),
		Nonce:            10,
		YParity:          1,
		R:                big.NewInt(12345),
		S:                big.NewInt(67890),
		Applied:          true,
		Error:            "",
		Timestamp:        time.Now(),
	}

	// Save
	err := storage.SaveSetCodeAuthorization(ctx, record)
	require.NoError(t, err)

	// Retrieve
	retrieved, err := storage.GetSetCodeAuthorization(ctx, record.TxHash, record.AuthIndex)
	require.NoError(t, err)
	assert.Equal(t, record.TxHash, retrieved.TxHash)
	assert.Equal(t, record.BlockNumber, retrieved.BlockNumber)
	assert.Equal(t, record.TargetAddress, retrieved.TargetAddress)
	assert.Equal(t, record.AuthorityAddress, retrieved.AuthorityAddress)
	assert.Equal(t, record.Applied, retrieved.Applied)
}

func TestGetSetCodeAuthorizationsByTx(t *testing.T) {
	storage, cleanup := setupTestSetCodeStorage(t)
	defer cleanup()

	ctx := context.Background()
	txHash := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")

	// Create multiple authorizations for same tx
	records := []*SetCodeAuthorizationRecord{
		{
			TxHash:           txHash,
			BlockNumber:      100,
			BlockHash:        common.HexToHash("0xaaa"),
			TxIndex:          1,
			AuthIndex:        0,
			TargetAddress:    common.HexToAddress("0xaaaa000000000000000000000000000000000001"),
			AuthorityAddress: common.HexToAddress("0xbbbb000000000000000000000000000000000002"),
			ChainID:          big.NewInt(1),
			Nonce:            1,
			Applied:          true,
			Timestamp:        time.Now(),
		},
		{
			TxHash:           txHash,
			BlockNumber:      100,
			BlockHash:        common.HexToHash("0xaaa"),
			TxIndex:          1,
			AuthIndex:        1,
			TargetAddress:    common.HexToAddress("0xcccc000000000000000000000000000000000003"),
			AuthorityAddress: common.HexToAddress("0xdddd000000000000000000000000000000000004"),
			ChainID:          big.NewInt(1),
			Nonce:            2,
			Applied:          false,
			Error:            SetCodeErrNonceMismatch,
			Timestamp:        time.Now(),
		},
	}

	// Save batch
	err := storage.SaveSetCodeAuthorizations(ctx, records)
	require.NoError(t, err)

	// Retrieve by tx
	retrieved, err := storage.GetSetCodeAuthorizationsByTx(ctx, txHash)
	require.NoError(t, err)
	assert.Len(t, retrieved, 2)
}

func TestGetSetCodeAuthorizationsByTarget(t *testing.T) {
	storage, cleanup := setupTestSetCodeStorage(t)
	defer cleanup()

	ctx := context.Background()
	targetAddr := common.HexToAddress("0xaaaa000000000000000000000000000000000001")

	// Create multiple authorizations for same target
	for i := 0; i < 5; i++ {
		record := &SetCodeAuthorizationRecord{
			TxHash:           common.HexToHash("0x" + string(rune('1'+i)) + "111111111111111111111111111111111111111111111111111111111111111"),
			BlockNumber:      uint64(100 + i),
			BlockHash:        common.HexToHash("0xaaa"),
			TxIndex:          uint64(i),
			AuthIndex:        0,
			TargetAddress:    targetAddr,
			AuthorityAddress: common.HexToAddress("0xbbbb000000000000000000000000000000000002"),
			ChainID:          big.NewInt(1),
			Nonce:            uint64(i),
			Applied:          true,
			Timestamp:        time.Now(),
		}
		err := storage.SaveSetCodeAuthorization(ctx, record)
		require.NoError(t, err)
	}

	// Get by target with pagination
	records, err := storage.GetSetCodeAuthorizationsByTarget(ctx, targetAddr, 3, 0)
	require.NoError(t, err)
	assert.Len(t, records, 3)

	// Check ordering (newest first)
	assert.True(t, records[0].BlockNumber >= records[1].BlockNumber)

	// Get count
	count, err := storage.GetSetCodeAuthorizationsCountByTarget(ctx, targetAddr)
	require.NoError(t, err)
	assert.Equal(t, 5, count)
}

func TestGetSetCodeAuthorizationsByAuthority(t *testing.T) {
	storage, cleanup := setupTestSetCodeStorage(t)
	defer cleanup()

	ctx := context.Background()
	authorityAddr := common.HexToAddress("0xbbbb000000000000000000000000000000000002")

	// Create multiple authorizations for same authority
	for i := 0; i < 3; i++ {
		record := &SetCodeAuthorizationRecord{
			TxHash:           common.HexToHash("0x" + string(rune('a'+i)) + "111111111111111111111111111111111111111111111111111111111111111"),
			BlockNumber:      uint64(200 + i),
			BlockHash:        common.HexToHash("0xbbb"),
			TxIndex:          uint64(i),
			AuthIndex:        0,
			TargetAddress:    common.HexToAddress("0xcccc000000000000000000000000000000000003"),
			AuthorityAddress: authorityAddr,
			ChainID:          big.NewInt(1),
			Nonce:            uint64(i),
			Applied:          true,
			Timestamp:        time.Now(),
		}
		err := storage.SaveSetCodeAuthorization(ctx, record)
		require.NoError(t, err)
	}

	// Get by authority
	records, err := storage.GetSetCodeAuthorizationsByAuthority(ctx, authorityAddr, 10, 0)
	require.NoError(t, err)
	assert.Len(t, records, 3)

	// Get count
	count, err := storage.GetSetCodeAuthorizationsCountByAuthority(ctx, authorityAddr)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestGetSetCodeAuthorizationsByBlock(t *testing.T) {
	storage, cleanup := setupTestSetCodeStorage(t)
	defer cleanup()

	ctx := context.Background()
	blockNumber := uint64(500)

	// Create authorizations in same block
	for i := 0; i < 4; i++ {
		record := &SetCodeAuthorizationRecord{
			TxHash:           common.HexToHash("0x" + string(rune('1'+i)) + "222222222222222222222222222222222222222222222222222222222222222"),
			BlockNumber:      blockNumber,
			BlockHash:        common.HexToHash("0xccc"),
			TxIndex:          uint64(i),
			AuthIndex:        0,
			TargetAddress:    common.HexToAddress("0xdddd000000000000000000000000000000000004"),
			AuthorityAddress: common.HexToAddress("0xeeee000000000000000000000000000000000005"),
			ChainID:          big.NewInt(1),
			Nonce:            uint64(i),
			Applied:          true,
			Timestamp:        time.Now(),
		}
		err := storage.SaveSetCodeAuthorization(ctx, record)
		require.NoError(t, err)
	}

	// Get by block
	records, err := storage.GetSetCodeAuthorizationsByBlock(ctx, blockNumber)
	require.NoError(t, err)
	assert.Len(t, records, 4)
}

func TestAddressDelegationState(t *testing.T) {
	storage, cleanup := setupTestSetCodeStorage(t)
	defer cleanup()

	ctx := context.Background()
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	targetAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// Get initial state (no delegation)
	state, err := storage.GetAddressDelegationState(ctx, addr)
	require.NoError(t, err)
	assert.False(t, state.HasDelegation)
	assert.Nil(t, state.DelegationTarget)

	// Update state with delegation
	state.HasDelegation = true
	state.DelegationTarget = &targetAddr
	state.LastUpdatedBlock = 100
	state.LastUpdatedTxHash = common.HexToHash("0xabc")

	err = storage.UpdateAddressDelegationState(ctx, state)
	require.NoError(t, err)

	// Retrieve updated state
	retrieved, err := storage.GetAddressDelegationState(ctx, addr)
	require.NoError(t, err)
	assert.True(t, retrieved.HasDelegation)
	assert.NotNil(t, retrieved.DelegationTarget)
	assert.Equal(t, targetAddr, *retrieved.DelegationTarget)
}

func TestAddressSetCodeStats(t *testing.T) {
	storage, cleanup := setupTestSetCodeStorage(t)
	defer cleanup()

	ctx := context.Background()
	addr := common.HexToAddress("0x3333333333333333333333333333333333333333")

	// Get initial stats (zero values)
	stats, err := storage.GetAddressSetCodeStats(ctx, addr)
	require.NoError(t, err)
	assert.Equal(t, 0, stats.AsTargetCount)
	assert.Equal(t, 0, stats.AsAuthorityCount)

	// Increment as target
	err = storage.IncrementSetCodeStats(ctx, addr, true, false, 100)
	require.NoError(t, err)

	// Increment as authority
	err = storage.IncrementSetCodeStats(ctx, addr, false, true, 101)
	require.NoError(t, err)

	// Increment both
	err = storage.IncrementSetCodeStats(ctx, addr, true, true, 102)
	require.NoError(t, err)

	// Get updated stats
	stats, err = storage.GetAddressSetCodeStats(ctx, addr)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.AsTargetCount)
	assert.Equal(t, 2, stats.AsAuthorityCount)
	assert.Equal(t, uint64(102), stats.LastActivityBlock)
}

func TestGetRecentSetCodeAuthorizations(t *testing.T) {
	storage, cleanup := setupTestSetCodeStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Create authorizations in different blocks
	for i := 0; i < 10; i++ {
		record := &SetCodeAuthorizationRecord{
			TxHash:           common.HexToHash("0x" + string(rune('0'+i)) + "333333333333333333333333333333333333333333333333333333333333333"),
			BlockNumber:      uint64(1000 + i),
			BlockHash:        common.HexToHash("0xddd"),
			TxIndex:          0,
			AuthIndex:        0,
			TargetAddress:    common.HexToAddress("0xffff000000000000000000000000000000000006"),
			AuthorityAddress: common.HexToAddress("0x1111111111111111111111111111111111111111"),
			ChainID:          big.NewInt(1),
			Nonce:            uint64(i),
			Applied:          true,
			Timestamp:        time.Now(),
		}
		err := storage.SaveSetCodeAuthorization(ctx, record)
		require.NoError(t, err)
	}

	// Get recent (should be newest first)
	records, err := storage.GetRecentSetCodeAuthorizations(ctx, 5)
	require.NoError(t, err)
	assert.Len(t, records, 5)

	// Verify ordering (newest first)
	for i := 1; i < len(records); i++ {
		assert.True(t, records[i-1].BlockNumber >= records[i].BlockNumber,
			"expected records[%d].BlockNumber >= records[%d].BlockNumber, got %d < %d",
			i-1, i, records[i-1].BlockNumber, records[i].BlockNumber)
	}
}

func TestGetSetCodeTransactionCount(t *testing.T) {
	storage, cleanup := setupTestSetCodeStorage(t)
	defer cleanup()

	ctx := context.Background()

	// Initial count should be 0
	count, err := storage.GetSetCodeTransactionCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Add some records
	for i := 0; i < 7; i++ {
		record := &SetCodeAuthorizationRecord{
			TxHash:           common.HexToHash("0x" + string(rune('a'+i)) + "444444444444444444444444444444444444444444444444444444444444444"),
			BlockNumber:      uint64(2000 + i),
			BlockHash:        common.HexToHash("0xeee"),
			TxIndex:          0,
			AuthIndex:        0,
			TargetAddress:    common.HexToAddress("0x2222222222222222222222222222222222222222"),
			AuthorityAddress: common.HexToAddress("0x3333333333333333333333333333333333333333"),
			ChainID:          big.NewInt(1),
			Nonce:            uint64(i),
			Applied:          true,
			Timestamp:        time.Now(),
		}
		err := storage.SaveSetCodeAuthorization(ctx, record)
		require.NoError(t, err)
	}

	// Count should be 7
	count, err = storage.GetSetCodeTransactionCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, 7, count)
}

func TestParseDelegation(t *testing.T) {
	tests := []struct {
		name     string
		code     []byte
		wantAddr common.Address
		wantOk   bool
	}{
		{
			name:     "valid delegation",
			code:     AddressToDelegation(common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")),
			wantAddr: common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
			wantOk:   true,
		},
		{
			name:     "empty code",
			code:     []byte{},
			wantAddr: common.Address{},
			wantOk:   false,
		},
		{
			name:     "wrong length",
			code:     []byte{0xef, 0x01, 0x00, 0x12},
			wantAddr: common.Address{},
			wantOk:   false,
		},
		{
			name:     "wrong prefix",
			code:     []byte{0xff, 0x01, 0x00, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78},
			wantAddr: common.Address{},
			wantOk:   false,
		},
		{
			name:     "contract code (not delegation)",
			code:     []byte{0x60, 0x80, 0x60, 0x40}, // PUSH1 0x80 PUSH1 0x40
			wantAddr: common.Address{},
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, ok := ParseDelegation(tt.code)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.wantAddr, addr)
			}
		})
	}
}

func TestAddressToDelegation(t *testing.T) {
	addr := common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12")
	code := AddressToDelegation(addr)

	// Check length
	assert.Equal(t, DelegationCodeLength, len(code))

	// Check prefix
	assert.Equal(t, DelegationPrefix[0], code[0])
	assert.Equal(t, DelegationPrefix[1], code[1])
	assert.Equal(t, DelegationPrefix[2], code[2])

	// Check address
	assert.Equal(t, addr.Bytes(), code[3:])

	// Verify round-trip
	parsed, ok := ParseDelegation(code)
	assert.True(t, ok)
	assert.Equal(t, addr, parsed)
}

func TestIsDelegation(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Valid delegation
	assert.True(t, IsDelegation(AddressToDelegation(addr)))

	// Not a delegation
	assert.False(t, IsDelegation([]byte{0x60, 0x80}))
	assert.False(t, IsDelegation(nil))
	assert.False(t, IsDelegation([]byte{}))
}
