package consensus

import (
	"github.com/ethereum/go-ethereum/common"
)

// ValidatorStats represents aggregated statistics for a validator
type ValidatorStats struct {
	Address common.Address `json:"address"`

	// Block production
	TotalBlocks    uint64 `json:"totalBlocks"`    // Total blocks in period
	BlocksProposed uint64 `json:"blocksProposed"` // Blocks proposed by this validator

	// Participation metrics
	PreparesSigned  uint64 `json:"preparesSigned"`  // Prepare messages signed
	CommitsSigned   uint64 `json:"commitsSigned"`   // Commit messages signed
	PreparesMissed  uint64 `json:"preparesMissed"`  // Prepare messages missed
	CommitsMissed   uint64 `json:"commitsMissed"`   // Commit messages missed
	ParticipationRate float64 `json:"participationRate"` // Percentage (0-100)

	// Recent activity tracking
	LastProposedBlock  uint64 `json:"lastProposedBlock,omitempty"`
	LastCommittedBlock uint64 `json:"lastCommittedBlock,omitempty"`
	LastSeenBlock      uint64 `json:"lastSeenBlock,omitempty"`
}

// ValidatorParticipation provides detailed participation data for a validator over a range
type ValidatorParticipation struct {
	Address    common.Address `json:"address"`
	StartBlock uint64         `json:"startBlock"`
	EndBlock   uint64         `json:"endBlock"`

	// Aggregated statistics
	TotalBlocks      uint64  `json:"totalBlocks"`
	BlocksProposed   uint64  `json:"blocksProposed"`
	BlocksCommitted  uint64  `json:"blocksCommitted"`
	BlocksMissed     uint64  `json:"blocksMissed"`
	ParticipationRate float64 `json:"participationRate"` // Percentage

	// Per-block breakdown
	Blocks []BlockParticipation `json:"blocks"`
}

// ValidatorSet represents the active validator set at a specific point
type ValidatorSet struct {
	BlockNumber uint64           `json:"blockNumber"`
	EpochNumber uint64           `json:"epochNumber,omitempty"`
	Validators  []common.Address `json:"validators"`
	Count       int              `json:"count"`
}

// ValidatorActivity tracks real-time validator activity
type ValidatorActivity struct {
	Address     common.Address `json:"address"`
	IsActive    bool           `json:"isActive"`    // Currently in validator set
	IsOnline    bool           `json:"isOnline"`    // Recently participated
	LastSeen    uint64         `json:"lastSeen"`    // Block number last seen
	BlocksAgo   uint64         `json:"blocksAgo"`   // Blocks since last seen
	CurrentStreak uint64       `json:"currentStreak"` // Consecutive blocks participated
}

// CalculateParticipationRate calculates the participation rate for validator stats
func (vs *ValidatorStats) CalculateParticipationRate() {
	if vs.TotalBlocks == 0 {
		vs.ParticipationRate = 0.0
		return
	}
	vs.ParticipationRate = float64(vs.CommitsSigned) / float64(vs.TotalBlocks) * 100.0
}

// UpdateWithBlock updates validator stats based on a new block's consensus data
func (vs *ValidatorStats) UpdateWithBlock(data *ConsensusData, validatorAddr common.Address) {
	vs.TotalBlocks++

	// Check if this validator was the proposer
	if data.Proposer == validatorAddr {
		vs.BlocksProposed++
		vs.LastProposedBlock = data.BlockNumber
	}

	// Check prepare participation
	for _, signer := range data.PrepareSigners {
		if signer == validatorAddr {
			vs.PreparesSigned++
			break
		}
	}

	// Check commit participation
	committed := false
	for _, signer := range data.CommitSigners {
		if signer == validatorAddr {
			vs.CommitsSigned++
			vs.LastCommittedBlock = data.BlockNumber
			committed = true
			break
		}
	}

	// Check if missed
	for _, missed := range data.MissedPrepare {
		if missed == validatorAddr {
			vs.PreparesMissed++
		}
	}

	for _, missed := range data.MissedCommit {
		if missed == validatorAddr {
			vs.CommitsMissed++
		}
	}

	// Update last seen
	if committed || data.Proposer == validatorAddr {
		vs.LastSeenBlock = data.BlockNumber
	}

	// Recalculate participation rate
	vs.CalculateParticipationRate()
}

// IsValidator checks if an address is in the validator set
func (vset *ValidatorSet) IsValidator(addr common.Address) bool {
	for _, v := range vset.Validators {
		if v == addr {
			return true
		}
	}
	return false
}

// AddValidator adds a validator to the set if not already present
func (vset *ValidatorSet) AddValidator(addr common.Address) {
	if !vset.IsValidator(addr) {
		vset.Validators = append(vset.Validators, addr)
		vset.Count = len(vset.Validators)
	}
}

// RemoveValidator removes a validator from the set
func (vset *ValidatorSet) RemoveValidator(addr common.Address) {
	for i, v := range vset.Validators {
		if v == addr {
			vset.Validators = append(vset.Validators[:i], vset.Validators[i+1:]...)
			vset.Count = len(vset.Validators)
			return
		}
	}
}

// ValidatorChange represents a change in validator set at epoch boundary
type ValidatorChange struct {
	EpochNumber        uint64           `json:"epochNumber"`
	BlockNumber        uint64           `json:"blockNumber"`
	PreviousValidators []common.Address `json:"previousValidators"`
	NewValidators      []common.Address `json:"newValidators"`
	AddedValidators    []common.Address `json:"addedValidators"`
	RemovedValidators  []common.Address `json:"removedValidators"`
}

// CalculateChanges computes the added and removed validators
func (vc *ValidatorChange) CalculateChanges() {
	prevSet := make(map[common.Address]bool)
	for _, addr := range vc.PreviousValidators {
		prevSet[addr] = true
	}

	newSet := make(map[common.Address]bool)
	for _, addr := range vc.NewValidators {
		newSet[addr] = true
	}

	// Find added validators (in new but not in prev)
	vc.AddedValidators = make([]common.Address, 0)
	for _, addr := range vc.NewValidators {
		if !prevSet[addr] {
			vc.AddedValidators = append(vc.AddedValidators, addr)
		}
	}

	// Find removed validators (in prev but not in new)
	vc.RemovedValidators = make([]common.Address, 0)
	for _, addr := range vc.PreviousValidators {
		if !newSet[addr] {
			vc.RemovedValidators = append(vc.RemovedValidators, addr)
		}
	}
}

// MinerStats represents block production statistics (similar to existing ValidatorStats in storage)
// This bridges the gap between existing miner tracking and new consensus tracking
type MinerStats struct {
	Address     common.Address `json:"address"`
	BlockCount  uint64         `json:"blockCount"`
	StartBlock  uint64         `json:"startBlock"`
	EndBlock    uint64         `json:"endBlock"`
	Percentage  float64        `json:"percentage"`
	IsValidator bool           `json:"isValidator"` // Whether this miner is in validator set
}
