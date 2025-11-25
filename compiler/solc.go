package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SolcCompiler implements the Compiler interface using solc binary
type SolcCompiler struct {
	config *Config
}

// NewSolcCompiler creates a new Solidity compiler instance
func NewSolcCompiler(config *Config) (*SolcCompiler, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create bin directory if it doesn't exist
	if err := os.MkdirAll(config.BinDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Create cache directory if caching is enabled
	if config.CacheEnabled {
		if err := os.MkdirAll(config.CacheDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create cache directory: %w", err)
		}
	}

	return &SolcCompiler{
		config: config,
	}, nil
}

// Compile compiles Solidity source code with the given options
func (s *SolcCompiler) Compile(ctx context.Context, opts *CompilationOptions) (*CompilationResult, error) {
	if err := s.validateCompilationOptions(opts); err != nil {
		return nil, err
	}

	if err := s.ensureCompilerAvailable(ctx, opts.CompilerVersion); err != nil {
		return nil, err
	}

	solcPath := s.getCompilerPath(opts.CompilerVersion)

	tmpDir, sourceFile, err := s.prepareSourceFile(opts.SourceCode)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	args := s.buildSolcArgs(opts, sourceFile)

	compileCtx, cancel := s.createCompilationContext(ctx, opts)
	if cancel != nil {
		defer cancel()
	}

	output, err := s.executeSolcCommand(compileCtx, solcPath, args)
	if err != nil {
		return nil, err
	}

	result, err := s.parseCompilationOutput(output, opts.ContractName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compilation output: %w", err)
	}

	result.CompilerVersion = opts.CompilerVersion

	return result, nil
}

// validateCompilationOptions validates the compilation options
func (s *SolcCompiler) validateCompilationOptions(opts *CompilationOptions) error {
	if opts == nil {
		return fmt.Errorf("options cannot be nil")
	}

	if opts.SourceCode == "" {
		return fmt.Errorf("source code cannot be empty")
	}

	if opts.CompilerVersion == "" {
		return fmt.Errorf("compiler version cannot be empty")
	}

	return nil
}

// ensureCompilerAvailable checks if compiler is available and downloads if needed
func (s *SolcCompiler) ensureCompilerAvailable(ctx context.Context, version string) error {
	available, err := s.IsVersionAvailable(version)
	if err != nil {
		return fmt.Errorf("failed to check version availability: %w", err)
	}

	if !available {
		if s.config.AutoDownload {
			if err := s.DownloadVersion(ctx, version); err != nil {
				return fmt.Errorf("failed to download compiler: %w", err)
			}
		} else {
			return ErrCompilerNotFound
		}
	}

	return nil
}

// prepareSourceFile creates a temporary directory and writes the source code to a file
func (s *SolcCompiler) prepareSourceFile(sourceCode string) (tmpDir string, sourceFile string, err error) {
	tmpDir, err = os.MkdirTemp("", "solc-compile-*")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	sourceFile = filepath.Join(tmpDir, "contract.sol")
	if err := os.WriteFile(sourceFile, []byte(sourceCode), 0644); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", fmt.Errorf("failed to write source file: %w", err)
	}

	return tmpDir, sourceFile, nil
}

// buildSolcArgs builds the solc command arguments
func (s *SolcCompiler) buildSolcArgs(opts *CompilationOptions, sourceFile string) []string {
	args := []string{
		"--combined-json", "abi,bin,metadata",
		"--optimize",
	}

	if opts.OptimizationEnabled {
		args = append(args, "--optimize-runs", fmt.Sprintf("%d", opts.OptimizationRuns))
	}

	if opts.EVMVersion != "" {
		args = append(args, "--evm-version", opts.EVMVersion)
	}

	args = append(args, sourceFile)

	return args
}

// createCompilationContext creates a context with timeout for compilation
func (s *SolcCompiler) createCompilationContext(ctx context.Context, opts *CompilationOptions) (context.Context, context.CancelFunc) {
	if opts.Timeout != nil {
		return opts.Timeout, nil
	}

	return context.WithTimeout(ctx, time.Duration(s.config.MaxCompilationTime)*time.Second)
}

