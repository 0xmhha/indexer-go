// Package detector provides automatic node detection for EVM chains.
// It uses web3_clientVersion and other heuristics to identify the
// connected node type (Anvil, Geth, StableOne, etc.)
package detector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
)

// NodeType represents the type of blockchain node
type NodeType string

const (
	// NodeTypeAnvil represents Foundry's Anvil local testnet
	NodeTypeAnvil NodeType = "anvil"

	// NodeTypeGeth represents Go-Ethereum node
	NodeTypeGeth NodeType = "geth"

	// NodeTypeStableOne represents StableOne node (WBFT consensus)
	NodeTypeStableOne NodeType = "stableone"

	// NodeTypeHardhat represents Hardhat Network
	NodeTypeHardhat NodeType = "hardhat"

	// NodeTypeGanache represents Ganache local testnet
	NodeTypeGanache NodeType = "ganache"

	// NodeTypeUnknown represents an unknown EVM node
	NodeTypeUnknown NodeType = "unknown"
)

// NodeInfo contains detected node information
type NodeInfo struct {
	// Type is the detected node type
	Type NodeType

	// ClientVersion is the raw client version string
	ClientVersion string

	// ChainID is the chain ID from eth_chainId
	ChainID uint64

	// IsLocal indicates if this appears to be a local/dev network
	IsLocal bool

	// SupportsPendingTx indicates if the node supports pending tx subscription
	SupportsPendingTx bool

	// SupportsDebug indicates if debug namespace is available
	SupportsDebug bool

	// SupportsAnvilMethods indicates if Anvil-specific methods are available
	SupportsAnvilMethods bool
}

// Detector provides automatic node type detection
type Detector struct {
	rpcClient *rpc.Client
	logger    *zap.Logger
	timeout   time.Duration
}

// NewDetector creates a new node detector
func NewDetector(rpcURL string, logger *zap.Logger) (*Detector, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := rpc.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	return &Detector{
		rpcClient: client,
		logger:    logger,
		timeout:   5 * time.Second,
	}, nil
}

// NewDetectorWithClient creates a detector with an existing RPC client
func NewDetectorWithClient(client *rpc.Client, logger *zap.Logger) *Detector {
	return &Detector{
		rpcClient: client,
		logger:    logger,
		timeout:   5 * time.Second,
	}
}

// Detect performs node detection and returns NodeInfo
func (d *Detector) Detect(ctx context.Context) (*NodeInfo, error) {
	info := &NodeInfo{
		Type: NodeTypeUnknown,
	}

	// Get client version
	clientVersion, err := d.getClientVersion(ctx)
	if err != nil {
		d.logger.Warn("Failed to get client version", zap.Error(err))
	} else {
		info.ClientVersion = clientVersion
		info.Type = d.parseClientVersion(clientVersion)
	}

	// Get chain ID
	chainID, err := d.getChainID(ctx)
	if err != nil {
		d.logger.Warn("Failed to get chain ID", zap.Error(err))
	} else {
		info.ChainID = chainID
		// Local dev networks typically use chain IDs like 31337 (Hardhat/Anvil default)
		// or 1337 (Ganache default)
		info.IsLocal = isLocalChainID(chainID)
	}

	// Check for Anvil-specific methods if type is still unknown or could be Anvil
	if info.Type == NodeTypeUnknown || info.Type == NodeTypeAnvil {
		if d.supportsAnvilMethods(ctx) {
			info.Type = NodeTypeAnvil
			info.SupportsAnvilMethods = true
		}
	}

	// Check for pending transaction support
	info.SupportsPendingTx = d.supportsPendingTx(ctx)

	// Check for debug namespace
	info.SupportsDebug = d.supportsDebug(ctx)

	d.logger.Info("Node detection complete",
		zap.String("type", string(info.Type)),
		zap.String("client_version", info.ClientVersion),
		zap.Uint64("chain_id", info.ChainID),
		zap.Bool("is_local", info.IsLocal),
		zap.Bool("anvil_methods", info.SupportsAnvilMethods),
	)

	return info, nil
}

