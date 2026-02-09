package verifier

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/compiler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock Compiler ---

type mockCompiler struct {
	compileResult *compiler.CompilationResult
	compileErr    error
	versions      []string
	versionsErr   error
	closeCalled   bool
}

func (m *mockCompiler) Compile(_ context.Context, _ *compiler.CompilationOptions) (*compiler.CompilationResult, error) {
	return m.compileResult, m.compileErr
}

func (m *mockCompiler) IsVersionAvailable(version string) (bool, error) {
	for _, v := range m.versions {
		if v == version {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockCompiler) ListVersions() ([]string, error) {
	return m.versions, m.versionsErr
}

func (m *mockCompiler) DownloadVersion(_ context.Context, _ string) error {
	return nil
}

func (m *mockCompiler) Close() error {
	m.closeCalled = true
	return nil
}

// --- Mock EthClient via interface extraction ---
// Since ContractVerifier uses ethclient.Client directly, we test internal methods
// that don't need the client, and test the full Verify flow with a testable verifier.

// testVerifier wraps ContractVerifier but overrides GetDeployedBytecode
type testVerifier struct {
	*ContractVerifier
	deployedCode string
	deployedErr  error
}

func (tv *testVerifier) GetDeployedBytecode(_ context.Context, _ [20]byte) (string, error) {
	return tv.deployedCode, tv.deployedErr
}

// --- Tests ---

func TestConstants(t *testing.T) {
	assert.Greater(t, MinBytecodeSimilarityThreshold, 0.9)
	assert.Less(t, MinBytecodeSimilarityThreshold, 1.0)
}

func TestErrorSentinels(t *testing.T) {
	assert.True(t, errors.Is(ErrBytecodeMismatch, ErrBytecodeMismatch))
	assert.True(t, errors.Is(ErrNoDeployedCode, ErrNoDeployedCode))
	assert.True(t, errors.Is(ErrInvalidConstructorArgs, ErrInvalidConstructorArgs))
	assert.False(t, errors.Is(ErrBytecodeMismatch, ErrNoDeployedCode))
}

func TestVerificationRequest_Fields(t *testing.T) {
	req := &VerificationRequest{
		SourceCode:          "pragma solidity ^0.8.0;",
		CompilerVersion:     "0.8.20",
		ContractName:        "Token",
		OptimizationEnabled: true,
		OptimizationRuns:    200,
		EVMVersion:          "london",
		ConstructorArguments: "000000000000000000000000000000000000000000000000000000000000002a",
		LicenseType:         "MIT",
	}

	assert.Equal(t, "Token", req.ContractName)
	assert.Equal(t, true, req.OptimizationEnabled)
	assert.Equal(t, 200, req.OptimizationRuns)
}

func TestVerificationResult_Fields(t *testing.T) {
	result := &VerificationResult{
		Success:          true,
		CompiledBytecode: "6080604052",
		DeployedBytecode: "6080604052",
		ABI:              `[{"inputs":[]}]`,
		Metadata:         `{"compiler":{"version":"0.8.20"}}`,
	}

	assert.True(t, result.Success)
	assert.Equal(t, "6080604052", result.CompiledBytecode)
	assert.Nil(t, result.Error)
}

func TestDefaultConfig(t *testing.T) {
	mc := &mockCompiler{}
	// We can't create a real ethclient, so test with nil and validate
	cfg := DefaultConfig(mc, nil)

	assert.Equal(t, mc, cfg.Compiler)
	assert.Nil(t, cfg.EthClient)
	assert.True(t, cfg.AllowMetadataVariance)
}

func TestConfig_Validate(t *testing.T) {
	mc := &mockCompiler{}

	tests := []struct {
		name    string
		config  *Config
		wantErr string
	}{
		{
			name:    "nil compiler",
			config:  &Config{Compiler: nil},
			wantErr: "compiler cannot be nil",
		},
		{
			name:    "nil eth client",
			config:  &Config{Compiler: mc, EthClient: nil},
			wantErr: "eth client cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestNewContractVerifier_NilConfig(t *testing.T) {
	v, err := NewContractVerifier(nil)
	require.Error(t, err)
	assert.Nil(t, v)
	assert.Contains(t, err.Error(), "config cannot be nil")
}

func TestNewContractVerifier_InvalidConfig(t *testing.T) {
	v, err := NewContractVerifier(&Config{})
	require.Error(t, err)
	assert.Nil(t, v)
	assert.Contains(t, err.Error(), "invalid config")
}

// --- stripMetadata tests ---

func TestStripMetadata_OldPattern(t *testing.T) {
	// Old pattern: a165627a7a72305820
	bytecode := "6080604052" + "a165627a7a72305820" + strings.Repeat("ab", 32) + "0029"
	v := &ContractVerifier{config: &Config{}}

	result := v.stripMetadata(bytecode)
	assert.Equal(t, "6080604052", result)
}

func TestStripMetadata_NewPattern(t *testing.T) {
	// New pattern: a264697066735822
	bytecode := "6080604052" + "a264697066735822" + strings.Repeat("cd", 32) + "64736f6c634300081400"
	v := &ContractVerifier{config: &Config{}}

	result := v.stripMetadata(bytecode)
	assert.Equal(t, "6080604052", result)
}

func TestStripMetadata_NoMetadata(t *testing.T) {
	bytecode := "6080604052348015"
	v := &ContractVerifier{config: &Config{}}

	result := v.stripMetadata(bytecode)
	assert.Equal(t, bytecode, result)
}

func TestStripMetadata_Empty(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	assert.Equal(t, "", v.stripMetadata(""))
}

// --- calculateSimilarity tests ---

func TestCalculateSimilarity_Identical(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	sim := v.calculateSimilarity("abcdef", "abcdef")
	assert.Equal(t, 1.0, sim)
}

func TestCalculateSimilarity_CompletelyDifferent(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	sim := v.calculateSimilarity("aaaaaa", "bbbbbb")
	assert.Equal(t, 0.0, sim)
}

func TestCalculateSimilarity_PartialMatch(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	// "abcd" vs "abef" → 2 match out of 4
	sim := v.calculateSimilarity("abcd", "abef")
	assert.InDelta(t, 0.5, sim, 0.01)
}

func TestCalculateSimilarity_DifferentLengths(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	// "abcdef" vs "abc" → 3 match out of 6 (max len)
	sim := v.calculateSimilarity("abcdef", "abc")
	assert.InDelta(t, 0.5, sim, 0.01)
}

func TestCalculateSimilarity_BothEmpty(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	assert.Equal(t, 1.0, v.calculateSimilarity("", ""))
}

func TestCalculateSimilarity_OneEmpty(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	assert.Equal(t, 0.0, v.calculateSimilarity("abc", ""))
	assert.Equal(t, 0.0, v.calculateSimilarity("", "abc"))
}

// --- CompareBytecode tests ---

func TestCompareBytecode_ExactMatch(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: false}}
	match, err := v.CompareBytecode("6080604052", "6080604052", "")
	require.NoError(t, err)
	assert.True(t, match)
}

func TestCompareBytecode_ExactMatch_With0xPrefix(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: false}}
	match, err := v.CompareBytecode("0x6080604052", "0x6080604052", "")
	require.NoError(t, err)
	assert.True(t, match)
}

