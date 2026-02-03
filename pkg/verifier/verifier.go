package verifier

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/0xmhha/indexer-go/pkg/compiler"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Common errors
var (
	// ErrBytecodeMismatch is returned when deployed and compiled bytecode don't match
	ErrBytecodeMismatch = errors.New("bytecode mismatch")

	// ErrNoDeployedCode is returned when no code is deployed at the address
	ErrNoDeployedCode = errors.New("no deployed code at address")

	// ErrInvalidConstructorArgs is returned when constructor arguments are invalid
	ErrInvalidConstructorArgs = errors.New("invalid constructor arguments")
)

// VerificationRequest represents a contract verification request
type VerificationRequest struct {
	// Address is the contract address to verify
	Address common.Address
	// SourceCode is the Solidity source code
	SourceCode string
	// CompilerVersion is the Solidity compiler version
	CompilerVersion string
	// ContractName is the name of the contract (required for multiple contracts)
	ContractName string
	// OptimizationEnabled indicates if optimization was enabled
	OptimizationEnabled bool
	// OptimizationRuns is the number of optimization runs
	OptimizationRuns int
	// EVMVersion is the target EVM version
	EVMVersion string
	// ConstructorArguments are the constructor arguments (hex encoded, without 0x)
	ConstructorArguments string
	// LicenseType is the contract license
	LicenseType string
}

// VerificationResult represents the result of a verification attempt
type VerificationResult struct {
	// Success indicates if verification succeeded
	Success bool
	// CompiledBytecode is the compiled bytecode
	CompiledBytecode string
	// DeployedBytecode is the deployed bytecode
	DeployedBytecode string
	// ABI is the contract ABI
	ABI string
	// Metadata is the contract metadata
	Metadata string
	// Error is the verification error if any
	Error error
}

// Verifier provides contract source code verification
type Verifier interface {
	// Verify verifies a contract's source code
	Verify(ctx context.Context, req *VerificationRequest) (*VerificationResult, error)

	// GetDeployedBytecode retrieves the deployed bytecode for an address
	GetDeployedBytecode(ctx context.Context, address common.Address) (string, error)

	// CompareBytecode compares deployed and compiled bytecode
	CompareBytecode(deployed, compiled, constructorArgs string) (bool, error)

	// Close releases verifier resources
	Close() error
}

// Config holds verifier configuration
type Config struct {
	// Compiler is the Solidity compiler instance
	Compiler compiler.Compiler

	// EthClient is the Ethereum client for fetching deployed bytecode
	EthClient *ethclient.Client

	// AllowMetadataVariance allows metadata hash differences in bytecode
	AllowMetadataVariance bool
}

