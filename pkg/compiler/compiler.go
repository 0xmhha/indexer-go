package compiler

import (
	"context"
	"errors"
	"fmt"
)

// Common errors
var (
	// ErrCompilerNotFound is returned when the compiler binary is not found
	ErrCompilerNotFound = errors.New("compiler not found")

	// ErrCompilationFailed is returned when compilation fails
	ErrCompilationFailed = errors.New("compilation failed")

	// ErrInvalidVersion is returned when an invalid compiler version is specified
	ErrInvalidVersion = errors.New("invalid compiler version")

	// ErrUnsupportedVersion is returned when an unsupported compiler version is requested
	ErrUnsupportedVersion = errors.New("unsupported compiler version")

	// ErrTimeout is returned when compilation times out
	ErrTimeout = errors.New("compilation timeout")
)

// ImmutableReference represents a single immutable variable reference position
type ImmutableReference struct {
	Start  int `json:"start"`
	Length int `json:"length"`
}

// CompilationResult represents the result of a successful compilation
type CompilationResult struct {
	// Bytecode is the compiled contract bytecode (without 0x prefix)
	Bytecode string
	// ABI is the contract ABI as JSON string
	ABI string
	// Metadata is the contract metadata
	Metadata string
	// CompilerVersion is the version of the compiler used
	CompilerVersion string
	// ImmutableReferences maps AST node IDs to positions in bytecode where immutable values are stored
	// Key is the AST node ID (as string), value is array of positions
	ImmutableReferences map[string][]ImmutableReference
}

// CompilationOptions represents options for contract compilation
type CompilationOptions struct {
	// SourceCode is the Solidity source code
	SourceCode string
	// CompilerVersion is the desired compiler version (e.g., "0.8.20")
	CompilerVersion string
	// ContractName is the name of the contract to compile (required for multiple contracts)
	ContractName string
	// OptimizationEnabled enables optimization
	OptimizationEnabled bool
	// OptimizationRuns is the number of optimization runs
	OptimizationRuns int
	// EVMVersion is the target EVM version (e.g., "london", "paris")
	EVMVersion string
	// Libraries is a map of library addresses for linking
	Libraries map[string]string
	// Timeout is the maximum compilation time
	Timeout context.Context
}

// Compiler provides Solidity contract compilation
type Compiler interface {
	// Compile compiles Solidity source code with the given options
	Compile(ctx context.Context, opts *CompilationOptions) (*CompilationResult, error)

	// IsVersionAvailable checks if a compiler version is available
	IsVersionAvailable(version string) (bool, error)

	// ListVersions returns all available compiler versions
	ListVersions() ([]string, error)

	// DownloadVersion downloads a specific compiler version
	DownloadVersion(ctx context.Context, version string) error

	// Close releases compiler resources
	Close() error
}

// Config holds compiler configuration
type Config struct {
	// BinDir is the directory for compiler binaries
	BinDir string

	// MaxCompilationTime is the maximum time for a single compilation
	MaxCompilationTime int // seconds

	// CacheEnabled enables caching of compilation results
	CacheEnabled bool

	// CacheDir is the directory for caching compiled results
	CacheDir string

	// AutoDownload automatically downloads missing compiler versions
	AutoDownload bool
}

// DefaultConfig returns a default compiler configuration
func DefaultConfig() *Config {
	return &Config{
		BinDir:             "./solc-bin",
		MaxCompilationTime: 30, // 30 seconds
		CacheEnabled:       true,
		CacheDir:           "./solc-cache",
		AutoDownload:       true,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.BinDir == "" {
		return fmt.Errorf("BinDir cannot be empty")
	}
	if c.MaxCompilationTime <= 0 {
		return fmt.Errorf("MaxCompilationTime must be positive")
	}
	if c.CacheEnabled && c.CacheDir == "" {
		return fmt.Errorf("CacheDir cannot be empty when caching is enabled")
	}
	return nil
}
