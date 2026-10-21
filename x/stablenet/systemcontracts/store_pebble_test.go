package systemcontracts

import (
	"context"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

func TestStore_GetActiveMinters(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage

	t.Run("empty minters list", func(t *testing.T) {
		minters, err := pebbleStorage.GetActiveMinters(ctx)
		if err != nil {
			t.Errorf("GetActiveMinters() error = %v", err)
		}
		if len(minters) != 0 {
			t.Errorf("GetActiveMinters() = %d minters, want 0", len(minters))
		}
	})

	t.Run("with stored minters", func(t *testing.T) {
		// Store some minter records
		minter1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
		minter2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

		key1 := MinterActiveIndexKey(minter1)
		key2 := MinterActiveIndexKey(minter2)

		err := pebbleStorage.Put(ctx, key1, storagepkg.EncodeBigInt(big.NewInt(1000000)))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		err = pebbleStorage.Put(ctx, key2, storagepkg.EncodeBigInt(big.NewInt(2000000)))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		minters, err := pebbleStorage.GetActiveMinters(ctx)
		if err != nil {
			t.Errorf("GetActiveMinters() error = %v", err)
		}
		if len(minters) != 2 {
			t.Errorf("GetActiveMinters() = %d minters, want 2", len(minters))
		}
	})
}

func TestStore_GetActiveMinters_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getactiveminters-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetActiveMinters(ctx)
	if err == nil {
		t.Error("GetActiveMinters() on closed storage should return error")
	}
}

func TestStore_GetMinterAllowance(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage
	minter := common.HexToAddress("0x3333333333333333333333333333333333333333")

	t.Run("non-existent minter returns zero", func(t *testing.T) {
		allowance, err := pebbleStorage.GetMinterAllowance(ctx, minter)
		if err != nil {
			t.Errorf("GetMinterAllowance() error = %v", err)
		}
		if allowance.Cmp(big.NewInt(0)) != 0 {
			t.Errorf("GetMinterAllowance() = %s, want 0", allowance.String())
		}
	})

	t.Run("existing minter returns allowance", func(t *testing.T) {
		expectedAllowance := big.NewInt(5000000)
		key := MinterActiveIndexKey(minter)
		err := pebbleStorage.Put(ctx, key, storagepkg.EncodeBigInt(expectedAllowance))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		allowance, err := pebbleStorage.GetMinterAllowance(ctx, minter)
		if err != nil {
			t.Errorf("GetMinterAllowance() error = %v", err)
		}
		if allowance.Cmp(expectedAllowance) != 0 {
			t.Errorf("GetMinterAllowance() = %s, want %s", allowance.String(), expectedAllowance.String())
		}
	})
}

func TestStore_GetMinterAllowance_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getminterallowance-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	minter := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetMinterAllowance(ctx, minter)
	if err == nil {
		t.Error("GetMinterAllowance() on closed storage should return error")
	}
}

func TestStore_GetMinterHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage
	minter := common.HexToAddress("0x4444444444444444444444444444444444444444")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetMinterHistory(ctx, minter)
		if err != nil {
			t.Errorf("GetMinterHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetMinterHistory() = %d events, want 0", len(history))
		}
	})
}

func TestStore_GetMinterHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getminterhistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	minter := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetMinterHistory(ctx, minter)
	if err == nil {
		t.Error("GetMinterHistory() on closed storage should return error")
	}
}

func TestStore_GetActiveValidators(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage

	t.Run("empty validators list", func(t *testing.T) {
		validators, err := pebbleStorage.GetActiveValidators(ctx)
		if err != nil {
			t.Errorf("GetActiveValidators() error = %v", err)
		}
		if len(validators) != 0 {
			t.Errorf("GetActiveValidators() = %d validators, want 0", len(validators))
		}
	})

	t.Run("with stored validators", func(t *testing.T) {
		validator1 := common.HexToAddress("0x5555555555555555555555555555555555555555")
		validator2 := common.HexToAddress("0x6666666666666666666666666666666666666666")

		key1 := ValidatorActiveIndexKey(validator1)
		key2 := ValidatorActiveIndexKey(validator2)

		err := pebbleStorage.Put(ctx, key1, []byte("active"))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		err = pebbleStorage.Put(ctx, key2, []byte("active"))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		validators, err := pebbleStorage.GetActiveValidators(ctx)
		if err != nil {
			t.Errorf("GetActiveValidators() error = %v", err)
		}
		if len(validators) != 2 {
			t.Errorf("GetActiveValidators() = %d validators, want 2", len(validators))
		}
	})
}

func TestStore_GetActiveValidators_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getactivevalidators-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetActiveValidators(ctx)
	if err == nil {
		t.Error("GetActiveValidators() on closed storage should return error")
	}
}

