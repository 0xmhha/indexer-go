package jsonrpc

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// Mock WBFT storage for testing
type mockWBFTStorage struct {
	storage.Storage
}

func (m *mockWBFTStorage) GetWBFTBlockExtra(ctx context.Context, blockNumber uint64) (*storage.WBFTBlockExtra, error) {
	if blockNumber == 0 {
		return nil, storage.ErrNotFound
	}
	return &storage.WBFTBlockExtra{
		BlockNumber:  blockNumber,
		BlockHash:    common.HexToHash("0x1234"),
		RandaoReveal: []byte{0x01, 0x02, 0x03},
		Round:        1,
		PrevRound:    0,
		Timestamp:    1234567890,
		GasTip:       big.NewInt(1000000000),
	}, nil
}

func (m *mockWBFTStorage) GetWBFTBlockExtraByHash(ctx context.Context, blockHash common.Hash) (*storage.WBFTBlockExtra, error) {
	return &storage.WBFTBlockExtra{
		BlockNumber:  100,
		BlockHash:    blockHash,
		RandaoReveal: []byte{0x01, 0x02, 0x03},
		Round:        1,
		PrevRound:    0,
		Timestamp:    1234567890,
	}, nil
}

func (m *mockWBFTStorage) GetEpochInfo(ctx context.Context, epochNumber uint64) (*storage.EpochInfo, error) {
	if epochNumber == 0 {
		return nil, storage.ErrNotFound
	}
	return &storage.EpochInfo{
		EpochNumber: epochNumber,
		BlockNumber: epochNumber * 100,
		Candidates: []storage.Candidate{
			{
				Address:   common.HexToAddress("0x1111"),
				Diligence: 100,
			},
		},
		Validators:    []uint32{0},
		BLSPublicKeys: [][]byte{{0x01, 0x02}},
	}, nil
}

func (m *mockWBFTStorage) GetLatestEpochInfo(ctx context.Context) (*storage.EpochInfo, error) {
	return m.GetEpochInfo(ctx, 10)
}

func (m *mockWBFTStorage) GetValidatorSigningStats(ctx context.Context, validator common.Address, fromBlock, toBlock uint64) (*storage.ValidatorSigningStats, error) {
	return &storage.ValidatorSigningStats{
		ValidatorAddress: validator,
		ValidatorIndex:   0,
		PrepareSignCount: 100,
		PrepareMissCount: 10,
		CommitSignCount:  95,
		CommitMissCount:  15,
		FromBlock:        fromBlock,
		ToBlock:          toBlock,
		SigningRate:      0.85,
	}, nil
}

func (m *mockWBFTStorage) GetAllValidatorsSigningStats(ctx context.Context, fromBlock, toBlock uint64, limit, offset int) ([]*storage.ValidatorSigningStats, error) {
	return []*storage.ValidatorSigningStats{
		{
			ValidatorAddress: common.HexToAddress("0x1111"),
			ValidatorIndex:   0,
			PrepareSignCount: 100,
			PrepareMissCount: 10,
			CommitSignCount:  95,
			CommitMissCount:  15,
			FromBlock:        fromBlock,
			ToBlock:          toBlock,
			SigningRate:      0.85,
		},
	}, nil
}

func (m *mockWBFTStorage) GetValidatorSigningActivity(ctx context.Context, validator common.Address, fromBlock, toBlock uint64, limit, offset int) ([]*storage.ValidatorSigningActivity, error) {
	return []*storage.ValidatorSigningActivity{
		{
			BlockNumber:      100,
			BlockHash:        common.HexToHash("0x1234"),
			ValidatorAddress: validator,
			ValidatorIndex:   0,
			SignedPrepare:    true,
			SignedCommit:     true,
			Round:            1,
			Timestamp:        1234567890,
		},
	}, nil
}

func (m *mockWBFTStorage) GetBlockSigners(ctx context.Context, blockNumber uint64) ([]common.Address, []common.Address, error) {
	preparers := []common.Address{
		common.HexToAddress("0x1111"),
		common.HexToAddress("0x2222"),
	}
	committers := []common.Address{
		common.HexToAddress("0x1111"),
		common.HexToAddress("0x3333"),
	}
	return preparers, committers, nil
}

func (m *mockWBFTStorage) GetEpochsList(ctx context.Context, limit, offset int) ([]*storage.EpochInfo, int, error) {
	return []*storage.EpochInfo{}, 0, nil
}
func (m *mockWBFTStorage) GetAddressStats(ctx context.Context, addr common.Address) (*storage.AddressStats, error) {
	return nil, nil
}

