// Package anvil provides E2E test helpers for running tests against a local Anvil instance.
// It manages Anvil process lifecycle and provides utilities for test setup/teardown.
package anvil

import (
	"context"
	"fmt"
	"math/big"
	"os/exec"
	"time"

	"github.com/0xmhha/indexer-go/adapters/factory"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
)

const (
	// DefaultPort is the default Anvil RPC port
	DefaultPort = 8545

	// DefaultChainID is the default Anvil chain ID
	DefaultChainID = 31337

	// StartupTimeout is how long to wait for Anvil to start
	StartupTimeout = 10 * time.Second

	// DefaultBlockTime is the default block time in seconds (0 = automine)
	DefaultBlockTime = 0
)

// TestConfig holds configuration for an Anvil test instance
type TestConfig struct {
	// Port is the RPC port to use
	Port int

	// ChainID is the chain ID to use
	ChainID uint64

	// BlockTime is the block mining interval (0 = automine)
	BlockTime int

	// Accounts is the number of accounts to create
	Accounts int

	// Balance is the initial balance of each account (in ETH)
	Balance int

	// Mnemonic is the mnemonic to use for account generation
	Mnemonic string

	// ForkURL is the URL to fork from (optional)
	ForkURL string

	// ForkBlockNumber is the block number to fork from
	ForkBlockNumber uint64
}

// DefaultTestConfig returns a default test configuration
func DefaultTestConfig() *TestConfig {
	return &TestConfig{
		Port:      DefaultPort,
		ChainID:   DefaultChainID,
		BlockTime: DefaultBlockTime,
		Accounts:  10,
		Balance:   10000, // 10000 ETH
	}
}

// TestInstance represents a running Anvil test instance
type TestInstance struct {
	Config    *TestConfig
	cmd       *exec.Cmd
	rpcURL    string
	rpcClient *rpc.Client
	ethClient *ethclient.Client
	logger    *zap.Logger
}

// NewTestInstance creates a new Anvil test instance with the given config
func NewTestInstance(config *TestConfig, logger *zap.Logger) *TestInstance {
	if config == nil {
		config = DefaultTestConfig()
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	return &TestInstance{
		Config: config,
		rpcURL: fmt.Sprintf("http://localhost:%d", config.Port),
		logger: logger,
	}
}

// Start starts the Anvil process
func (t *TestInstance) Start(ctx context.Context) error {
	args := []string{
		"--port", fmt.Sprintf("%d", t.Config.Port),
		"--chain-id", fmt.Sprintf("%d", t.Config.ChainID),
		"--accounts", fmt.Sprintf("%d", t.Config.Accounts),
		"--balance", fmt.Sprintf("%d", t.Config.Balance),
	}

	if t.Config.BlockTime > 0 {
		args = append(args, "--block-time", fmt.Sprintf("%d", t.Config.BlockTime))
	}

	if t.Config.Mnemonic != "" {
		args = append(args, "--mnemonic", t.Config.Mnemonic)
	}

	if t.Config.ForkURL != "" {
		args = append(args, "--fork-url", t.Config.ForkURL)
		if t.Config.ForkBlockNumber > 0 {
			args = append(args, "--fork-block-number", fmt.Sprintf("%d", t.Config.ForkBlockNumber))
		}
	}

	t.cmd = exec.CommandContext(ctx, "anvil", args...)
	t.logger.Info("Starting Anvil", zap.Strings("args", args))

	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start anvil: %w", err)
	}

	// Wait for Anvil to be ready
	if err := t.waitForReady(ctx); err != nil {
		t.Stop()
		return fmt.Errorf("anvil failed to start: %w", err)
	}

	t.logger.Info("Anvil started successfully",
		zap.String("url", t.rpcURL),
		zap.Uint64("chainId", t.Config.ChainID),
	)

	return nil
}

// waitForReady waits for Anvil to accept connections
func (t *TestInstance) waitForReady(ctx context.Context) error {
	deadline := time.Now().Add(StartupTimeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		client, err := rpc.DialContext(ctx, t.rpcURL)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Try to get the chain ID to verify connection
		var chainID string
		err = client.CallContext(ctx, &chainID, "eth_chainId")
		if err != nil {
			client.Close()
			time.Sleep(100 * time.Millisecond)
			continue
		}

		t.rpcClient = client
		t.ethClient = ethclient.NewClient(client)
		return nil
	}

	return fmt.Errorf("timeout waiting for anvil to start")
}

// Stop stops the Anvil process
func (t *TestInstance) Stop() {
	if t.ethClient != nil {
		t.ethClient.Close()
		t.ethClient = nil
	}

	if t.rpcClient != nil {
		t.rpcClient.Close()
		t.rpcClient = nil
	}

	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
		t.cmd.Wait()
		t.cmd = nil
	}

	t.logger.Info("Anvil stopped")
}

// RPCURL returns the RPC URL for the Anvil instance
func (t *TestInstance) RPCURL() string {
	return t.rpcURL
}

// RPCClient returns the RPC client
func (t *TestInstance) RPCClient() *rpc.Client {
	return t.rpcClient
}