func TestStore_GetGasTipHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetGasTipHistory(ctx, 0, 100)
		if err != nil {
			t.Errorf("GetGasTipHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetGasTipHistory() = %d events, want 0", len(history))
		}
	})
}

func TestStore_GetGasTipHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getgastiphistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetGasTipHistory(ctx, 0, 100)
	if err == nil {
		t.Error("GetGasTipHistory() on closed storage should return error")
	}
}

func TestStore_GetValidatorHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage
	validator := common.HexToAddress("0x7777777777777777777777777777777777777777")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetValidatorHistory(ctx, validator)
		if err != nil {
			t.Errorf("GetValidatorHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetValidatorHistory() = %d events, want 0", len(history))
		}
	})
}

func TestStore_GetValidatorHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getvalidatorhistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	validator := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetValidatorHistory(ctx, validator)
	if err == nil {
		t.Error("GetValidatorHistory() on closed storage should return error")
	}
}

func TestStore_GetMinterConfigHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetMinterConfigHistory(ctx, 0, 100)
		if err != nil {
			t.Errorf("GetMinterConfigHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetMinterConfigHistory() = %d events, want 0", len(history))
		}
	})
}

func TestStore_GetMinterConfigHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getminterconfighistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetMinterConfigHistory(ctx, 0, 100)
	if err == nil {
		t.Error("GetMinterConfigHistory() on closed storage should return error")
	}
}

func TestStore_GetEmergencyPauseHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage
	contract := common.HexToAddress("0xdddddddddddddddddddddddddddddddddddddddd")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetEmergencyPauseHistory(ctx, contract)
		if err != nil {
			t.Errorf("GetEmergencyPauseHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetEmergencyPauseHistory() = %d events, want 0", len(history))
		}
	})
}

func TestStore_GetEmergencyPauseHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getemergencypausehistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetEmergencyPauseHistory(ctx, contract)
	if err == nil {
		t.Error("GetEmergencyPauseHistory() on closed storage should return error")
	}
}

func TestStore_GetDepositMintProposals(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage

	t.Run("empty proposals", func(t *testing.T) {
		proposals, err := pebbleStorage.GetDepositMintProposals(ctx, 0, 100, ProposalStatusAll)
		if err != nil {
			t.Errorf("GetDepositMintProposals() error = %v", err)
		}
		if len(proposals) != 0 {
			t.Errorf("GetDepositMintProposals() = %d proposals, want 0", len(proposals))
		}
	})
}

func TestStore_GetDepositMintProposals_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getdepositmintproposals-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetDepositMintProposals(ctx, 0, 100, ProposalStatusAll)
	if err == nil {
		t.Error("GetDepositMintProposals() on closed storage should return error")
	}
}

func TestStore_GetBurnHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage
	account := common.HexToAddress("0x8888888888888888888888888888888888888888")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetBurnHistory(ctx, 0, 100, account)
		if err != nil {
			t.Errorf("GetBurnHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetBurnHistory() = %d events, want 0", len(history))
		}
	})
}

func TestStore_GetBurnHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getburnhistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	account := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetBurnHistory(ctx, 0, 100, account)
	if err == nil {
		t.Error("GetBurnHistory() on closed storage should return error")
	}
}

func TestStore_GetBlacklistedAddresses(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage

	t.Run("empty blacklist", func(t *testing.T) {
		addresses, err := pebbleStorage.GetBlacklistedAddresses(ctx)
		if err != nil {
			t.Errorf("GetBlacklistedAddresses() error = %v", err)
		}
		if len(addresses) != 0 {
			t.Errorf("GetBlacklistedAddresses() = %d addresses, want 0", len(addresses))
		}
	})

	t.Run("with stored blacklisted addresses", func(t *testing.T) {
		addr1 := common.HexToAddress("0x9999999999999999999999999999999999999999")
		addr2 := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

		key1 := BlacklistActiveIndexKey(addr1)
		key2 := BlacklistActiveIndexKey(addr2)

		err := pebbleStorage.Put(ctx, key1, []byte("1"))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		err = pebbleStorage.Put(ctx, key2, []byte("1"))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		addresses, err := pebbleStorage.GetBlacklistedAddresses(ctx)
		if err != nil {
			t.Errorf("GetBlacklistedAddresses() error = %v", err)
		}
		if len(addresses) != 2 {
			t.Errorf("GetBlacklistedAddresses() = %d addresses, want 2", len(addresses))
		}
	})
}

func TestStore_GetBlacklistedAddresses_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getblacklistedaddresses-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetBlacklistedAddresses(ctx)
	if err == nil {
		t.Error("GetBlacklistedAddresses() on closed storage should return error")
	}
}