// executeSolcCommand executes the solc command and returns the output
func (s *SolcCompiler) executeSolcCommand(ctx context.Context, solcPath string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, solcPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("%w: %s", ErrCompilationFailed, string(output))
	}

	return output, nil
}

// parseCompilationOutput parses the solc JSON output
func (s *SolcCompiler) parseCompilationOutput(output []byte, contractName string) (*CompilationResult, error) {
	var jsonOutput struct {
		Contracts map[string]struct {
			Abi      string `json:"abi"`
			Bin      string `json:"bin"`
			Metadata string `json:"metadata"`
		} `json:"contracts"`
	}

	if err := json.Unmarshal(output, &jsonOutput); err != nil {
		return nil, fmt.Errorf("failed to parse JSON output: %w", err)
	}

	if len(jsonOutput.Contracts) == 0 {
		return nil, fmt.Errorf("no contracts found in compilation output")
	}

	// If contract name is specified, find that specific contract
	if contractName != "" {
		for key, contract := range jsonOutput.Contracts {
			if strings.Contains(key, contractName) {
				return &CompilationResult{
					Bytecode: contract.Bin,
					ABI:      contract.Abi,
					Metadata: contract.Metadata,
				}, nil
			}
		}
		return nil, fmt.Errorf("contract %s not found in compilation output", contractName)
	}

	// Otherwise return the first contract
	for _, contract := range jsonOutput.Contracts {
		return &CompilationResult{
			Bytecode: contract.Bin,
			ABI:      contract.Abi,
			Metadata: contract.Metadata,
		}, nil
	}

	return nil, fmt.Errorf("no contracts found")
}

// IsVersionAvailable checks if a compiler version is available
func (s *SolcCompiler) IsVersionAvailable(version string) (bool, error) {
	solcPath := s.getCompilerPath(version)
	_, err := os.Stat(solcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ListVersions returns all available compiler versions
func (s *SolcCompiler) ListVersions() ([]string, error) {
	entries, err := os.ReadDir(s.config.BinDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read bin directory: %w", err)
	}

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "solc-") {
			version := strings.TrimPrefix(name, "solc-")
			version = strings.TrimSuffix(version, filepath.Ext(version))
			versions = append(versions, version)
		}
	}

	return versions, nil
}

// DownloadVersion downloads a specific compiler version
func (s *SolcCompiler) DownloadVersion(ctx context.Context, version string) error {
	// Determine platform
	platform := runtime.GOOS
	if platform != "linux" && platform != "darwin" && platform != "windows" {
		return fmt.Errorf("unsupported platform: %s", platform)
	}

	// Build download URL
	// Using solc-bin repository: https://binaries.soliditylang.org/
	baseURL := "https://binaries.soliditylang.org"
	var downloadURL string

	switch platform {
	case "linux":
		downloadURL = fmt.Sprintf("%s/linux-amd64/solc-linux-amd64-v%s", baseURL, version)
	case "darwin":
		downloadURL = fmt.Sprintf("%s/macosx-amd64/solc-macosx-amd64-v%s", baseURL, version)
	case "windows":
		downloadURL = fmt.Sprintf("%s/windows-amd64/solc-windows-amd64-v%s.exe", baseURL, version)
	}

	// Download compiler binary
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download compiler: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download compiler: status %d", resp.StatusCode)
	}

	// Save compiler binary
	solcPath := s.getCompilerPath(version)
	file, err := os.OpenFile(solcPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create compiler file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("failed to save compiler binary: %w", err)
	}

	return nil
}

// Close releases compiler resources
func (s *SolcCompiler) Close() error {
	// No resources to release for now
	return nil
}

// getCompilerPath returns the path to the compiler binary for a specific version
func (s *SolcCompiler) getCompilerPath(version string) string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return filepath.Join(s.config.BinDir, fmt.Sprintf("solc-%s%s", version, ext))
}
