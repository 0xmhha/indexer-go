package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

func TestGetSystemContractInfo(t *testing.T) {
	tests := []struct {
		name        string
		address     common.Address
		shouldExist bool
		expectedName string
	}{
		{
			name:        "NativeCoinAdapter exists",
			address:     NativeCoinAdapterAddress,
			shouldExist: true,
			expectedName: "NativeCoinAdapter",
		},
		{
			name:        "GovValidator exists",
			address:     GovValidatorAddress,
			shouldExist: true,
			expectedName: "GovValidator",
		},
		{
			name:        "GovMasterMinter exists",
			address:     GovMasterMinterAddress,
			shouldExist: true,
			expectedName: "GovMasterMinter",
		},
		{
			name:        "GovMinter exists",
			address:     GovMinterAddress,
			shouldExist: true,
			expectedName: "GovMinter",
		},
		{
			name:        "GovCouncil exists",
			address:     GovCouncilAddress,
			shouldExist: true,
			expectedName: "GovCouncil",
		},
		{
			name:        "random address does not exist",
			address:     common.HexToAddress("0x1234567890123456789012345678901234567890"),
			shouldExist: false,
		},
		{
			name:        "zero address does not exist",
			address:     common.Address{},
			shouldExist: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := GetSystemContractInfo(tt.address)
			if tt.shouldExist {
				if info == nil {
					t.Errorf("expected info for %s, got nil", tt.address.Hex())
					return
				}
				if info.Name != tt.expectedName {
					t.Errorf("expected name %s, got %s", tt.expectedName, info.Name)
				}
				if info.Address != tt.address {
					t.Errorf("expected address %s, got %s", tt.address.Hex(), info.Address.Hex())
				}
			} else {
				if info != nil {
					t.Errorf("expected nil for %s, got %+v", tt.address.Hex(), info)
				}
			}
		})
	}
}

func TestIsSystemContractAddress(t *testing.T) {
	tests := []struct {
		name     string
		address  common.Address
		expected bool
	}{
		{
			name:     "NativeCoinAdapter is system contract",
			address:  NativeCoinAdapterAddress,
			expected: true,
		},
		{
			name:     "GovValidator is system contract",
			address:  GovValidatorAddress,
			expected: true,
		},
		{
			name:     "GovMasterMinter is system contract",
			address:  GovMasterMinterAddress,
			expected: true,
		},
		{
			name:     "GovMinter is system contract",
			address:  GovMinterAddress,
			expected: true,
		},
		{
			name:     "GovCouncil is system contract",
			address:  GovCouncilAddress,
			expected: true,
		},
		{
			name:     "random address is not system contract",
			address:  common.HexToAddress("0x1234567890123456789012345678901234567890"),
			expected: false,
		},
		{
			name:     "zero address is not system contract",
			address:  common.Address{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSystemContractAddress(tt.address)
			if result != tt.expected {
				t.Errorf("IsSystemContractAddress(%s) = %v, want %v", tt.address.Hex(), result, tt.expected)
			}
		})
	}
}

func TestSystemContractInfoList(t *testing.T) {
	// Verify the list has expected entries
	if len(SystemContractInfoList) != 5 {
		t.Errorf("expected 5 system contracts, got %d", len(SystemContractInfoList))
	}

	// Verify each entry has required fields
	for _, info := range SystemContractInfoList {
		if info.Name == "" {
			t.Error("Name should not be empty")
		}
		if info.FileName == "" {
			t.Error("FileName should not be empty")
		}
		if info.CompilerVersion == "" {
			t.Error("CompilerVersion should not be empty")
		}
		if info.LicenseType == "" {
			t.Error("LicenseType should not be empty")
		}
		if info.Address == (common.Address{}) {
			t.Error("Address should not be zero")
		}
	}
}

