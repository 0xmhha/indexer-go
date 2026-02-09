package fetch

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

func TestNewSetCodeProcessor(t *testing.T) {
	p := NewSetCodeProcessor(zap.NewNop(), nil)
	if p == nil {
		t.Fatal("expected non-nil processor")
	}
}

func TestIsSetCodeTransaction_Legacy(t *testing.T) {
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &common.Address{},
		Value:    big.NewInt(0),
	})

	if IsSetCodeTransaction(tx) {
		t.Error("expected legacy tx not to be SetCode")
	}
}

func TestIsSetCodeTransaction_DynamicFee(t *testing.T) {
	tx := types.NewTx(&types.DynamicFeeTx{
		Nonce:     0,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(2000000000),
		Gas:       21000,
		To:        &common.Address{},
		Value:     big.NewInt(0),
	})

	if IsSetCodeTransaction(tx) {
		t.Error("expected dynamic fee tx not to be SetCode")
	}
}

func TestGetSetCodeAuthorizationCount_NonSetCode(t *testing.T) {
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &common.Address{},
		Value:    big.NewInt(0),
	})

	if GetSetCodeAuthorizationCount(tx) != 0 {
		t.Error("expected 0 for non-SetCode tx")
	}
}

func TestExtractSetCodeAuthorizationsFromTx_NonSetCode(t *testing.T) {
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &common.Address{},
		Value:    big.NewInt(0),
	})

	result := ExtractSetCodeAuthorizationsFromTx(tx, 100, common.Hash{}, 0, time.Now())
	if result != nil {
		t.Error("expected nil for non-SetCode tx")
	}
}

func TestCalculateSetCodeIntrinsicGas_NonSetCode(t *testing.T) {
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &common.Address{},
		Value:    big.NewInt(0),
	})

	if CalculateSetCodeIntrinsicGas(tx) != 0 {
		t.Error("expected 0 for non-SetCode tx")
	}
}

func TestGetSetCodeGasBreakdown_NonSetCode(t *testing.T) {
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &common.Address{},
		Value:    big.NewInt(0),
	})

	result := GetSetCodeGasBreakdown(tx)
	if result != nil {
		t.Error("expected nil for non-SetCode tx")
	}
}

func TestCalculateSetCodeTxStats_Empty(t *testing.T) {
	stats := CalculateSetCodeTxStats(nil)
	if stats.TotalTransactions != 0 || stats.TotalAuthorizations != 0 {
		t.Error("expected zero stats for nil records")
	}

	stats = CalculateSetCodeTxStats([]*storagepkg.SetCodeAuthorizationRecord{})
	if stats.TotalTransactions != 0 {
		t.Error("expected zero stats for empty records")
	}
}

func TestCalculateSetCodeTxStats_WithRecords(t *testing.T) {
	txHash1 := common.HexToHash("0xaaa")
	txHash2 := common.HexToHash("0xbbb")
	target1 := common.HexToAddress("0x111")
	target2 := common.HexToAddress("0x222")
	auth1 := common.HexToAddress("0x333")
	auth2 := common.HexToAddress("0x444")

	records := []*storagepkg.SetCodeAuthorizationRecord{
		{TxHash: txHash1, TargetAddress: target1, AuthorityAddress: auth1, Applied: true},
		{TxHash: txHash1, TargetAddress: target2, AuthorityAddress: auth2, Applied: true},
		{TxHash: txHash2, TargetAddress: target1, AuthorityAddress: auth1, Applied: false},
	}

	stats := CalculateSetCodeTxStats(records)

	if stats.TotalTransactions != 2 {
		t.Errorf("expected 2 unique txs, got %d", stats.TotalTransactions)
	}
	if stats.TotalAuthorizations != 3 {
		t.Errorf("expected 3 total auths, got %d", stats.TotalAuthorizations)
	}
	if stats.AppliedCount != 2 {
		t.Errorf("expected 2 applied, got %d", stats.AppliedCount)
	}
	if stats.FailedCount != 1 {
		t.Errorf("expected 1 failed, got %d", stats.FailedCount)
	}
	if stats.UniqueTargets != 2 {
		t.Errorf("expected 2 unique targets, got %d", stats.UniqueTargets)
	}
	if stats.UniqueAuthorities != 2 {
		t.Errorf("expected 2 unique authorities, got %d", stats.UniqueAuthorities)
	}
}

func TestCalculateSetCodeTxStats_ZeroAuthority(t *testing.T) {
	records := []*storagepkg.SetCodeAuthorizationRecord{
		{
			TxHash:           common.HexToHash("0xaaa"),
			TargetAddress:    common.HexToAddress("0x111"),
			AuthorityAddress: common.Address{}, // Zero address
			Applied:          false,
		},
	}

	stats := CalculateSetCodeTxStats(records)
	if stats.UniqueAuthorities != 0 {
		t.Errorf("expected 0 unique authorities (zero addr excluded), got %d", stats.UniqueAuthorities)
	}
}

func TestProcessSetCodeTransaction_NonSetCode(t *testing.T) {
	p := NewSetCodeProcessor(zap.NewNop(), nil)

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &common.Address{},
		Value:    big.NewInt(0),
	})

	header := &types.Header{Number: big.NewInt(1)}
	block := types.NewBlockWithHeader(header)

	// Should return nil for non-SetCode tx
	err := p.ProcessSetCodeTransaction(nil, tx, nil, block, 0)
	if err != nil {
		t.Errorf("expected nil error for non-SetCode tx, got %v", err)
	}
}

func TestProcessSetCodeTransactionBatch_NoSetCodeTxs(t *testing.T) {
	p := NewSetCodeProcessor(zap.NewNop(), nil)

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &common.Address{},
		Value:    big.NewInt(0),
	})

	header := &types.Header{Number: big.NewInt(1)}
	block := types.NewBlockWithHeader(header)

	err := p.ProcessSetCodeTransactionBatch(nil, []*types.Transaction{tx}, nil, block)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}