// DefaultConfig returns a default verifier configuration
func DefaultConfig(compiler compiler.Compiler, ethClient *ethclient.Client) *Config {
	return &Config{
		Compiler:              compiler,
		EthClient:             ethClient,
		AllowMetadataVariance: true,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Compiler == nil {
		return fmt.Errorf("compiler cannot be nil")
	}
	if c.EthClient == nil {
		return fmt.Errorf("eth client cannot be nil")
	}
	return nil
}

// ContractVerifier implements the Verifier interface
type ContractVerifier struct {
	config *Config
}

// NewContractVerifier creates a new contract verifier instance
func NewContractVerifier(config *Config) (*ContractVerifier, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &ContractVerifier{
		config: config,
	}, nil
}

// Verify verifies a contract's source code
func (v *ContractVerifier) Verify(ctx context.Context, req *VerificationRequest) (*VerificationResult, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	result := &VerificationResult{
		Success: false,
	}

	// Get deployed bytecode
	deployedBytecode, err := v.GetDeployedBytecode(ctx, req.Address)
	if err != nil {
		result.Error = err
		return result, err
	}

	if deployedBytecode == "" || deployedBytecode == "0x" {
		result.Error = ErrNoDeployedCode
		return result, ErrNoDeployedCode
	}

	result.DeployedBytecode = deployedBytecode

	// Compile source code
	compileOpts := &compiler.CompilationOptions{
		SourceCode:          req.SourceCode,
		CompilerVersion:     req.CompilerVersion,
		ContractName:        req.ContractName,
		OptimizationEnabled: req.OptimizationEnabled,
		OptimizationRuns:    req.OptimizationRuns,
		EVMVersion:          req.EVMVersion,
		Timeout:             ctx,
	}

	compileResult, err := v.config.Compiler.Compile(ctx, compileOpts)
	if err != nil {
		result.Error = fmt.Errorf("compilation failed: %w", err)
		return result, result.Error
	}

	result.CompiledBytecode = compileResult.Bytecode
	result.ABI = compileResult.ABI
	result.Metadata = compileResult.Metadata

	// Compare bytecode
	match, err := v.CompareBytecode(deployedBytecode, compileResult.Bytecode, req.ConstructorArguments)
	if err != nil {
		result.Error = err
		return result, err
	}

	if !match {
		result.Error = ErrBytecodeMismatch
		return result, ErrBytecodeMismatch
	}

	result.Success = true
	return result, nil
}

// GetDeployedBytecode retrieves the deployed bytecode for an address
func (v *ContractVerifier) GetDeployedBytecode(ctx context.Context, address common.Address) (string, error) {
	code, err := v.config.EthClient.CodeAt(ctx, address, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get code: %w", err)
	}

	return hex.EncodeToString(code), nil
}

// CompareBytecode compares deployed and compiled bytecode
// Note: eth_getCode returns runtime bytecode, which does NOT include constructor arguments.
// Constructor arguments are only in creation bytecode (deployment transaction).
// Therefore, we compare runtime bytecode directly without removing constructor args.
func (v *ContractVerifier) CompareBytecode(deployed, compiled, constructorArgs string) (bool, error) {
	// Remove 0x prefix if present
	deployed = strings.TrimPrefix(deployed, "0x")
	compiled = strings.TrimPrefix(compiled, "0x")
	// Note: constructorArgs is not used for runtime bytecode comparison
	// It's kept in the signature for API compatibility

	// Direct comparison
	if deployed == compiled {
		return true, nil
	}

	// If metadata variance is allowed, try comparing without metadata
	if v.config.AllowMetadataVariance {
		return v.compareBytecodeWithoutMetadata(deployed, compiled)
	}

	return false, nil
}

// compareBytecodeWithoutMetadata compares bytecode ignoring metadata hash
// Solidity appends metadata at the end of bytecode with the pattern:
// 0xa165627a7a72305820{32-byte-hash}0029
// or for newer versions:
// 0xa264697066735822{32-byte-hash}64736f6c63{version}
func (v *ContractVerifier) compareBytecodeWithoutMetadata(deployed, compiled string) (bool, error) {
	// Find metadata marker in deployed bytecode
	deployedWithoutMeta := v.stripMetadata(deployed)
	compiledWithoutMeta := v.stripMetadata(compiled)

	// Compare bytecode without metadata
	if deployedWithoutMeta == compiledWithoutMeta {
		return true, nil
	}

	// Calculate similarity ratio
	similarity := v.calculateSimilarity(deployedWithoutMeta, compiledWithoutMeta)

	// If similarity is high enough, consider it a match
	return similarity > MinBytecodeSimilarityThreshold, nil
}

// stripMetadata removes metadata from bytecode
func (v *ContractVerifier) stripMetadata(bytecode string) string {
	// Old metadata pattern: 0xa165627a7a72305820
	oldPattern := "a165627a7a72305820"
	// New metadata pattern: 0xa264697066735822
	newPattern := "a264697066735822"

	// Try to find and remove old metadata
	if idx := strings.LastIndex(bytecode, oldPattern); idx != -1 {
		return bytecode[:idx]
	}

	// Try to find and remove new metadata
	if idx := strings.LastIndex(bytecode, newPattern); idx != -1 {
		return bytecode[:idx]
	}

	return bytecode
}

// calculateSimilarity calculates the similarity ratio between two strings
func (v *ContractVerifier) calculateSimilarity(s1, s2 string) float64 {
	if len(s1) == 0 && len(s2) == 0 {
		return 1.0
	}
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	// Use simple byte-by-byte comparison for now
	minLen := len(s1)
	if len(s2) < minLen {
		minLen = len(s2)
	}

	matches := 0
	for i := 0; i < minLen; i++ {
		if s1[i] == s2[i] {
			matches++
		}
	}

	return float64(matches) / float64(max(len(s1), len(s2)))
}

// Close releases verifier resources
func (v *ContractVerifier) Close() error {
	if v.config.Compiler != nil {
		return v.config.Compiler.Close()
	}
	return nil
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