func TestStore_GetBlacklistHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage
	addr := common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetBlacklistHistory(ctx, addr)
		if err != nil {
			t.Errorf("GetBlacklistHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetBlacklistHistory() = %d events, want 0", len(history))
		}
	})
}

func TestStore_GetBlacklistHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getblacklisthistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetBlacklistHistory(ctx, addr)
	if err == nil {
		t.Error("GetBlacklistHistory() on closed storage should return error")
	}
}

func TestStore_GetAuthorizedAccounts(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage

	t.Run("empty authorized accounts", func(t *testing.T) {
		accounts, err := pebbleStorage.GetAuthorizedAccounts(ctx)
		if err != nil {
			t.Errorf("GetAuthorizedAccounts() error = %v", err)
		}
		if len(accounts) != 0 {
			t.Errorf("GetAuthorizedAccounts() = %d accounts, want 0", len(accounts))
		}
	})
}

func TestStore_GetAuthorizedAccounts_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getauthorizedaccounts-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetAuthorizedAccounts(ctx)
	if err == nil {
		t.Error("GetAuthorizedAccounts() on closed storage should return error")
	}
}

func TestStore_GetProposals(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage
	contract := common.HexToAddress("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")

	t.Run("empty proposals", func(t *testing.T) {
		proposals, err := pebbleStorage.GetProposals(ctx, contract, ProposalStatusAll, 100, 0)
		if err != nil {
			t.Errorf("GetProposals() error = %v", err)
		}
		if len(proposals) != 0 {
			t.Errorf("GetProposals() = %d proposals, want 0", len(proposals))
		}
	})
}

func TestStore_GetProposals_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getproposals-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetProposals(ctx, contract, ProposalStatusAll, 100, 0)
	if err == nil {
		t.Error("GetProposals() on closed storage should return error")
	}
}

func TestStore_GetProposalById(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage
	contract := common.HexToAddress("0xffffffffffffffffffffffffffffffffffffffff")

	t.Run("non-existent proposal", func(t *testing.T) {
		proposal, err := pebbleStorage.GetProposalById(ctx, contract, big.NewInt(999))
		if err != nil {
			t.Errorf("GetProposalById() error = %v", err)
		}
		if proposal != nil {
			t.Error("GetProposalById() should return nil for non-existent proposal")
		}
	})
}

func TestStore_GetProposalById_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getproposalbyid-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetProposalById(ctx, contract, big.NewInt(1))
	if err == nil {
		t.Error("GetProposalById() on closed storage should return error")
	}
}

func TestStore_GetProposalVotes(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage
	contract := common.HexToAddress("0x0000000000000000000000000000000000000001")

	t.Run("empty votes", func(t *testing.T) {
		votes, err := pebbleStorage.GetProposalVotes(ctx, contract, big.NewInt(1))
		if err != nil {
			t.Errorf("GetProposalVotes() error = %v", err)
		}
		if len(votes) != 0 {
			t.Errorf("GetProposalVotes() = %d votes, want 0", len(votes))
		}
	})
}

func TestStore_GetProposalVotes_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getproposalvotes-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetProposalVotes(ctx, contract, big.NewInt(1))
	if err == nil {
		t.Error("GetProposalVotes() on closed storage should return error")
	}
}

func TestStore_GetMemberHistory(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage
	member := common.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")

	t.Run("empty history", func(t *testing.T) {
		history, err := pebbleStorage.GetMemberHistory(ctx, member)
		if err != nil {
			t.Errorf("GetMemberHistory() error = %v", err)
		}
		if len(history) != 0 {
			t.Errorf("GetMemberHistory() = %d events, want 0", len(history))
		}
	})
}

func TestStore_GetMemberHistory_ClosedStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-getmemberhistory-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	member := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = pebbleStorage.GetMemberHistory(ctx, member)
	if err == nil {
		t.Error("GetMemberHistory() on closed storage should return error")
	}
}

func TestStore_GetTotalSupply_Basic(t *testing.T) {
	storage, cleanup := setupTestStorage(t)
	defer cleanup()
	ctx := context.Background()

	pebbleStorage := storage

	t.Run("get total supply", func(t *testing.T) {
		supply, err := pebbleStorage.GetTotalSupply(ctx)
		if err != nil {
			t.Errorf("GetTotalSupply() error = %v", err)
		}
		// Should return zero for non-existent token
		if supply == nil {
			t.Error("GetTotalSupply() should return non-nil value")
		}
	})
}

func TestStore_GetTotalSupply_ClosedStorage_Basic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pebble-gettotalsupply-closed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := storagepkg.DefaultConfig(tmpDir)
	pebbleStorage, err := newTestDB(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	_ = pebbleStorage.Close()

	ctx := context.Background()
	_, err = pebbleStorage.GetTotalSupply(ctx)
	if err == nil {
		t.Error("GetTotalSupply() on closed storage should return error")
	}
}
