// Package multichain provides multi-chain management capabilities for the indexer.
// It enables a single indexer instance to connect to and index multiple blockchain networks.
package multichain

import (
	"time"
)

// ChainStatus represents the current operational state of a chain.
type ChainStatus string

const (
	// StatusRegistered indicates the chain has been registered but not started.
	StatusRegistered ChainStatus = "registered"
	// StatusStarting indicates the chain is initializing connections.
	StatusStarting ChainStatus = "starting"
	// StatusSyncing indicates the chain is catching up with the network.
	StatusSyncing ChainStatus = "syncing"
	// StatusActive indicates the chain is fully synced and indexing new blocks.
	StatusActive ChainStatus = "active"
	// StatusStopping indicates the chain is gracefully shutting down.
	StatusStopping ChainStatus = "stopping"
	// StatusStopped indicates the chain has been stopped.
	StatusStopped ChainStatus = "stopped"
	// StatusError indicates the chain encountered an unrecoverable error.
	StatusError ChainStatus = "error"
)

// HealthStatus represents the health state of a chain connection.
type HealthStatus struct {
	ChainID       string        `json:"chainId"`
	Status        ChainStatus   `json:"status"`
	IsHealthy     bool          `json:"isHealthy"`
	LatestHeight  uint64        `json:"latestHeight"`
	IndexedHeight uint64        `json:"indexedHeight"`
	SyncLag       uint64        `json:"syncLag"`
	LastBlockTime time.Time     `json:"lastBlockTime"`
	LastError     string        `json:"lastError,omitempty"`
	LastErrorTime *time.Time    `json:"lastErrorTime,omitempty"`
	RPCLatency    time.Duration `json:"rpcLatency"`
	Uptime        time.Duration `json:"uptime"`
	CheckedAt     time.Time     `json:"checkedAt"`
}

// SyncProgress represents the synchronization progress of a chain.
type SyncProgress struct {
	CurrentHeight          uint64        `json:"currentHeight"`
	LatestHeight           uint64        `json:"latestHeight"`
	StartingHeight         uint64        `json:"startingHeight"`
	PercentComplete        float64       `json:"percentComplete"`
	BlocksPerSecond        float64       `json:"blocksPerSecond"`
	EstimatedTimeRemaining time.Duration `json:"estimatedTimeRemaining"`
	SyncStartedAt          time.Time     `json:"syncStartedAt"`
}

// ChainInfo contains read-only information about a registered chain.
type ChainInfo struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	ChainID     uint64      `json:"chainId"`
	RPCEndpoint string      `json:"rpcEndpoint"`
	WSEndpoint  string      `json:"wsEndpoint,omitempty"`
	AdapterType string      `json:"adapterType"`
	Status      ChainStatus `json:"status"`
	StartHeight uint64      `json:"startHeight"`
	CreatedAt   time.Time   `json:"createdAt"`
	StartedAt   *time.Time  `json:"startedAt,omitempty"`
}

// ChainMetrics contains operational metrics for a chain.
type ChainMetrics struct {
	ChainID             string        `json:"chainId"`
	BlocksIndexed       uint64        `json:"blocksIndexed"`
	TransactionsIndexed uint64        `json:"transactionsIndexed"`
	LogsIndexed         uint64        `json:"logsIndexed"`
	RPCCalls            uint64        `json:"rpcCalls"`
	RPCErrors           uint64        `json:"rpcErrors"`
	AverageBlockTime    time.Duration `json:"averageBlockTime"`
	AverageRPCLatency   time.Duration `json:"averageRpcLatency"`
	DiskUsage           uint64        `json:"diskUsage"`
}