// getClientVersion calls web3_clientVersion
func (d *Detector) getClientVersion(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	var result string
	err := d.rpcClient.CallContext(ctx, &result, "web3_clientVersion")
	if err != nil {
		return "", err
	}
	return result, nil
}

// getChainID calls eth_chainId
func (d *Detector) getChainID(ctx context.Context) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	var result string
	err := d.rpcClient.CallContext(ctx, &result, "eth_chainId")
	if err != nil {
		return 0, err
	}

	// Parse hex string
	var chainID uint64
	_, err = fmt.Sscanf(result, "0x%x", &chainID)
	if err != nil {
		return 0, fmt.Errorf("failed to parse chain ID: %w", err)
	}
	return chainID, nil
}

// parseClientVersion extracts node type from client version string
func (d *Detector) parseClientVersion(version string) NodeType {
	lowerVersion := strings.ToLower(version)

	// Anvil: "anvil/v0.2.0", "anvil/0.2.0"
	if strings.Contains(lowerVersion, "anvil") {
		return NodeTypeAnvil
	}

	// Hardhat: "HardhatNetwork/2.0.0"
	if strings.Contains(lowerVersion, "hardhat") {
		return NodeTypeHardhat
	}

	// Ganache: "Ganache/v7.0.0", "EthereumJS TestRPC"
	if strings.Contains(lowerVersion, "ganache") || strings.Contains(lowerVersion, "testrpc") {
		return NodeTypeGanache
	}

	// StableOne: "stableone/v1.0.0", "go-stablenet"
	if strings.Contains(lowerVersion, "stableone") || strings.Contains(lowerVersion, "stablenet") {
		return NodeTypeStableOne
	}

	// Geth: "Geth/v1.10.0-stable", "go-ethereum"
	if strings.Contains(lowerVersion, "geth") || strings.Contains(lowerVersion, "go-ethereum") {
		return NodeTypeGeth
	}

	return NodeTypeUnknown
}

// supportsAnvilMethods checks if Anvil-specific RPC methods are available
func (d *Detector) supportsAnvilMethods(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	// Try to call anvil_nodeInfo - this is Anvil-specific
	var result interface{}
	err := d.rpcClient.CallContext(ctx, &result, "anvil_nodeInfo")
	if err == nil {
		return true
	}

	// Alternative: try anvil_autoImpersonateAccount (check method exists)
	// This RPC exists in Anvil but may return error if not enabled
	err = d.rpcClient.CallContext(ctx, &result, "anvil_getAutomine")
	return err == nil
}

// supportsPendingTx checks if pending transaction subscription is supported
func (d *Detector) supportsPendingTx(ctx context.Context) bool {
	// This is a heuristic - most modern nodes support this
	// We could try to subscribe and immediately unsubscribe
	// For now, assume true for known local dev nodes
	return true
}

// supportsDebug checks if debug namespace is available
func (d *Detector) supportsDebug(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	// Try to call debug_traceBlockByNumber with block 0
	var result interface{}
	err := d.rpcClient.CallContext(ctx, &result, "debug_traceBlockByNumber", "0x0")
	// If method doesn't exist, we get "method not found" error
	// If it exists but fails (e.g., no block), that's still a "supported" indicator
	if err != nil {
		errStr := err.Error()
		// "method not found" or similar indicates no debug support
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "not supported") {
			return false
		}
	}
	return true
}

// isLocalChainID checks if a chain ID is commonly used for local development
func isLocalChainID(chainID uint64) bool {
	localChainIDs := map[uint64]bool{
		1337:  true, // Ganache default
		31337: true, // Hardhat/Anvil default
		1338:  true, // Alternative dev chain
		9999:  true, // Common dev chain
		1234:  true, // Common dev chain
	}
	return localChainIDs[chainID]
}

// Close releases resources
func (d *Detector) Close() {
	if d.rpcClient != nil {
		d.rpcClient.Close()
	}
}

// DetectFromRPCURL is a convenience function to detect node type from URL
func DetectFromRPCURL(ctx context.Context, rpcURL string, logger *zap.Logger) (*NodeInfo, error) {
	detector, err := NewDetector(rpcURL, logger)
	if err != nil {
		return nil, err
	}
	defer detector.Close()

	return detector.Detect(ctx)
}
