package constants

import "time"

// ChainType represents the type of blockchain
type ChainType string

const (
	// ChainTypeEVM represents EVM-compatible chains
	ChainTypeEVM ChainType = "evm"
	// ChainTypeCosmos represents Cosmos SDK-based chains
	ChainTypeCosmos ChainType = "cosmos"
)

// ConsensusType represents the consensus mechanism
type ConsensusType string

const (
	// ConsensusTypeWBFT represents WBFT (Weighted Byzantine Fault Tolerance) consensus
	ConsensusTypeWBFT ConsensusType = "wbft"
	// ConsensusTypePoA represents Proof of Authority consensus
	ConsensusTypePoA ConsensusType = "poa"
	// ConsensusTypePoS represents Proof of Stake consensus
	ConsensusTypePoS ConsensusType = "pos"
	// ConsensusTypeTendermint represents Tendermint consensus
	ConsensusTypeTendermint ConsensusType = "tendermint"
)

// ChainConfig holds chain-specific configuration
// This structure is designed to be extensible for different chain types
type ChainConfig struct {
	// Basic chain info
	ChainType     ChainType     `yaml:"chain_type"`
	ConsensusType ConsensusType `yaml:"consensus_type"`

	// Consensus parameters
	Consensus ConsensusConfig `yaml:"consensus"`

	// Native token configuration
	NativeToken TokenConfig `yaml:"native_token"`
}

// ConsensusConfig holds consensus-specific parameters
type ConsensusConfig struct {
	// EpochLength is the number of blocks per epoch
	EpochLength uint64 `yaml:"epoch_length"`

	// QuorumNumerator and QuorumDenominator define the quorum threshold
	// e.g., 2/3 majority = numerator:2, denominator:3
	QuorumNumerator   uint64 `yaml:"quorum_numerator"`
	QuorumDenominator uint64 `yaml:"quorum_denominator"`

	// BlockTime is the expected block production interval
	BlockTime time.Duration `yaml:"block_time"`

	// ConfirmationBlocks is the number of blocks to consider a transaction final
	ConfirmationBlocks uint64 `yaml:"confirmation_blocks"`
}

// TokenConfig holds native token configuration
type TokenConfig struct {
	Name     string `yaml:"name"`
	Symbol   string `yaml:"symbol"`
	Decimals int    `yaml:"decimals"`
}

// DefaultChainConfig returns default configuration for StableOne chain
func DefaultChainConfig() *ChainConfig {
	return &ChainConfig{
		ChainType:     ChainTypeEVM,
		ConsensusType: ConsensusTypeWBFT,
		Consensus: ConsensusConfig{
			EpochLength:        DefaultEpochLength,
			QuorumNumerator:    DefaultQuorumNumerator,
			QuorumDenominator:  DefaultQuorumDenominator,
			BlockTime:          DefaultBlockTime,
			ConfirmationBlocks: DefaultConfirmationBlocks,
		},
		NativeToken: TokenConfig{
			Name:     DefaultNativeTokenName,
			Symbol:   DefaultNativeTokenSymbol,
			Decimals: DefaultNativeTokenDecimals,
		},
	}
}

// WBFT Consensus Constants (StableOne specific)
const (
	// DefaultEpochLength is the default number of blocks per epoch
	// This matches the default epoch length in go-stablenet/consensus/wbft/config.go
	DefaultEpochLength = 10

	// DefaultQuorumNumerator is the numerator for quorum calculation (2/3 majority)
	DefaultQuorumNumerator = 2

	// DefaultQuorumDenominator is the denominator for quorum calculation
	DefaultQuorumDenominator = 3

	// DefaultMinParticipationRate is the minimum participation rate for healthy consensus
	// Calculated as (QuorumNumerator/QuorumDenominator) * 100
	DefaultMinParticipationRate = 66.7

	// DefaultBlockTime is the typical block time (can vary by chain)
	DefaultBlockTime = 12 * time.Second

	// DefaultConfirmationBlocks is the default number of confirmations to consider a block final
	DefaultConfirmationBlocks = 12
)

// Native Token Constants (StableOne specific)
const (
	// DefaultNativeTokenName is the name of the native token
	DefaultNativeTokenName = "WKRC"

	// DefaultNativeTokenSymbol is the symbol of the native token
	DefaultNativeTokenSymbol = "WKRC"

	// DefaultNativeTokenDecimals is the decimal places of the native token
	DefaultNativeTokenDecimals = 18
)

// CalculateEpochNumber returns the epoch number for a given block number
func CalculateEpochNumber(blockNumber uint64, epochLength uint64) uint64 {
	if epochLength == 0 {
		epochLength = DefaultEpochLength
	}
	return blockNumber / epochLength
}

// IsEpochBoundary returns true if the block is an epoch boundary
func IsEpochBoundary(blockNumber uint64, epochLength uint64) bool {
	if epochLength == 0 {
		epochLength = DefaultEpochLength
	}
	return blockNumber > 0 && blockNumber%epochLength == 0
}

// CalculateQuorum calculates the required quorum for a given validator count
func CalculateQuorum(validatorCount int, numerator, denominator uint64) int {
	if denominator == 0 {
		denominator = DefaultQuorumDenominator
	}
	if numerator == 0 {
		numerator = DefaultQuorumNumerator
	}
	// Formula: (validatorCount * numerator / denominator) + 1
	return int((uint64(validatorCount)*numerator)/denominator) + 1
}

// HasQuorum checks if the given count meets the quorum requirement
func HasQuorum(count, total int, numerator, denominator uint64) bool {
	required := CalculateQuorum(total, numerator, denominator)
	return count >= required
}
