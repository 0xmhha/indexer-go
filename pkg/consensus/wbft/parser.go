// Package wbft provides a consensus parser for WBFT (Weighted Byzantine Fault Tolerance).
// WBFT is a BFT-based consensus mechanism used by StableOne and similar chains.
package wbft

import (
	"bytes"
	"context"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/types/chain"
	consensustypes "github.com/0xmhha/indexer-go/pkg/types/consensus"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"go.uber.org/zap"
)

// Ensure Parser implements chain.ConsensusParser
var _ chain.ConsensusParser = (*Parser)(nil)

// Parser implements chain.ConsensusParser for WBFT consensus
type Parser struct {
	epochLength uint64
	logger      *zap.Logger
	// Cache for validator set lookups
	validatorCache map[uint64][]common.Address
}

// NewParser creates a new WBFT consensus parser
func NewParser(epochLength uint64, logger *zap.Logger) *Parser {
	if epochLength == 0 {
		epochLength = constants.DefaultEpochLength
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Parser{
		epochLength:    epochLength,
		logger:         logger,
		validatorCache: make(map[uint64][]common.Address),
	}
}

// ConsensusType returns the type of consensus this parser handles
func (p *Parser) ConsensusType() chain.ConsensusType {
	return chain.ConsensusTypeWBFT
}

// ParseConsensusData extracts consensus information from a block
func (p *Parser) ParseConsensusData(block *types.Block) (*chain.ConsensusData, error) {
	if block == nil {
		return nil, fmt.Errorf("block is nil")
	}

	header := block.Header()

	// Parse WBFT extra data
	wbftExtra, err := p.ParseWBFTExtra(header)
	if err != nil {
		return nil, fmt.Errorf("failed to parse WBFT extra data: %w", err)
	}

	// Extract validators from committed seal
	validators, err := p.ExtractValidators(wbftExtra)
	if err != nil {
		p.logger.Warn("Failed to extract validators from WBFT extra",
			zap.Uint64("block_number", header.Number.Uint64()),
			zap.Error(err),
		)
		validators = []common.Address{}
	}

	// Cache validators for later lookup via GetValidators()
	if len(validators) > 0 {
		p.CacheValidators(header.Number.Uint64(), validators)
	}

	// Extract commit signers
	commitSigners, err := p.ExtractSignersFromSeal(wbftExtra.CommittedSeal, validators)
	if err != nil {
		p.logger.Warn("Failed to extract commit signers",
			zap.Uint64("block_number", header.Number.Uint64()),
			zap.Error(err),
		)
		commitSigners = []common.Address{}
	}

	// Build ConsensusData
	consensusData := &chain.ConsensusData{
		ConsensusType:    chain.ConsensusTypeWBFT,
		BlockNumber:      header.Number.Uint64(),
		BlockHash:        header.Hash(),
		ProposerAddress:  header.Coinbase,
		ValidatorCount:   len(validators),
		SignedValidators: commitSigners,
	}

	// Calculate participation rate
	if len(validators) > 0 {
		consensusData.ParticipationRate = float64(len(commitSigners)) / float64(len(validators)) * 100.0
	}

	// Check for epoch boundary
	isEpochBoundary := p.isEpochBoundaryBlock(header.Number.Uint64())
	consensusData.IsEpochBoundary = isEpochBoundary

	if isEpochBoundary {
		epochNum := constants.CalculateEpochNumber(header.Number.Uint64(), p.epochLength)
		consensusData.EpochNumber = &epochNum

		// Parse epoch validators if available
		if wbftExtra.EpochInfo != nil {
			epochValidators := p.extractEpochValidators(wbftExtra.EpochInfo)
			consensusData.EpochValidators = epochValidators
		}
	}

	// Store extra data for extended parsing
	consensusData.ExtraData = wbftExtra

	return consensusData, nil
}

// GetValidators returns the current validator set at a specific block
func (p *Parser) GetValidators(ctx context.Context, blockNumber uint64) ([]common.Address, error) {
	// Check exact block cache first
	if validators, ok := p.validatorCache[blockNumber]; ok {
		return validators, nil
	}

	// Find the nearest cached block (validator set is valid until next epoch boundary)
	var nearestBlock uint64
	var nearestValidators []common.Address
	for cachedBlock, validators := range p.validatorCache {
		// Find the highest cached block that is <= requested block
		if cachedBlock <= blockNumber && cachedBlock > nearestBlock {
			nearestBlock = cachedBlock
			nearestValidators = validators
		}
	}

	if nearestValidators != nil {
		return nearestValidators, nil
	}

	// No cached validators found - need to parse blocks first
	return nil, fmt.Errorf("no cached validators for block %d - call ParseConsensusData on earlier blocks first", blockNumber)
}

// IsEpochBoundary checks if the block is an epoch boundary
func (p *Parser) IsEpochBoundary(block *types.Block) bool {
	if block == nil {
		return false
	}
	return p.isEpochBoundaryBlock(block.NumberU64())
}

// GetEpochLength returns the configured epoch length
func (p *Parser) GetEpochLength() uint64 {
	return p.epochLength
}

// isEpochBoundaryBlock checks if a block number is an epoch boundary
func (p *Parser) isEpochBoundaryBlock(blockNumber uint64) bool {
	return constants.IsEpochBoundary(blockNumber, p.epochLength)
}

// ParseWBFTExtra parses the WBFT extra data from a block header
func (p *Parser) ParseWBFTExtra(header *types.Header) (*consensustypes.WBFTExtra, error) {
	if len(header.Extra) < 32 {
		return nil, fmt.Errorf("extra data too short: expected at least 32 bytes, got %d", len(header.Extra))
	}

	// WBFT extra data structure:
	// [0:32] - Vanity data (32 bytes)
	// [32:] - RLP encoded WBFT data

	extra := &consensustypes.WBFTExtra{
		VanityData: header.Extra[:32],
	}

	// If there's more data after vanity, decode it
	if len(header.Extra) > 32 {
		var wbftData struct {
			RandaoReveal      []byte
			PrevRound         uint32
			PrevPreparedSeal  *wbftAggregatedSealRLP
			PrevCommittedSeal *wbftAggregatedSealRLP
			Round             uint32
			PreparedSeal      *wbftAggregatedSealRLP
			CommittedSeal     *wbftAggregatedSealRLP
			GasTip            *big.Int
			EpochInfo         *epochInfoRLP
		}

		if err := rlp.DecodeBytes(header.Extra[32:], &wbftData); err != nil {
			// Try alternative RLP structures for different WBFT versions
			p.logger.Debug("Failed to decode full WBFT extra, trying simplified structure",
				zap.Uint64("block_number", header.Number.Uint64()),
				zap.Error(err),
			)
			return extra, nil // Return with just vanity data
		}

		extra.RandaoReveal = wbftData.RandaoReveal
		extra.PrevRound = wbftData.PrevRound
		extra.Round = wbftData.Round
		extra.GasTip = wbftData.GasTip

		// Convert seals
		if wbftData.PrevPreparedSeal != nil {
			extra.PrevPreparedSeal = &consensustypes.WBFTAggregatedSeal{
				Sealers:   wbftData.PrevPreparedSeal.Sealers,
				Signature: wbftData.PrevPreparedSeal.Signature,
			}
		}
		if wbftData.PrevCommittedSeal != nil {
			extra.PrevCommittedSeal = &consensustypes.WBFTAggregatedSeal{
				Sealers:   wbftData.PrevCommittedSeal.Sealers,
				Signature: wbftData.PrevCommittedSeal.Signature,
			}
		}
		if wbftData.PreparedSeal != nil {
			extra.PreparedSeal = &consensustypes.WBFTAggregatedSeal{
				Sealers:   wbftData.PreparedSeal.Sealers,
				Signature: wbftData.PreparedSeal.Signature,
			}
		}
		if wbftData.CommittedSeal != nil {
			extra.CommittedSeal = &consensustypes.WBFTAggregatedSeal{
				Sealers:   wbftData.CommittedSeal.Sealers,
				Signature: wbftData.CommittedSeal.Signature,
			}
		}

		// Convert epoch info
		if wbftData.EpochInfo != nil {
			extra.EpochInfo = &consensustypes.EpochInfoRaw{
				Validators:    wbftData.EpochInfo.Validators,
				BLSPublicKeys: wbftData.EpochInfo.BLSPublicKeys,
			}
			// Convert candidates
			if len(wbftData.EpochInfo.Candidates) > 0 {
				extra.EpochInfo.Candidates = make([]*consensustypes.CandidateRaw, len(wbftData.EpochInfo.Candidates))
				for i, c := range wbftData.EpochInfo.Candidates {
					extra.EpochInfo.Candidates[i] = &consensustypes.CandidateRaw{
						Address:   c.Address,
						Diligence: c.Diligence,
					}
				}
			}
		}
	}

	return extra, nil
}

// RLP helper structures for decoding
type wbftAggregatedSealRLP struct {
	Sealers   []byte
	Signature []byte
}

type candidateRLP struct {
	Address   common.Address
	Diligence uint64
}

type epochInfoRLP struct {
	Candidates    []*candidateRLP
	Validators    []uint32
	BLSPublicKeys [][]byte
}

// ExtractValidators extracts validator addresses from the WBFT extra data
func (p *Parser) ExtractValidators(extra *consensustypes.WBFTExtra) ([]common.Address, error) {
	if extra == nil || extra.EpochInfo == nil {
		return nil, nil
	}

	// If we have epoch info with candidates, use that
	if len(extra.EpochInfo.Candidates) > 0 && len(extra.EpochInfo.Validators) > 0 {
		validators := make([]common.Address, 0, len(extra.EpochInfo.Validators))
		for _, idx := range extra.EpochInfo.Validators {
			if int(idx) < len(extra.EpochInfo.Candidates) {
				validators = append(validators, extra.EpochInfo.Candidates[idx].Address)
			}
		}
		return validators, nil
	}

	return nil, nil
}

// ExtractSignersFromSeal extracts signer addresses from an aggregated seal
func (p *Parser) ExtractSignersFromSeal(seal *consensustypes.WBFTAggregatedSeal, validators []common.Address) ([]common.Address, error) {
	if seal == nil || len(seal.Sealers) == 0 {
		return nil, nil
	}

	if len(validators) == 0 {
		return nil, nil
	}

	// The sealers field is a bitmap indicating which validators signed
	signers := make([]common.Address, 0)
	for i, validator := range validators {
		byteIndex := i / 8
		bitIndex := uint(i % 8)

		if byteIndex < len(seal.Sealers) {
			if seal.Sealers[byteIndex]&(1<<bitIndex) != 0 {
				signers = append(signers, validator)
			}
		}
	}

	return signers, nil
}

// extractEpochValidators extracts validator addresses from epoch info
func (p *Parser) extractEpochValidators(epochInfo *consensustypes.EpochInfoRaw) []common.Address {
	if epochInfo == nil {
		return nil
	}

	validators := make([]common.Address, 0)

	if len(epochInfo.Candidates) > 0 && len(epochInfo.Validators) > 0 {
		for _, idx := range epochInfo.Validators {
			if int(idx) < len(epochInfo.Candidates) {
				validators = append(validators, epochInfo.Candidates[idx].Address)
			}
		}
	}

	return validators
}

// GetEpochInfo returns detailed epoch information for a block
func (p *Parser) GetEpochInfo(block *types.Block) (*consensustypes.EpochData, error) {
	if block == nil {
		return nil, fmt.Errorf("block is nil")
	}

	if !p.IsEpochBoundary(block) {
		return nil, nil
	}

	wbftExtra, err := p.ParseWBFTExtra(block.Header())
	if err != nil {
		return nil, fmt.Errorf("failed to parse WBFT extra: %w", err)
	}

	if wbftExtra.EpochInfo == nil {
		return nil, nil
	}

	epochData := &consensustypes.EpochData{
		EpochNumber:    constants.CalculateEpochNumber(block.NumberU64(), p.epochLength),
		CandidateCount: len(wbftExtra.EpochInfo.Candidates),
	}

	// Build validator list
	if len(wbftExtra.EpochInfo.Candidates) > 0 {
		epochData.ValidatorCount = len(wbftExtra.EpochInfo.Validators)
		epochData.Validators = make([]consensustypes.ValidatorInfo, 0, epochData.ValidatorCount)

		for i, idx := range wbftExtra.EpochInfo.Validators {
			if int(idx) < len(wbftExtra.EpochInfo.Candidates) {
				candidate := wbftExtra.EpochInfo.Candidates[idx]
				validatorInfo := consensustypes.ValidatorInfo{
					Address: candidate.Address,
					Index:   uint32(i),
				}
				if i < len(wbftExtra.EpochInfo.BLSPublicKeys) {
					validatorInfo.BLSPubKey = wbftExtra.EpochInfo.BLSPublicKeys[i]
				}
				epochData.Validators = append(epochData.Validators, validatorInfo)
			}
		}

		// Build candidates list
		epochData.Candidates = make([]consensustypes.CandidateInfo, 0, len(wbftExtra.EpochInfo.Candidates))
		for _, c := range wbftExtra.EpochInfo.Candidates {
			epochData.Candidates = append(epochData.Candidates, consensustypes.CandidateInfo{
				Address:   c.Address,
				Diligence: c.Diligence,
			})
		}
	}

	return epochData, nil
}

// GetRoundInfo returns round information for a block
func (p *Parser) GetRoundInfo(block *types.Block) (*consensustypes.RoundInfo, error) {
	if block == nil {
		return nil, fmt.Errorf("block is nil")
	}

	wbftExtra, err := p.ParseWBFTExtra(block.Header())
	if err != nil {
		return nil, fmt.Errorf("failed to parse WBFT extra: %w", err)
	}

	return &consensustypes.RoundInfo{
		BlockNumber:       block.NumberU64(),
		FinalRound:        wbftExtra.Round,
		TotalRoundChanges: wbftExtra.Round,
		SuccessOnFirstTry: wbftExtra.Round == 0,
	}, nil
}

// DecodeVanityData decodes the vanity data from a block header
func (p *Parser) DecodeVanityData(block *types.Block) (string, error) {
	if block == nil {
		return "", fmt.Errorf("block is nil")
	}

	if len(block.Header().Extra) < 32 {
		return "", fmt.Errorf("extra data too short")
	}

	vanity := block.Header().Extra[:32]
	// Trim null bytes
	vanity = bytes.TrimRight(vanity, "\x00")
	return string(vanity), nil
}

// CacheValidators stores validators in the cache for a specific block
func (p *Parser) CacheValidators(blockNumber uint64, validators []common.Address) {
	p.validatorCache[blockNumber] = validators
}

// ClearCache clears the validator cache
func (p *Parser) ClearCache() {
	p.validatorCache = make(map[uint64][]common.Address)
}
