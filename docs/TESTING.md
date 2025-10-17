# Testing Guide

This document provides guidelines for testing in the indexer-go project.

## Table of Contents

- [Overview](#overview)
- [Running Tests](#running-tests)
- [Test Structure](#test-structure)
- [Coverage Requirements](#coverage-requirements)
- [Writing Tests](#writing-tests)
- [Test Utilities](#test-utilities)
- [Integration Tests](#integration-tests)
- [Best Practices](#best-practices)

## Overview

The indexer-go project follows Test-Driven Development (TDD) practices with high test coverage standards. All components must meet minimum coverage requirements before being merged.

**Current Coverage Status**:
- **fetch**: 90.0% ✅
- **internal/config**: 95.0% ✅
- **internal/logger**: 91.7% ✅
- **storage**: 72.4% (target: 90%)
- **client**: 16.7% unit tests (integration tests require running Ethereum node)

## Running Tests

### Quick Start

```bash
# Run all unit tests
./scripts/test.sh

# Run with verbose output
./scripts/test.sh -v

# Generate coverage report
./scripts/test.sh -c

# Generate HTML coverage report (opens in browser)
./scripts/test.sh -h

# Run all tests including integration tests
./scripts/test.sh -a
```

### Manual Test Commands

```bash
# Run all tests in short mode (skip integration tests)
go test -short ./...

# Run tests with coverage
go test -short -cover ./...

# Run tests for a specific package
go test -v ./fetch/

# Run a specific test
go test -v ./fetch/ -run TestFetchBlock

# Run tests with race detector
go test -race -short ./...

# Generate coverage report
go test -short -coverprofile=coverage.out -covermode=atomic ./...
go tool cover -html=coverage.out -o coverage.html
```

## Test Structure

### Directory Structure

```
indexer-go/
├── fetch/
│   ├── fetcher.go
│   └── fetcher_test.go
├── internal/
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── logger/
│   │   ├── logger.go
│   │   └── logger_test.go
│   └── testutil/          # Test utilities and fixtures
│       └── testutil.go
├── storage/
│   ├── pebble.go
│   ├── pebble_test.go
│   └── storage.go
└── scripts/
    └── test.sh            # Test automation script
```

### Test File Naming

- Test files must be named `*_test.go`
- Test files should be in the same package as the code they test
- Integration tests should use build tags: `//go:build integration`

### Test Function Naming

```go
// Unit test
func TestFunctionName(t *testing.T) { ... }

// Table-driven test
func TestFunctionName(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        // test cases
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test code
        })
    }
}

// Integration test
func TestClientIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    // test code
}
```

## Coverage Requirements

### Minimum Coverage Targets

| Component | Target | Current | Status |
|-----------|--------|---------|--------|
| fetch | 90% | 90.0% | ✅ |
| internal/config | 90% | 95.0% | ✅ |
| internal/logger | 90% | 91.7% | ✅ |
| storage | 90% | 72.4% | 🔄 |
| client | 90% | 16.7%* | 🔄 |

\* Client package has low unit test coverage because most functionality requires integration tests with a running Ethereum node.

### Coverage Standards

- **90% minimum** for all production code
- **100%** for critical paths (storage, transaction processing)
- **80% minimum** for utility packages
- Integration tests are separate and don't count towards unit test coverage

### Excluded from Coverage

- Generated code
- Test utilities
- Main functions
- Experimental features

## Writing Tests

### Test-Driven Development (TDD)

1. **Write the test first** - Define expected behavior
2. **Run the test** - Verify it fails (red)
3. **Implement the code** - Make the test pass (green)
4. **Refactor** - Improve code quality
5. **Verify coverage** - Ensure >90% coverage

### Table-Driven Tests

Use table-driven tests for testing multiple scenarios:

```go
func TestValidateConfig(t *testing.T) {
    tests := []struct {
        name    string
        config  *Config
        wantErr bool
        errMsg  string
    }{
        {
            name: "valid config",
            config: &Config{
                RPC: RPCConfig{Endpoint: "http://localhost:8545"},
            },
            wantErr: false,
        },
        {
            name: "missing endpoint",
            config: &Config{
                RPC: RPCConfig{Endpoint: ""},
            },
            wantErr: true,
            errMsg:  "endpoint is required",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.config.Validate()
            if (err != nil) != tt.wantErr {
                t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if tt.wantErr && err.Error() != tt.errMsg {
                t.Errorf("Validate() error = %q, want %q", err.Error(), tt.errMsg)
            }
        })
    }
}
```

### Mock Objects

Use interfaces and mock implementations for testing:

```go
// Interface for testing
type Client interface {
    GetBlock(ctx context.Context, height uint64) (*Block, error)
}

// Mock implementation
type mockClient struct {
    blocks map[uint64]*Block
    err    error
}

func (m *mockClient) GetBlock(ctx context.Context, height uint64) (*Block, error) {
    if m.err != nil {
        return nil, m.err
    }
    return m.blocks[height], nil
}
```

## Test Utilities

The `internal/testutil` package provides common test utilities:

### Test Fixtures

```go
import "github.com/wemix-blockchain/indexer-go/internal/testutil"

// Create test block
block := testutil.NewTestBlock(height)

// Create block with transactions
block := testutil.NewTestBlockWithTransactions(height, txCount)

// Create test receipt
receipt := testutil.NewTestReceipt(txHash, blockNumber, status)

// Create test logger
logger := testutil.NewTestLogger(t)
```

### Assertions

```go
// Assert no error
testutil.AssertNoError(t, err, "Failed to create client")

// Assert error
testutil.AssertError(t, err, "Should fail with invalid config")

// Assert equality
testutil.AssertEqual(t, expected, actual, "Block height mismatch")

// Assert conditions
testutil.AssertTrue(t, condition, "Condition should be true")
testutil.AssertFalse(t, condition, "Condition should be false")

// Assert nil/not nil
testutil.AssertNil(t, value, "Value should be nil")
testutil.AssertNotNil(t, value, "Value should not be nil")
```

## Integration Tests

### Running Integration Tests

Integration tests require external dependencies (e.g., Ethereum node):

```bash
# Skip integration tests (default)
go test -short ./...

# Run integration tests
go test ./...

# Run only integration tests
go test -run Integration ./...
```

### Writing Integration Tests

```go
func TestClientIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }

    // Setup integration test environment
    endpoint := "http://localhost:8545"
    client, err := NewClient(endpoint)
    if err != nil {
        t.Skipf("Cannot connect to Ethereum node: %v", err)
    }
    defer client.Close()

    // Integration test code
    // ...
}
```

### Integration Test Requirements

- Must be skippable with `-short` flag
- Must check for required services before running
- Must clean up resources (defer cleanup)
- Must not affect other tests
- Must be idempotent

## Best Practices

### General Testing Principles

1. **Tests should be fast** - Unit tests should run in milliseconds
2. **Tests should be isolated** - No dependencies between tests
3. **Tests should be deterministic** - Same input = same output
4. **Tests should be readable** - Clear test names and assertions
5. **Tests should be maintainable** - Easy to update when code changes

### Code Coverage Best Practices

1. **Focus on critical paths** - Prioritize high-value code
2. **Test edge cases** - Boundary conditions, error paths
3. **Test error handling** - All error paths should be covered
4. **Don't test for coverage** - Test for correctness, coverage follows
5. **Review uncovered code** - Understand why code isn't covered

### Test Organization

1. **Arrange-Act-Assert** (AAA) pattern
   ```go
   func TestFunction(t *testing.T) {
       // Arrange - setup test data
       input := "test"

       // Act - execute function
       result := Function(input)

       // Assert - verify result
       if result != "expected" {
           t.Errorf("got %v, want %v", result, "expected")
       }
   }
   ```

2. **Use subtests** for related test cases
   ```go
   t.Run("subtest name", func(t *testing.T) {
       // subtest code
   })
   ```

3. **Use t.Helper()** for test helper functions
   ```go
   func assertNoError(t *testing.T, err error) {
       t.Helper()
       if err != nil {
           t.Fatalf("unexpected error: %v", err)
       }
   }
   ```

### Common Pitfalls

1. **Don't use global state** - Tests should not depend on global variables
2. **Don't test implementation details** - Test behavior, not internals
3. **Don't skip error checking** - Always check errors in tests
4. **Don't use real external services** - Use mocks or skip with `-short`
5. **Don't ignore flaky tests** - Fix or remove unstable tests

### Performance Testing

```go
func BenchmarkFunction(b *testing.B) {
    for i := 0; i < b.N; i++ {
        Function()
    }
}

// Run benchmarks
// go test -bench=. -benchmem ./...
```

### Race Detection

```bash
# Run tests with race detector
go test -race -short ./...

# Race detector is more thorough but slower
# Run regularly during development and in CI/CD
```

## Continuous Integration

### Pre-commit Checks

Before committing code:

```bash
# Run all unit tests
./scripts/test.sh

# Run with coverage
./scripts/test.sh -c

# Run with race detector
go test -race -short ./...

# Run linters
go vet ./...
go fmt ./...
```

### CI/CD Pipeline

The CI/CD pipeline should:

1. Run all unit tests with coverage
2. Run integration tests (if dependencies available)
3. Check coverage requirements (>90%)
4. Run race detector
5. Run linters and formatters
6. Generate coverage reports

## References

- [Go Testing Package](https://pkg.go.dev/testing)
- [Table-Driven Tests in Go](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests)
- [Testing Best Practices](https://golang.org/doc/effective_go#testing)
- [Go Test Coverage](https://go.dev/blog/cover)