func TestCompareBytecode_Mismatch_NoVariance(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: false}}
	match, err := v.CompareBytecode("6080604052", "6080604053", "")
	require.NoError(t, err)
	assert.False(t, match)
}

func TestCompareBytecode_MetadataVariance(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: true}}

	// Same code, different metadata
	code := "6080604052348015600e5780fd5b50"
	deployed := code + "a264697066735822" + strings.Repeat("aa", 32) + "64736f6c634300081400"
	compiled := code + "a264697066735822" + strings.Repeat("bb", 32) + "64736f6c634300081400"

	match, err := v.CompareBytecode(deployed, compiled, "")
	require.NoError(t, err)
	assert.True(t, match)
}

func TestCompareBytecode_ConstructorArgs_Ignored(t *testing.T) {
	// Constructor args don't affect runtime bytecode comparison
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: false}}
	match, err := v.CompareBytecode("6080604052", "6080604052", "000000000000000000000000000000000000000000000000000000000000002a")
	require.NoError(t, err)
	assert.True(t, match)
}

// --- maskImmutablePositions tests ---

func TestMaskImmutablePositions_NoRefs(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	result := v.maskImmutablePositions("6080604052", nil)
	assert.Equal(t, "6080604052", result)
}

func TestMaskImmutablePositions_WithRefs(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	// Create bytecode of at least 20 hex chars
	bytecode := "60806040523480156000e0"

	refs := map[string][]compiler.ImmutableReference{
		"42": {
			{Start: 2, Length: 3}, // byte offset 2, length 3 → hex offset 4, length 6
		},
	}

	result := v.maskImmutablePositions(bytecode, refs)
	// Positions 4-9 should be masked with 'X'
	assert.Equal(t, "6080XXXXXX3480156000e0", result)
}

