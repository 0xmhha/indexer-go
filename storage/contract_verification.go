package storage

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// ContractVerification represents verified contract source code information
type ContractVerification struct {
	// Address is the contract address
	Address common.Address
	// IsVerified indicates if the contract is verified
	IsVerified bool
	// Name is the contract name
	Name string
	// CompilerVersion is the Solidity compiler version used
	CompilerVersion string
	// OptimizationEnabled indicates if optimization was enabled
	OptimizationEnabled bool
	// OptimizationRuns is the number of optimization runs
	OptimizationRuns int
	// SourceCode is the contract source code
	SourceCode string
	// ABI is the contract ABI as JSON string
	ABI string
	// ConstructorArguments are the constructor arguments (hex encoded)
	ConstructorArguments string
	// VerifiedAt is the verification timestamp
	VerifiedAt time.Time
	// LicenseType is the contract license (e.g., "MIT", "Apache-2.0")
	LicenseType string
}

// ContractVerificationReader provides read access to contract verification data
type ContractVerificationReader interface {
	// GetContractVerification returns verification data for a contract
	GetContractVerification(ctx context.Context, address common.Address) (*ContractVerification, error)

	// IsContractVerified checks if a contract is verified
	IsContractVerified(ctx context.Context, address common.Address) (bool, error)

	// ListVerifiedContracts returns all verified contract addresses with pagination
	ListVerifiedContracts(ctx context.Context, limit, offset int) ([]common.Address, error)

	// CountVerifiedContracts returns the total number of verified contracts
	CountVerifiedContracts(ctx context.Context) (int, error)
}

// ContractVerificationWriter provides write access to contract verification data
type ContractVerificationWriter interface {
	// SetContractVerification stores contract verification data
	SetContractVerification(ctx context.Context, verification *ContractVerification) error

	// DeleteContractVerification removes contract verification data
	DeleteContractVerification(ctx context.Context, address common.Address) error
}
