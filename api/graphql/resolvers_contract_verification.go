package graphql

import (
	"fmt"
	"time"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/0xmhha/indexer-go/verifier"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveContractVerification handles the contractVerification query
func (s *Schema) resolveContractVerification(params graphql.ResolveParams) (interface{}, error) {
	ctx := params.Context

	// Extract address parameter
	addressStr, ok := params.Args["address"].(string)
	if !ok || addressStr == "" {
		return nil, fmt.Errorf("address is required")
	}

	// Validate address
	if !common.IsHexAddress(addressStr) {
		return nil, fmt.Errorf("invalid address format")
	}

	address := common.HexToAddress(addressStr)

	// Cast storage to ContractVerificationReader
	verificationReader, ok := s.storage.(storage.ContractVerificationReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support contract verification queries")
	}

	// Get contract verification data
	verification, err := verificationReader.GetContractVerification(ctx, address)
	if err != nil {
		if err == storage.ErrNotFound {
			// Return unverified contract result
			return map[string]interface{}{
				"address":    address.Hex(),
				"isVerified": false,
			}, nil
		}
		s.logger.Error("failed to get contract verification",
			zap.String("address", address.Hex()),
			zap.Error(err))
		return nil, err
	}

	// Convert to GraphQL format
	result := map[string]interface{}{
		"address":              verification.Address.Hex(),
		"isVerified":           verification.IsVerified,
		"name":                 verification.Name,
		"compilerVersion":      verification.CompilerVersion,
		"optimizationEnabled":  verification.OptimizationEnabled,
		"optimizationRuns":     verification.OptimizationRuns,
		"sourceCode":           verification.SourceCode,
		"abi":                  verification.ABI,
		"constructorArguments": verification.ConstructorArguments,
		"verifiedAt":           verification.VerifiedAt.Format(time.RFC3339),
		"licenseType":          verification.LicenseType,
	}

	return result, nil
}

// resolveVerifyContract handles the verifyContract mutation
func (s *Schema) resolveVerifyContract(params graphql.ResolveParams) (interface{}, error) {
	ctx := params.Context

	// Extract parameters
	addressStr, ok := params.Args["address"].(string)
	if !ok || addressStr == "" {
		return nil, fmt.Errorf("address is required")
	}

	sourceCode, ok := params.Args["sourceCode"].(string)
	if !ok || sourceCode == "" {
		return nil, fmt.Errorf("sourceCode is required")
	}

	compilerVersion, ok := params.Args["compilerVersion"].(string)
	if !ok || compilerVersion == "" {
		return nil, fmt.Errorf("compilerVersion is required")
	}

	optimizationEnabled, ok := params.Args["optimizationEnabled"].(bool)
	if !ok {
		optimizationEnabled = false
	}

	// Optional parameters
	optimizationRuns := 200 // Default value
	if runs, ok := params.Args["optimizationRuns"].(int); ok && runs > 0 {
		optimizationRuns = runs
	}

	constructorArguments := ""
	if args, ok := params.Args["constructorArguments"].(string); ok {
		constructorArguments = args
	}

	contractName := ""
	if name, ok := params.Args["contractName"].(string); ok {
		contractName = name
	}

	licenseType := ""
	if license, ok := params.Args["licenseType"].(string); ok {
		licenseType = license
	}

	// Validate address
	if !common.IsHexAddress(addressStr) {
		return nil, fmt.Errorf("invalid address format")
	}

	address := common.HexToAddress(addressStr)

	// Cast storage to ContractVerificationWriter
	verificationWriter, ok := s.storage.(storage.ContractVerificationWriter)
	if !ok {
		return nil, fmt.Errorf("storage does not support contract verification writes")
	}

	// Check if verifier is available
	if s.verifier == nil {
		return nil, fmt.Errorf("contract verifier is not configured")
	}

	// Create verification request
	req := &verifier.VerificationRequest{
		Address:              address,
		SourceCode:           sourceCode,
		CompilerVersion:      compilerVersion,
		ContractName:         contractName,
		OptimizationEnabled:  optimizationEnabled,
		OptimizationRuns:     optimizationRuns,
		ConstructorArguments: constructorArguments,
		LicenseType:          licenseType,
	}

	// Verify contract
	result, err := s.verifier.Verify(ctx, req)
	if err != nil {
		s.logger.Error("contract verification failed",
			zap.String("address", address.Hex()),
			zap.Error(err))
		return nil, fmt.Errorf("verification failed: %w", err)
	}

	if !result.Success {
		s.logger.Warn("contract verification unsuccessful",
			zap.String("address", address.Hex()),
			zap.Error(result.Error))
		return nil, fmt.Errorf("verification failed: %w", result.Error)
	}

	// Store verification data
	verification := &storage.ContractVerification{
		Address:              address,
		IsVerified:           true,
		Name:                 contractName,
		CompilerVersion:      compilerVersion,
		OptimizationEnabled:  optimizationEnabled,
		OptimizationRuns:     optimizationRuns,
		SourceCode:           sourceCode,
		ABI:                  result.ABI,
		ConstructorArguments: constructorArguments,
		VerifiedAt:           time.Now(),
		LicenseType:          licenseType,
	}

	if err := verificationWriter.SetContractVerification(ctx, verification); err != nil {
		s.logger.Error("failed to store contract verification",
			zap.String("address", address.Hex()),
			zap.Error(err))
		return nil, fmt.Errorf("failed to store verification: %w", err)
	}

	s.logger.Info("contract verified successfully",
		zap.String("address", address.Hex()),
		zap.String("compiler", compilerVersion))

	// Return verification result
	return map[string]interface{}{
		"address":              verification.Address.Hex(),
		"isVerified":           verification.IsVerified,
		"name":                 verification.Name,
		"compilerVersion":      verification.CompilerVersion,
		"optimizationEnabled":  verification.OptimizationEnabled,
		"optimizationRuns":     verification.OptimizationRuns,
		"sourceCode":           verification.SourceCode,
		"abi":                  verification.ABI,
		"constructorArguments": verification.ConstructorArguments,
		"verifiedAt":           verification.VerifiedAt.Format(time.RFC3339),
		"licenseType":          verification.LicenseType,
	}, nil
}