func TestMaskImmutablePositions_OutOfBounds(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	bytecode := "608060"

	refs := map[string][]compiler.ImmutableReference{
		"1": {
			{Start: 100, Length: 32}, // way out of bounds
		},
	}

	result := v.maskImmutablePositions(bytecode, refs)
	assert.Equal(t, "608060", result) // unchanged
}

func TestMaskImmutablePositions_PartialOutOfBounds(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	bytecode := "6080604052" // 10 hex chars = 5 bytes

	refs := map[string][]compiler.ImmutableReference{
		"1": {
			{Start: 3, Length: 10}, // starts at hex 6, length 20 → overflows
		},
	}

	result := v.maskImmutablePositions(bytecode, refs)
	// From hex offset 6 to end should be masked
	assert.Equal(t, "608060XXXX", result)
}

func TestMaskImmutablePositions_MultipleRefs(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	bytecode := "6080604052348015600e"

	refs := map[string][]compiler.ImmutableReference{
		"1": {{Start: 0, Length: 2}},  // hex 0-3
		"2": {{Start: 5, Length: 2}},  // hex 10-13
	}

	result := v.maskImmutablePositions(bytecode, refs)
	assert.Equal(t, "XXXX604052XXXX15600e", result)
}

// --- CompareBytecodeWithImmutables tests ---

func TestCompareBytecodeWithImmutables_ExactMatch(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: true}}

	match, err := v.CompareBytecodeWithImmutables("6080604052", "6080604052", nil)
	require.NoError(t, err)
	assert.True(t, match)
}

func TestCompareBytecodeWithImmutables_DiffOnlyInImmutables(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: true}}

	// Create bytecodes that differ only at immutable positions
	// 20 hex chars = 10 bytes
	deployed := "6080604052aaaa15600e"
	compiled := "6080604052bbbb15600e"

	refs := map[string][]compiler.ImmutableReference{
		"42": {{Start: 5, Length: 2}}, // hex 10-13 where aa/bb differ
	}

	match, err := v.CompareBytecodeWithImmutables(deployed, compiled, refs)
	require.NoError(t, err)
	assert.True(t, match)
}

func TestCompareBytecodeWithImmutables_NoRefs_FallsBackToSimilarity(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: true}}

	// Very similar bytecodes (differ by just metadata)
	code := strings.Repeat("60", 100)
	deployed := code + "a264697066735822" + strings.Repeat("aa", 32) + "end"
	compiled := code + "a264697066735822" + strings.Repeat("bb", 32) + "end"

	match, err := v.CompareBytecodeWithImmutables(deployed, compiled, nil)
	require.NoError(t, err)
	assert.True(t, match) // metadata stripped, code is identical
}

func TestCompareBytecodeWithImmutables_CompletelyDifferent(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: true}}

	deployed := strings.Repeat("aa", 50)
	compiled := strings.Repeat("bb", 50)

	match, err := v.CompareBytecodeWithImmutables(deployed, compiled, nil)
	require.NoError(t, err)
	assert.False(t, match)
}

func TestCompareBytecodeWithImmutables_With0xPrefix(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: true}}

	match, err := v.CompareBytecodeWithImmutables("0x6080604052", "0x6080604052", nil)
	require.NoError(t, err)
	assert.True(t, match)
}