func (m *mockWBFTStorage) ListABIs(ctx context.Context) ([]common.Address, error) {
	return []common.Address{}, nil
}

func TestGetWBFTBlockExtra(t *testing.T) {
	logger := zap.NewNop()
	handler := NewHandler(&mockWBFTStorage{}, logger)

	tests := []struct {
		name        string
		params      string
		expectError bool
	}{
		{
			name:        "valid block number",
			params:      `{"blockNumber": 100}`,
			expectError: false,
		},
		{
			name:        "valid block number as string",
			params:      `{"blockNumber": "100"}`,
			expectError: false,
		},
		{
			name:        "block not found",
			params:      `{"blockNumber": 0}`,
			expectError: false, // Returns nil, not error
		},
		{
			name:        "missing parameter",
			params:      `{}`,
			expectError: true,
		},
		{
			name:        "invalid parameter type",
			params:      `{"blockNumber": "invalid"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := handler.getWBFTBlockExtra(context.Background(), json.RawMessage(tt.params))
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != nil {
					if m, ok := result.(map[string]interface{}); ok {
						if m["blockNumber"] == nil {
							t.Error("expected blockNumber in result")
						}
					}
				}
			}
		})
	}
}

func TestGetEpochInfo(t *testing.T) {
	logger := zap.NewNop()
	handler := NewHandler(&mockWBFTStorage{}, logger)

	tests := []struct {
		name        string
		params      string
		expectError bool
	}{
		{
			name:        "valid epoch number",
			params:      `{"epochNumber": 5}`,
			expectError: false,
		},
		{
			name:        "valid epoch number as string",
			params:      `{"epochNumber": "5"}`,
			expectError: false,
		},
		{
			name:        "epoch not found",
			params:      `{"epochNumber": 0}`,
			expectError: false,
		},
		{
			name:        "missing parameter",
			params:      `{}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := handler.getEpochInfo(context.Background(), json.RawMessage(tt.params))
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != nil {
					if m, ok := result.(map[string]interface{}); ok {
						if m["epochNumber"] == nil {
							t.Error("expected epochNumber in result")
						}
					}
				}
			}
		})
	}
}

func TestGetLatestEpochInfo(t *testing.T) {
	logger := zap.NewNop()
	handler := NewHandler(&mockWBFTStorage{}, logger)

	result, err := handler.getLatestEpochInfo(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected result, got nil")
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}

	if m["epochNumber"] == nil {
		t.Error("expected epochNumber in result")
	}
}

func TestGetValidatorSigningStats(t *testing.T) {
	logger := zap.NewNop()
	handler := NewHandler(&mockWBFTStorage{}, logger)

	tests := []struct {
		name        string
		params      string
		expectError bool
	}{
		{
			name:        "valid parameters",
			params:      `{"validatorAddress": "0x1111111111111111111111111111111111111111", "fromBlock": 100, "toBlock": 200}`,
			expectError: false,
		},
		{
			name:        "valid parameters as strings",
			params:      `{"validatorAddress": "0x1111111111111111111111111111111111111111", "fromBlock": "100", "toBlock": "200"}`,
			expectError: false,
		},
		{
			name:        "missing validatorAddress",
			params:      `{"fromBlock": 100, "toBlock": 200}`,
			expectError: true,
		},
		{
			name:        "missing fromBlock",
			params:      `{"validatorAddress": "0x1111111111111111111111111111111111111111", "toBlock": 200}`,
			expectError: true,
		},
		{
			name:        "missing toBlock",
			params:      `{"validatorAddress": "0x1111111111111111111111111111111111111111", "fromBlock": 100}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := handler.getValidatorSigningStats(context.Background(), json.RawMessage(tt.params))
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Error("expected result, got nil")
				}
			}
		})
	}
}

func TestGetBlockSigners(t *testing.T) {
	logger := zap.NewNop()
	handler := NewHandler(&mockWBFTStorage{}, logger)

	result, err := handler.getBlockSigners(context.Background(), json.RawMessage(`{"blockNumber": 100}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected result, got nil")
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}

	if m["preparers"] == nil || m["committers"] == nil {
		t.Error("expected preparers and committers in result")
	}

	preparers, ok := m["preparers"].([]string)
	if !ok || len(preparers) != 2 {
		t.Error("expected 2 preparers")
	}

	committers, ok := m["committers"].([]string)
	if !ok || len(committers) != 2 {
		t.Error("expected 2 committers")
	}
}