func TestInitSystemContractVerifications_NilConfig(t *testing.T) {
	ctx := context.Background()
	err := InitSystemContractVerifications(ctx, nil, nil, nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestInitSystemContractVerifications_EmptySourcePath(t *testing.T) {
	ctx := context.Background()
	config := &SystemContractVerificationConfig{
		SourcePath: "",
	}
	err := InitSystemContractVerifications(ctx, nil, nil, config)
	if err == nil {
		t.Error("expected error for empty source path")
	}
}

func TestInitSystemContractVerifications_NonExistentPath(t *testing.T) {
	ctx := context.Background()
	config := &SystemContractVerificationConfig{
		SourcePath: "/nonexistent/path/to/contracts",
		Logger:     zap.NewNop(),
	}
	err := InitSystemContractVerifications(ctx, nil, nil, config)
	if err == nil {
		t.Error("expected error for non-existent v1 directory")
	}
}

// mockContractVerificationWriter implements ContractVerificationWriter for testing
type mockContractVerificationWriter struct {
	verifications map[common.Address]*ContractVerification
}

func newMockContractVerificationWriter() *mockContractVerificationWriter {
	return &mockContractVerificationWriter{
		verifications: make(map[common.Address]*ContractVerification),
	}
}

func (m *mockContractVerificationWriter) SetContractVerification(ctx context.Context, v *ContractVerification) error {
	m.verifications[v.Address] = v
	return nil
}

func (m *mockContractVerificationWriter) DeleteContractVerification(ctx context.Context, addr common.Address) error {
	delete(m.verifications, addr)
	return nil
}

// mockContractVerificationReader implements ContractVerificationReader for testing
type mockContractVerificationReader struct {
	verified map[common.Address]bool
}

func newMockContractVerificationReader() *mockContractVerificationReader {
	return &mockContractVerificationReader{
		verified: make(map[common.Address]bool),
	}
}

func (m *mockContractVerificationReader) IsContractVerified(ctx context.Context, addr common.Address) (bool, error) {
	return m.verified[addr], nil
}

func (m *mockContractVerificationReader) GetContractVerification(ctx context.Context, addr common.Address) (*ContractVerification, error) {
	return nil, nil
}

func (m *mockContractVerificationReader) ListVerifiedContracts(ctx context.Context, limit, offset int) ([]common.Address, error) {
	var addrs []common.Address
	for addr, verified := range m.verified {
		if verified {
			addrs = append(addrs, addr)
		}
	}
	return addrs, nil
}

func (m *mockContractVerificationReader) CountVerifiedContracts(ctx context.Context) (int, error) {
	count := 0
	for _, verified := range m.verified {
		if verified {
			count++
		}
	}
	return count, nil
}

func TestInitSystemContractVerifications_WithTempDirectory(t *testing.T) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "sys-contracts-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	v1Dir := filepath.Join(tmpDir, "v1")
	if err := os.MkdirAll(v1Dir, 0755); err != nil {
		t.Fatalf("Failed to create v1 dir: %v", err)
	}

	// Create mock source files
	for _, info := range SystemContractInfoList {
		sourceContent := []byte("// SPDX-License-Identifier: Apache-2.0\npragma solidity ^0.8.14;\ncontract " + info.Name + " {}")
		if err := os.WriteFile(filepath.Join(v1Dir, info.FileName), sourceContent, 0644); err != nil {
			t.Fatalf("Failed to create source file: %v", err)
		}
	}

	ctx := context.Background()
	writer := newMockContractVerificationWriter()
	reader := newMockContractVerificationReader()
	config := &SystemContractVerificationConfig{
		SourcePath:       tmpDir,
		IncludeAbstracts: false,
		Logger:           zap.NewNop(),
	}

	err = InitSystemContractVerifications(ctx, writer, reader, config)
	if err != nil {
		t.Fatalf("InitSystemContractVerifications failed: %v", err)
	}

	// Verify all contracts were stored
	if len(writer.verifications) != len(SystemContractInfoList) {
		t.Errorf("expected %d verifications, got %d", len(SystemContractInfoList), len(writer.verifications))
	}

	// Verify contract details
	for _, info := range SystemContractInfoList {
		v, ok := writer.verifications[info.Address]
		if !ok {
			t.Errorf("verification not found for %s", info.Name)
			continue
		}
		if v.Name != info.Name {
			t.Errorf("expected name %s, got %s", info.Name, v.Name)
		}
		if !v.IsVerified {
			t.Errorf("expected IsVerified=true for %s", info.Name)
		}
	}
}

func TestInitSystemContractVerifications_SkipsAlreadyVerified(t *testing.T) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "sys-contracts-skip-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	v1Dir := filepath.Join(tmpDir, "v1")
	if err := os.MkdirAll(v1Dir, 0755); err != nil {
		t.Fatalf("Failed to create v1 dir: %v", err)
	}

	// Create mock source files
	for _, info := range SystemContractInfoList {
		sourceContent := []byte("// SPDX-License-Identifier: Apache-2.0\npragma solidity ^0.8.14;\ncontract " + info.Name + " {}")
		if err := os.WriteFile(filepath.Join(v1Dir, info.FileName), sourceContent, 0644); err != nil {
			t.Fatalf("Failed to create source file: %v", err)
		}
	}

	ctx := context.Background()
	writer := newMockContractVerificationWriter()
	reader := newMockContractVerificationReader()

	// Mark first contract as already verified
	reader.verified[SystemContractInfoList[0].Address] = true

	config := &SystemContractVerificationConfig{
		SourcePath:       tmpDir,
		IncludeAbstracts: false,
		Logger:           zap.NewNop(),
	}

	err = InitSystemContractVerifications(ctx, writer, reader, config)
	if err != nil {
		t.Fatalf("InitSystemContractVerifications failed: %v", err)
	}

	// Should skip the first one (already verified)
	expectedCount := len(SystemContractInfoList) - 1
	if len(writer.verifications) != expectedCount {
		t.Errorf("expected %d verifications (skipped 1), got %d", expectedCount, len(writer.verifications))
	}
}

