package storage

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestPebbleStorage_SetGetContractVerification(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	contractAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	verification := &ContractVerification{
		Address:              contractAddr,
		IsVerified:           true,
		Name:                 "TestContract",
		CompilerVersion:      "v0.8.19+commit.7dd6d404",
		OptimizationEnabled:  true,
		OptimizationRuns:     200,
		SourceCode:           "pragma solidity ^0.8.0; contract Test {}",
		ABI:                  `[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"}]`,
		ConstructorArguments: "0x",
		VerifiedAt:           time.Unix(1234567890, 0),
	}

	// Set verification
	err := storage.SetContractVerification(ctx, verification)
	if err != nil {
		t.Fatalf("SetContractVerification() error = %v", err)
	}

	// Get verification
	retrieved, err := storage.GetContractVerification(ctx, contractAddr)
	if err != nil {
		t.Fatalf("GetContractVerification() error = %v", err)
	}

	// Verify fields
	if retrieved.Address != verification.Address {
		t.Errorf("Address = %v, want %v", retrieved.Address, verification.Address)
	}
	if retrieved.Name != verification.Name {
		t.Errorf("Name = %v, want %v", retrieved.Name, verification.Name)
	}
	if retrieved.CompilerVersion != verification.CompilerVersion {
		t.Errorf("CompilerVersion = %v, want %v", retrieved.CompilerVersion, verification.CompilerVersion)
	}
	if retrieved.OptimizationEnabled != verification.OptimizationEnabled {
		t.Errorf("OptimizationEnabled = %v, want %v", retrieved.OptimizationEnabled, verification.OptimizationEnabled)
	}
}

func TestPebbleStorage_GetContractVerification_NotFound(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	nonExistentAddr := common.HexToAddress("0x9999999999999999999999999999999999999999")

	_, err := storage.GetContractVerification(ctx, nonExistentAddr)
	if err != ErrNotFound {
		t.Errorf("GetContractVerification() error = %v, want ErrNotFound", err)
	}
}

func TestPebbleStorage_IsContractVerified(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	contractAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// Initially not verified
	isVerified, err := storage.IsContractVerified(ctx, contractAddr)
	if err != nil {
		t.Fatalf("IsContractVerified() error = %v", err)
	}
	if isVerified {
		t.Error("IsContractVerified() = true, want false")
	}

	// Set verification
	verification := &ContractVerification{
		Address:         contractAddr,
		IsVerified:      true,
		Name:            "TestContract",
		CompilerVersion: "v0.8.19",
		SourceCode:      "contract Test {}",
		VerifiedAt:      time.Unix(1234567890, 0),
	}
	err = storage.SetContractVerification(ctx, verification)
	if err != nil {
		t.Fatalf("SetContractVerification() error = %v", err)
	}

	// Now should be verified
	isVerified, err = storage.IsContractVerified(ctx, contractAddr)
	if err != nil {
		t.Fatalf("IsContractVerified() error = %v", err)
	}
	if !isVerified {
		t.Error("IsContractVerified() = false, want true")
	}
}

func TestPebbleStorage_ListVerifiedContracts(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Add multiple verified contracts
	contracts := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	for i, addr := range contracts {
		verification := &ContractVerification{
			Address:         addr,
			IsVerified:      true,
			Name:            "TestContract",
			CompilerVersion: "v0.8.19",
			SourceCode:      "contract Test {}",
			VerifiedAt:      time.Unix(int64(1234567890+i), 0),
		}
		err := storage.SetContractVerification(ctx, verification)
		if err != nil {
			t.Fatalf("SetContractVerification() error = %v", err)
		}
	}

	// List all contracts
	list, err := storage.ListVerifiedContracts(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListVerifiedContracts() error = %v", err)
	}

	if len(list) != 3 {
		t.Errorf("ListVerifiedContracts() returned %d contracts, want 3", len(list))
	}

	// Test pagination
	list, err = storage.ListVerifiedContracts(ctx, 2, 0)
	if err != nil {
		t.Fatalf("ListVerifiedContracts() with limit error = %v", err)
	}

	if len(list) != 2 {
		t.Errorf("ListVerifiedContracts(limit=2) returned %d contracts, want 2", len(list))
	}

	// Test offset
	list, err = storage.ListVerifiedContracts(ctx, 10, 2)
	if err != nil {
		t.Fatalf("ListVerifiedContracts() with offset error = %v", err)
	}

	if len(list) != 1 {
		t.Errorf("ListVerifiedContracts(offset=2) returned %d contracts, want 1", len(list))
	}
}