// EthClient returns the ethclient
func (t *TestInstance) EthClient() *ethclient.Client {
	return t.ethClient
}

// CreateAdapter creates an adapter using the factory
func (t *TestInstance) CreateAdapter(ctx context.Context) (*factory.CreateResult, error) {
	return factory.CreateAdapter(ctx, t.rpcURL, t.logger)
}

// =============================================================================
// Test Helper Methods
// =============================================================================

// MineBlocks mines the specified number of blocks
func (t *TestInstance) MineBlocks(ctx context.Context, count uint64) error {
	return t.rpcClient.CallContext(ctx, nil, "anvil_mine", count)
}

// SetAutomine enables or disables automine
func (t *TestInstance) SetAutomine(ctx context.Context, enabled bool) error {
	return t.rpcClient.CallContext(ctx, nil, "evm_setAutomine", enabled)
}

// Snapshot creates a blockchain snapshot
func (t *TestInstance) Snapshot(ctx context.Context) (string, error) {
	var snapshotID string
	err := t.rpcClient.CallContext(ctx, &snapshotID, "evm_snapshot")
	return snapshotID, err
}

// Revert reverts to a previous snapshot
func (t *TestInstance) Revert(ctx context.Context, snapshotID string) error {
	var result bool
	err := t.rpcClient.CallContext(ctx, &result, "evm_revert", snapshotID)
	return err
}

// SetBalance sets the balance of an account
func (t *TestInstance) SetBalance(ctx context.Context, address common.Address, balance *big.Int) error {
	balanceHex := "0x" + balance.Text(16)
	return t.rpcClient.CallContext(ctx, nil, "anvil_setBalance", address.Hex(), balanceHex)
}

// GetLatestBlockNumber returns the latest block number
func (t *TestInstance) GetLatestBlockNumber(ctx context.Context) (uint64, error) {
	return t.ethClient.BlockNumber(ctx)
}

// GetBlockByNumber retrieves a block by its number
func (t *TestInstance) GetBlockByNumber(ctx context.Context, number uint64) (interface{}, error) {
	return t.ethClient.BlockByNumber(ctx, big.NewInt(int64(number)))
}

// GetBlockHashByNumber retrieves a block hash by its number using raw RPC
// (avoids EIP-4844 parsing issues with go-ethereum's ethclient)
func (t *TestInstance) GetBlockHashByNumber(ctx context.Context, number *big.Int) (common.Hash, error) {
	var blockNumArg string
	if number == nil {
		blockNumArg = "latest"
	} else {
		blockNumArg = "0x" + number.Text(16)
	}

	var result struct {
		Hash common.Hash `json:"hash"`
	}
	err := t.rpcClient.CallContext(ctx, &result, "eth_getBlockByNumber", blockNumArg, false)
	if err != nil {
		return common.Hash{}, err
	}
	return result.Hash, nil
}

// IncreaseTime increases the blockchain time by the specified seconds
func (t *TestInstance) IncreaseTime(ctx context.Context, seconds uint64) error {
	return t.rpcClient.CallContext(ctx, nil, "evm_increaseTime", seconds)
}

// SetNextBlockTimestamp sets the timestamp of the next block
func (t *TestInstance) SetNextBlockTimestamp(ctx context.Context, timestamp uint64) error {
	return t.rpcClient.CallContext(ctx, nil, "evm_setNextBlockTimestamp", timestamp)
}

// ImpersonateAccount starts impersonating an account
func (t *TestInstance) ImpersonateAccount(ctx context.Context, address common.Address) error {
	return t.rpcClient.CallContext(ctx, nil, "anvil_impersonateAccount", address.Hex())
}

// StopImpersonatingAccount stops impersonating an account
func (t *TestInstance) StopImpersonatingAccount(ctx context.Context, address common.Address) error {
	return t.rpcClient.CallContext(ctx, nil, "anvil_stopImpersonatingAccount", address.Hex())
}

// Reset resets the blockchain state
func (t *TestInstance) Reset(ctx context.Context) error {
	return t.rpcClient.CallContext(ctx, nil, "anvil_reset", map[string]interface{}{})
}

// GetAccounts returns the list of test accounts
func (t *TestInstance) GetAccounts(ctx context.Context) ([]common.Address, error) {
	var accounts []common.Address
	err := t.rpcClient.CallContext(ctx, &accounts, "eth_accounts")
	return accounts, err
}

// =============================================================================
// Convenience Functions
// =============================================================================

// MustStart starts Anvil and panics on error
func (t *TestInstance) MustStart(ctx context.Context) {
	if err := t.Start(ctx); err != nil {
		panic(fmt.Sprintf("failed to start anvil: %v", err))
	}
}

// IsAnvilInstalled checks if Anvil is installed
func IsAnvilInstalled() bool {
	_, err := exec.LookPath("anvil")
	return err == nil
}

// SkipIfNoAnvil skips the test if Anvil is not installed
func SkipIfNoAnvil(skip func(args ...interface{})) {
	if !IsAnvilInstalled() {
		skip("anvil not installed, skipping E2E test")
	}
}