// --- compareBytecodeWithoutMetadata tests ---

func TestCompareBytecodeWithoutMetadata_SameCodeDiffMeta(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: true}}

	code := "6080604052348015600e5780fd5b50"
	deployed := code + "a165627a7a72305820" + strings.Repeat("11", 32) + "0029"
	compiled := code + "a165627a7a72305820" + strings.Repeat("22", 32) + "0029"

	match, err := v.compareBytecodeWithoutMetadata(deployed, compiled)
	require.NoError(t, err)
	assert.True(t, match)
}

func TestCompareBytecodeWithoutMetadata_HighSimilarity(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: true}}

	// 97% similar (above threshold)
	base := strings.Repeat("60", 100)
	deployed := base[:197] + "aaa" // change last 3 chars
	compiled := base

	match, err := v.compareBytecodeWithoutMetadata(deployed, compiled)
	require.NoError(t, err)
	assert.True(t, match)
}

func TestCompareBytecodeWithoutMetadata_LowSimilarity(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: true}}

	deployed := strings.Repeat("aa", 50)
	compiled := strings.Repeat("bb", 50)

	match, err := v.compareBytecodeWithoutMetadata(deployed, compiled)
	require.NoError(t, err)
	assert.False(t, match)
}

// --- Verify tests (using mock pattern to avoid ethclient) ---