func TestPebbleStorage_ListVerifiedContracts_Empty(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	list, err := storage.ListVerifiedContracts(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListVerifiedContracts() error = %v", err)
	}

	if len(list) != 0 {
		t.Errorf("ListVerifiedContracts() returned %d contracts, want 0", len(list))
	}
}

func TestPebbleStorage_CountVerifiedContracts(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	// Initially 0
	count, err := storage.CountVerifiedContracts(ctx)
	if err != nil {
		t.Fatalf("CountVerifiedContracts() error = %v", err)
	}
	if count != 0 {
		t.Errorf("CountVerifiedContracts() = %d, want 0", count)
	}

	// Add contracts
	contracts := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
	}

	for _, addr := range contracts {
		verification := &ContractVerification{
			Address:         addr,
			IsVerified:      true,
			Name:            "TestContract",
			CompilerVersion: "v0.8.19",
			SourceCode:      "contract Test {}",
			VerifiedAt:      time.Unix(1234567890, 0),
		}
		err := storage.SetContractVerification(ctx, verification)
		if err != nil {
			t.Fatalf("SetContractVerification() error = %v", err)
		}
	}

	// Count should be 3
	count, err = storage.CountVerifiedContracts(ctx)
	if err != nil {
		t.Fatalf("CountVerifiedContracts() error = %v", err)
	}
	if count != 3 {
		t.Errorf("CountVerifiedContracts() = %d, want 3", count)
	}
}

func TestPebbleStorage_DeleteContractVerification(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()

	contractAddr := common.HexToAddress("0x4444444444444444444444444444444444444444")

	// Set verification
	verification := &ContractVerification{
		Address:         contractAddr,
		IsVerified:      true,
		Name:            "TestContract",
		CompilerVersion: "v0.8.19",
		SourceCode:      "contract Test {}",
		VerifiedAt:      time.Unix(1234567890, 0),
	}
	err := storage.SetContractVerification(ctx, verification)
	if err != nil {
		t.Fatalf("SetContractVerification() error = %v", err)
	}

	// Verify it exists
	isVerified, err := storage.IsContractVerified(ctx, contractAddr)
	if err != nil {
		t.Fatalf("IsContractVerified() error = %v", err)
	}
	if !isVerified {
		t.Error("Contract should be verified before deletion")
	}

	// Delete verification
	err = storage.DeleteContractVerification(ctx, contractAddr)
	if err != nil {
		t.Fatalf("DeleteContractVerification() error = %v", err)
	}

	// Verify it's deleted
	isVerified, err = storage.IsContractVerified(ctx, contractAddr)
	if err != nil {
		t.Fatalf("IsContractVerified() error = %v", err)
	}
	if isVerified {
		t.Error("Contract should not be verified after deletion")
	}

	// Getting should return ErrNotFound
	_, err = storage.GetContractVerification(ctx, contractAddr)
	if err != ErrNotFound {
		t.Errorf("GetContractVerification() error = %v, want ErrNotFound", err)
	}
}

func TestPebbleStorage_ContractVerification_ClosedStorage(t *testing.T) {
	s, cleanup := setupTestStorage(t)
	defer cleanup()

	storage := s.(*PebbleStorage)
	ctx := context.Background()
	addr := common.HexToAddress("0x5555555555555555555555555555555555555555")

	// Close storage
	storage.Close()

	// All operations should return ErrClosed
	_, err := storage.GetContractVerification(ctx, addr)
	if err != ErrClosed {
		t.Errorf("GetContractVerification() on closed storage error = %v, want ErrClosed", err)
	}

	_, err = storage.IsContractVerified(ctx, addr)
	if err != ErrClosed {
		t.Errorf("IsContractVerified() on closed storage error = %v, want ErrClosed", err)
	}

	_, err = storage.ListVerifiedContracts(ctx, 10, 0)
	if err != ErrClosed {
		t.Errorf("ListVerifiedContracts() on closed storage error = %v, want ErrClosed", err)
	}

	_, err = storage.CountVerifiedContracts(ctx)
	if err != ErrClosed {
		t.Errorf("CountVerifiedContracts() on closed storage error = %v, want ErrClosed", err)
	}

	err = storage.SetContractVerification(ctx, &ContractVerification{Address: addr})
	if err != ErrClosed {
		t.Errorf("SetContractVerification() on closed storage error = %v, want ErrClosed", err)
	}

	err = storage.DeleteContractVerification(ctx, addr)
	if err != ErrClosed {
		t.Errorf("DeleteContractVerification() on closed storage error = %v, want ErrClosed", err)
	}
}
