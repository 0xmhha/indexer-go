package graphql

import (
	"context"
	"fmt"
	"time"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/verifier"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// verificationParams holds the parameters for contract verification
type verificationParams struct {
	addressStr           string
	sourceCode           string
	compilerVersion      string
	optimizationEnabled  bool
	optimizationRuns     int
	constructorArguments string
	contractName         string
	licenseType          string
}

// ContractVerificationResponse represents the GraphQL response for contract verification
type ContractVerificationResponse struct {
	Address              string `json:"address"`
	IsVerified           bool   `json:"isVerified"`
	Name                 string `json:"name,omitempty"`
	CompilerVersion      string `json:"compilerVersion,omitempty"`
	OptimizationEnabled  bool   `json:"optimizationEnabled,omitempty"`
	OptimizationRuns     int    `json:"optimizationRuns,omitempty"`
	SourceCode           string `json:"sourceCode,omitempty"`
	ABI                  string `json:"abi,omitempty"`
	ConstructorArguments string `json:"constructorArguments,omitempty"`
	VerifiedAt           string `json:"verifiedAt,omitempty"`
	LicenseType          string `json:"licenseType,omitempty"`
}

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
			return &ContractVerificationResponse{
				Address:    address.Hex(),
				IsVerified: false,
			}, nil
		}
		s.logger.Error("failed to get contract verification",
			zap.String("address", address.Hex()),
			zap.Error(err))
		return nil, err
	}

	// Convert to GraphQL format
	return toVerificationResponse(verification), nil
}

// resolveVerifyContract handles the verifyContract mutation
func (s *Schema) resolveVerifyContract(params graphql.ResolveParams) (interface{}, error) {
	ctx := params.Context

	// Extract and validate parameters
	vParams, err := extractVerificationParams(params)
	if err != nil {
		return nil, err
	}

	address, err := validateVerificationInputs(vParams.addressStr)
	if err != nil {
		return nil, err
	}

	// Check storage and verifier availability
	verificationWriter, err := s.getVerificationWriter()
	if err != nil {
		return nil, err
	}

	if s.verifier == nil {
		return nil, fmt.Errorf("contract verifier is not configured")
	}

	// Build and execute verification request
	req := buildVerificationRequest(vParams, address)

	result, err := s.executeVerification(ctx, address, req)
	if err != nil {
		return nil, err
	}

	// Store verification result
	verification, err := s.storeVerificationResult(ctx, verificationWriter, vParams, address, result)
	if err != nil {
		return nil, err
	}

	s.logger.Info("contract verified successfully",
		zap.String("address", address.Hex()),
		zap.String("compiler", vParams.compilerVersion))

	// Return verification result
	return toVerificationResponse(verification), nil
}

// extractVerificationParams extracts and validates parameters from GraphQL params
func extractVerificationParams(params graphql.ResolveParams) (*verificationParams, error) {
	// Extract required parameters
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

	// Extract optional parameters
	optimizationRuns := DefaultOptimizationRuns
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

	return &verificationParams{
		addressStr:           addressStr,
		sourceCode:           sourceCode,
		compilerVersion:      compilerVersion,
		optimizationEnabled:  optimizationEnabled,
		optimizationRuns:     optimizationRuns,
		constructorArguments: constructorArguments,
		contractName:         contractName,
		licenseType:          licenseType,
	}, nil
}

// validateVerificationInputs validates the address format
func validateVerificationInputs(addressStr string) (common.Address, error) {
	if !common.IsHexAddress(addressStr) {
		return common.Address{}, fmt.Errorf("invalid address format")
	}
	return common.HexToAddress(addressStr), nil
}

// getVerificationWriter checks if storage supports contract verification writes
func (s *Schema) getVerificationWriter() (storage.ContractVerificationWriter, error) {
	verificationWriter, ok := s.storage.(storage.ContractVerificationWriter)
	if !ok {
		return nil, fmt.Errorf("storage does not support contract verification writes")
	}
	return verificationWriter, nil
}

// buildVerificationRequest creates a verification request from parameters
func buildVerificationRequest(vParams *verificationParams, address common.Address) *verifier.VerificationRequest {
	return &verifier.VerificationRequest{
		Address:              address,
		SourceCode:           vParams.sourceCode,
		CompilerVersion:      vParams.compilerVersion,
		ContractName:         vParams.contractName,
		OptimizationEnabled:  vParams.optimizationEnabled,
		OptimizationRuns:     vParams.optimizationRuns,
		ConstructorArguments: vParams.constructorArguments,
		LicenseType:          vParams.licenseType,
	}
}

// executeVerification executes the verification and handles errors
func (s *Schema) executeVerification(ctx context.Context, address common.Address, req *verifier.VerificationRequest) (*verifier.VerificationResult, error) {
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

	return result, nil
}

// storeVerificationResult stores the verification result in the database
func (s *Schema) storeVerificationResult(
	ctx context.Context,
	writer storage.ContractVerificationWriter,
	vParams *verificationParams,
	address common.Address,
	result *verifier.VerificationResult,
) (*storage.ContractVerification, error) {
	verification := &storage.ContractVerification{
		Address:              address,
		IsVerified:           true,
		Name:                 vParams.contractName,
		CompilerVersion:      vParams.compilerVersion,
		OptimizationEnabled:  vParams.optimizationEnabled,
		OptimizationRuns:     vParams.optimizationRuns,
		SourceCode:           vParams.sourceCode,
		ABI:                  result.ABI,
		ConstructorArguments: vParams.constructorArguments,
		VerifiedAt:           time.Now(),
		LicenseType:          vParams.licenseType,
	}

	if err := writer.SetContractVerification(ctx, verification); err != nil {
		s.logger.Error("failed to store contract verification",
			zap.String("address", address.Hex()),
			zap.Error(err))
		return nil, fmt.Errorf("failed to store verification: %w", err)
	}

	return verification, nil
}

// toVerificationResponse converts storage.ContractVerification to GraphQL response
func toVerificationResponse(verification *storage.ContractVerification) *ContractVerificationResponse {
	return &ContractVerificationResponse{
		Address:              verification.Address.Hex(),
		IsVerified:           verification.IsVerified,
		Name:                 verification.Name,
		CompilerVersion:      verification.CompilerVersion,
		OptimizationEnabled:  verification.OptimizationEnabled,
		OptimizationRuns:     verification.OptimizationRuns,
		SourceCode:           verification.SourceCode,
		ABI:                  verification.ABI,
		ConstructorArguments: verification.ConstructorArguments,
		VerifiedAt:           verification.VerifiedAt.Format(time.RFC3339),
		LicenseType:          verification.LicenseType,
	}
}