func TestVerify_NilRequest(t *testing.T) {
	mc := &mockCompiler{}
	// Create verifier bypassing ethclient validation
	v := &ContractVerifier{
		config: &Config{
			Compiler:              mc,
			AllowMetadataVariance: true,
		},
	}

	result, err := v.Verify(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// --- Close tests ---

func TestClose_CallsCompilerClose(t *testing.T) {
	mc := &mockCompiler{}
	v := &ContractVerifier{config: &Config{Compiler: mc}}

	err := v.Close()
	require.NoError(t, err)
	assert.True(t, mc.closeCalled)
}

func TestClose_NilCompiler(t *testing.T) {
	v := &ContractVerifier{config: &Config{}}
	err := v.Close()
	require.NoError(t, err)
}

// --- GetDeployedBytecode tests (requires working ethclient, so test hex encoding) ---

func TestHexEncoding(t *testing.T) {
	// Test that our hex encoding matches what GetDeployedBytecode would return
	code := []byte{0x60, 0x80, 0x60, 0x40, 0x52}
	encoded := hex.EncodeToString(code)
	assert.Equal(t, "6080604052", encoded)
}

// --- max function test ---

func TestMax(t *testing.T) {
	assert.Equal(t, 5, max(3, 5))
	assert.Equal(t, 5, max(5, 3))
	assert.Equal(t, 3, max(3, 3))
	assert.Equal(t, 0, max(0, 0))
	assert.Equal(t, 0, max(-1, 0))
}

// --- Integration-style tests (all internal, no network) ---

func TestVerificationWorkflow_MatchingBytecode(t *testing.T) {
	// Simulate the full verification logic without network calls
	mc := &mockCompiler{
		compileResult: &compiler.CompilationResult{
			Bytecode: "6080604052348015600e5780fd5b50",
			ABI:      `[{"inputs":[]}]`,
			Metadata: `{"compiler":"0.8.20"}`,
		},
	}

	v := &ContractVerifier{
		config: &Config{
			Compiler:              mc,
			AllowMetadataVariance: true,
		},
	}

	// Simulate what Verify does internally:
	deployedBytecode := "6080604052348015600e5780fd5b50"
	compiledBytecode := mc.compileResult.Bytecode

	match, err := v.CompareBytecodeWithImmutables(deployedBytecode, compiledBytecode, nil)
	require.NoError(t, err)
	assert.True(t, match)
}

func TestVerificationWorkflow_MetadataDifference(t *testing.T) {
	code := "6080604052348015600e5780fd5b50"

	v := &ContractVerifier{
		config: &Config{
			Compiler:              &mockCompiler{},
			AllowMetadataVariance: true,
		},
	}

	// Same code, different metadata
	deployed := code + "a264697066735822" + strings.Repeat("aa", 32) + "64736f6c634300081400"
	compiled := code + "a264697066735822" + strings.Repeat("bb", 32) + "64736f6c634300081400"

	match, err := v.CompareBytecodeWithImmutables(deployed, compiled, nil)
	require.NoError(t, err)
	assert.True(t, match)
}

func TestVerificationWorkflow_ImmutableVariables(t *testing.T) {
	v := &ContractVerifier{
		config: &Config{
			Compiler:              &mockCompiler{},
			AllowMetadataVariance: true,
		},
	}

	// Bytecodes differ at immutable positions (same length, 20 hex chars = 10 bytes each section)
	deployed := "6080604052" + "deadbeefdeadbeefdead" + "348015600e"
	compiled := "6080604052" + "00000000000000000000" + "348015600e"

	// Immutable at byte offset 5, length 10 (the 20 hex char portion)
	refs := map[string][]compiler.ImmutableReference{
		"100": {{Start: 5, Length: 10}},
	}

	match, err := v.CompareBytecodeWithImmutables(deployed, compiled, refs)
	require.NoError(t, err)
	assert.True(t, match)
}

func TestVerificationWorkflow_CompleteMismatch(t *testing.T) {
	v := &ContractVerifier{
		config: &Config{
			Compiler:              &mockCompiler{},
			AllowMetadataVariance: true,
		},
	}

	deployed := strings.Repeat("aa", 100)
	compiled := strings.Repeat("bb", 100)

	match, err := v.CompareBytecodeWithImmutables(deployed, compiled, nil)
	require.NoError(t, err)
	assert.False(t, match)
}

func TestVerificationWorkflow_EmptyDeployed(t *testing.T) {
	v := &ContractVerifier{
		config: &Config{
			Compiler:              &mockCompiler{},
			AllowMetadataVariance: true,
		},
	}

	match, err := v.CompareBytecodeWithImmutables("", "6080604052", nil)
	require.NoError(t, err)
	assert.False(t, match)
}

// --- Edge case: real-world-like bytecode comparison ---

func TestRealWorldLikeBytecodeComparison(t *testing.T) {
	v := &ContractVerifier{
		config: &Config{
			Compiler:              &mockCompiler{},
			AllowMetadataVariance: true,
		},
	}

	// Simulate realistic ERC20 contract bytecode (simplified)
	coreCode := "608060405234801561001057600080fd5b506040516109" +
		"d83803806109d883398181016040528101906100339190" +
		"6101a557600073ffffffffffffffffffffffffffffffff" +
		"ffffffff16815260200190815260200160002081905550"

	// Different IPFS hashes in metadata
	metaDeployed := "a264697066735822" + fmt.Sprintf("%064x", 12345) + "64736f6c634300081400"
	metaCompiled := "a264697066735822" + fmt.Sprintf("%064x", 67890) + "64736f6c634300081400"

	deployed := coreCode + metaDeployed
	compiled := coreCode + metaCompiled

	match, err := v.CompareBytecodeWithImmutables(deployed, compiled, nil)
	require.NoError(t, err)
	assert.True(t, match, "should match when only metadata differs")
}

func TestSimilarityThreshold_BoundaryValues(t *testing.T) {
	v := &ContractVerifier{config: &Config{AllowMetadataVariance: true}}

	// Generate bytecodes at various similarity levels
	tests := []struct {
		name      string
		diffPct   float64 // percentage of differing chars
		wantMatch bool
	}{
		{"1% different", 0.01, true},
		{"5% different", 0.05, true},
		{"7% different - boundary", 0.07, false}, // threshold is 0.93
		{"10% different", 0.10, false},
		{"50% different", 0.50, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := 200
			base := strings.Repeat("a", size)
			diffCount := int(float64(size) * tt.diffPct)

			modified := []byte(base)
			for i := 0; i < diffCount; i++ {
				modified[i] = 'b'
			}

			match, err := v.compareBytecodeWithoutMetadata(string(modified), base)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMatch, match)
		})
	}
}
