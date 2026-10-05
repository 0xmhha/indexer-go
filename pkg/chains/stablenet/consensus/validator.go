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
	PreparesSigned    uint64  `json:"preparesSigned"`    // Prepare messages signed
	CommitsSigned     uint64  `json:"commitsSigned"`     // Commit messages signed
	PreparesMissed    uint64  `json:"preparesMissed"`    // Prepare messages missed
	CommitsMissed     uint64  `json:"commitsMissed"`     // Commit messages missed
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
	TotalBlocks       uint64  `json:"totalBlocks"`
	BlocksProposed    uint64  `json:"blocksProposed"`
	BlocksCommitted   uint64  `json:"blocksCommitted"`
	BlocksMissed      uint64  `json:"blocksMissed"`
	ParticipationRate float64 `json:"participationRate"` // Percentage

	// Per-block breakdown
	Blocks []BlockParticipation `json:"blocks"`
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
