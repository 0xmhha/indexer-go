package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// SystemContractInfo contains metadata for a system contract
type SystemContractInfo struct {
	Address         common.Address
	Name            string
	FileName        string
	CompilerVersion string
	LicenseType     string
}

// SystemContractInfoList contains all system contract metadata
var SystemContractInfoList = []SystemContractInfo{
	{
		Address:         NativeCoinAdapterAddress,
		Name:            "NativeCoinAdapter",
		FileName:        "NativeCoinAdapter.sol",
		CompilerVersion: "0.8.14",
		LicenseType:     "Apache-2.0",
	},
	{
		Address:         GovValidatorAddress,
		Name:            "GovValidator",
		FileName:        "GovValidator.sol",
		CompilerVersion: "0.8.14",
		LicenseType:     "GPL-3.0-or-later",
	},
	{
		Address:         GovMasterMinterAddress,
		Name:            "GovMasterMinter",
		FileName:        "GovMasterMinter.sol",
		CompilerVersion: "0.8.14",
		LicenseType:     "GPL-3.0-or-later",
	},
	{
		Address:         GovMinterAddress,
		Name:            "GovMinter",
		FileName:        "GovMinter.sol",
		CompilerVersion: "0.8.14",
		LicenseType:     "GPL-3.0-or-later",
	},
	{
		Address:         GovCouncilAddress,
		Name:            "GovCouncil",
		FileName:        "GovCouncil.sol",
		CompilerVersion: "0.8.14",
		LicenseType:     "GPL-3.0-or-later",
	},
}

// SystemContractVerificationConfig contains configuration for system contract verification initialization
type SystemContractVerificationConfig struct {
	// SourcePath is the path to the directory containing v1/*.sol files
	// e.g., "/path/to/go-stablenet/systemcontracts/solidity"
	SourcePath string

	// IncludeAbstracts determines whether to include abstract contracts in the source code
	// If true, GovBase.sol and other abstracts will be included
	IncludeAbstracts bool

	// Logger for logging initialization progress
	Logger *zap.Logger
}

// InitSystemContractVerifications initializes verification data for all system contracts
// by reading source code from the specified path and storing it in the database
func InitSystemContractVerifications(ctx context.Context, storage ContractVerificationWriter, reader ContractVerificationReader, config *SystemContractVerificationConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	if config.SourcePath == "" {
		return fmt.Errorf("source path cannot be empty")
	}

	v1Path := filepath.Join(config.SourcePath, "v1")

	// Check if v1 directory exists
	if _, err := os.Stat(v1Path); os.IsNotExist(err) {
		return fmt.Errorf("v1 directory not found at %s", v1Path)
	}

	logger := config.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	logger.Info("Initializing system contract verifications",
		zap.String("source_path", config.SourcePath),
		zap.Int("contract_count", len(SystemContractInfoList)),
	)

	// Load abstract contracts if needed
	var abstractsContent string
	if config.IncludeAbstracts {
		abstractsPath := filepath.Join(config.SourcePath, "abstracts")
		abstracts, err := loadAbstractContracts(abstractsPath)
		if err != nil {
			logger.Warn("Failed to load abstract contracts", zap.Error(err))
		} else {
			abstractsContent = abstracts
		}
	}

	successCount := 0
	skipCount := 0

	for _, info := range SystemContractInfoList {
		// Check if already verified
		isVerified, err := reader.IsContractVerified(ctx, info.Address)
		if err == nil && isVerified {
			logger.Debug("System contract already verified, skipping",
				zap.String("name", info.Name),
				zap.String("address", info.Address.Hex()),
			)
			skipCount++
			continue
		}

		// Read source code
		sourceFile := filepath.Join(v1Path, info.FileName)
		sourceCode, err := os.ReadFile(sourceFile)
		if err != nil {
			logger.Error("Failed to read source code",
				zap.String("name", info.Name),
				zap.String("file", sourceFile),
				zap.Error(err),
			)
			continue
		}

		// Combine with abstracts if available
		fullSource := string(sourceCode)
		if abstractsContent != "" {
			fullSource = fmt.Sprintf("// === Abstract Contracts ===\n%s\n\n// === Main Contract: %s ===\n%s",
				abstractsContent, info.Name, sourceCode)
		}

		// Create verification entry
		verification := &ContractVerification{
			Address:             info.Address,
			IsVerified:          true,
			Name:                info.Name,
			CompilerVersion:     info.CompilerVersion,
			OptimizationEnabled: true,
			OptimizationRuns:    200,
			SourceCode:          fullSource,
			ABI:                 "", // ABI will be empty for now - can be added later
			VerifiedAt:          time.Now(),
			LicenseType:         info.LicenseType,
		}

		if err := storage.SetContractVerification(ctx, verification); err != nil {
			logger.Error("Failed to store verification",
				zap.String("name", info.Name),
				zap.String("address", info.Address.Hex()),
				zap.Error(err),
			)
			continue
		}

		logger.Info("System contract verification initialized",
			zap.String("name", info.Name),
			zap.String("address", info.Address.Hex()),
			zap.Int("source_size", len(fullSource)),
		)
		successCount++
	}

	logger.Info("System contract verification initialization complete",
		zap.Int("success", successCount),
		zap.Int("skipped", skipCount),
		zap.Int("total", len(SystemContractInfoList)),
	)

	return nil
}

// loadAbstractContracts loads all abstract contract files and combines them
func loadAbstractContracts(abstractsPath string) (string, error) {
	abstractFiles := []string{
		"GovBase.sol",
		"Mintable.sol",
		"Blacklistable.sol",
		"AbstractFiatToken.sol",
	}

	var combined string
	for _, fileName := range abstractFiles {
		filePath := filepath.Join(abstractsPath, fileName)
		content, err := os.ReadFile(filePath)
		if err != nil {
			// Skip missing files
			continue
		}
		combined += fmt.Sprintf("// --- %s ---\n%s\n\n", fileName, string(content))
	}

	// Also try to load EIP abstracts
	eipPath := filepath.Join(abstractsPath, "eip")
	eipFiles := []string{"EIP712Domain.sol", "EIP2612.sol", "EIP3009.sol"}
	for _, fileName := range eipFiles {
		filePath := filepath.Join(eipPath, fileName)
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		combined += fmt.Sprintf("// --- eip/%s ---\n%s\n\n", fileName, string(content))
	}

	return combined, nil
}

// GetSystemContractInfo returns the system contract info for a given address
func GetSystemContractInfo(address common.Address) *SystemContractInfo {
	for _, info := range SystemContractInfoList {
		if info.Address == address {
			return &info
		}
	}
	return nil
}

// IsSystemContractAddress returns true if the address is a system contract
func IsSystemContractAddress(address common.Address) bool {
	return GetSystemContractInfo(address) != nil
}