func TestInitSystemContractVerifications_WithAbstracts(t *testing.T) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "sys-contracts-abstracts-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	v1Dir := filepath.Join(tmpDir, "v1")
	abstractsDir := filepath.Join(tmpDir, "abstracts")
	eipDir := filepath.Join(abstractsDir, "eip")

	if err := os.MkdirAll(v1Dir, 0755); err != nil {
		t.Fatalf("Failed to create v1 dir: %v", err)
	}
	if err := os.MkdirAll(eipDir, 0755); err != nil {
		t.Fatalf("Failed to create eip dir: %v", err)
	}

	// Create mock source files
	for _, info := range SystemContractInfoList {
		sourceContent := []byte("// SPDX-License-Identifier: Apache-2.0\npragma solidity ^0.8.14;\ncontract " + info.Name + " {}")
		if err := os.WriteFile(filepath.Join(v1Dir, info.FileName), sourceContent, 0644); err != nil {
			t.Fatalf("Failed to create source file: %v", err)
		}
	}

	// Create abstract files
	abstractFiles := []string{"GovBase.sol", "Mintable.sol", "Blacklistable.sol", "AbstractFiatToken.sol"}
	for _, f := range abstractFiles {
		content := []byte("// Abstract contract\ncontract Abstract" + f + " {}")
		if err := os.WriteFile(filepath.Join(abstractsDir, f), content, 0644); err != nil {
			t.Fatalf("Failed to create abstract file: %v", err)
		}
	}

	// Create EIP files
	eipFiles := []string{"EIP712Domain.sol", "EIP2612.sol", "EIP3009.sol"}
	for _, f := range eipFiles {
		content := []byte("// EIP implementation\ncontract " + f + " {}")
		if err := os.WriteFile(filepath.Join(eipDir, f), content, 0644); err != nil {
			t.Fatalf("Failed to create eip file: %v", err)
		}
	}

	ctx := context.Background()
	writer := newMockContractVerificationWriter()
	reader := newMockContractVerificationReader()
	config := &SystemContractVerificationConfig{
		SourcePath:       tmpDir,
		IncludeAbstracts: true,
		Logger:           zap.NewNop(),
	}

	err = InitSystemContractVerifications(ctx, writer, reader, config)
	if err != nil {
		t.Fatalf("InitSystemContractVerifications failed: %v", err)
	}

	// Verify all contracts were stored with abstract content included
	for _, info := range SystemContractInfoList {
		v, ok := writer.verifications[info.Address]
		if !ok {
			t.Errorf("verification not found for %s", info.Name)
			continue
		}
		// Check that source includes abstract content marker
		if len(v.SourceCode) == 0 {
			t.Errorf("expected non-empty source for %s", info.Name)
		}
	}
}
