// Package poa provides a consensus parser for Proof of Authority (PoA) chains.
// This includes support for Clique consensus (used by Geth/Anvil) and similar PoA mechanisms.
package poa

import (
	"context"
	"encoding/binary"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/types/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// Clique consensus constants
const (
	// ExtraVanity is the fixed number of extra-data prefix bytes reserved for signer vanity
	ExtraVanity = 32

	// ExtraSeal is the fixed number of extra-data suffix bytes reserved for signer seal
	ExtraSeal = 65 // Secp256k1 signature
)

// Ensure Parser implements chain.ConsensusParser
var _ chain.ConsensusParser = (*Parser)(nil)

// Parser implements chain.ConsensusParser for PoA/Clique consensus
type Parser struct {
	logger *zap.Logger
	// signerCache stores known signers from parsed blocks
	// In PoA, these are the validators that have signed blocks
	signerCache map[common.Address]bool
	// blockSigners maps block numbers to their signers
	blockSigners map[uint64]common.Address
}

// NewParser creates a new PoA consensus parser
func NewParser(logger *zap.Logger) *Parser {
	return &Parser{
		logger:       logger,
		signerCache:  make(map[common.Address]bool),
		blockSigners: make(map[uint64]common.Address),
	}
}

// ConsensusType returns the consensus type identifier
func (p *Parser) ConsensusType() chain.ConsensusType {
	return chain.ConsensusTypePoA
}

// ParseConsensusData extracts consensus information from a block header
func (p *Parser) ParseConsensusData(block *types.Block) (*chain.ConsensusData, error) {
	if block == nil {
		return nil, errors.New("block is nil")
	}

	header := block.Header()

	// Extract signer from the block header's extra data
	signer, err := p.extractSigner(header)
	if err != nil {
		p.logger.Debug("Failed to extract signer from block",
			zap.Uint64("block", block.NumberU64()),
			zap.Error(err),
		)
		// Don't fail - return partial data
		signer = common.Address{}
	}

	// Cache the signer for GetValidators() lookups
	if signer != (common.Address{}) {
		p.signerCache[signer] = true
		p.blockSigners[block.NumberU64()] = signer
	}

	// Build consensus data
	consensusData := &chain.ConsensusData{
		ConsensusType:     chain.ConsensusTypePoA,
		BlockNumber:       block.NumberU64(),
		BlockHash:         block.Hash(),
		ProposerAddress:   signer,
		ParticipationRate: 100.0, // PoA has single signer per block
		ValidatorCount:    1,     // Single signer for this block
		SignedValidators:  []common.Address{signer},
		IsEpochBoundary:   false, // PoA typically doesn't have epochs
		ExtraData: &PoAExtraData{
			Signer:     signer,
			Difficulty: block.Difficulty().Uint64(),
			Nonce:      binary.BigEndian.Uint64(header.Nonce[:]),
			Coinbase:   header.Coinbase,
		},
	}

	return consensusData, nil
}

// GetValidators returns the validator set at a specific block
// For PoA, validators are typically configured at genesis.
// This returns all known signers discovered from parsed blocks.
func (p *Parser) GetValidators(ctx context.Context, blockNumber uint64) ([]common.Address, error) {
	// Return all known signers from parsed blocks
	// In PoA, the validator set is typically fixed at genesis,
	// but we can infer it from blocks we've seen
	validators := make([]common.Address, 0, len(p.signerCache))
	for signer := range p.signerCache {
		validators = append(validators, signer)
	}
	return validators, nil
}

// GetBlockSigner returns the signer for a specific block if known
func (p *Parser) GetBlockSigner(blockNumber uint64) (common.Address, bool) {
	signer, ok := p.blockSigners[blockNumber]
	return signer, ok
}

// ClearCache clears the signer cache
func (p *Parser) ClearCache() {
	p.signerCache = make(map[common.Address]bool)
	p.blockSigners = make(map[uint64]common.Address)
}

// IsEpochBoundary checks if the block is an epoch boundary
// PoA/Clique doesn't have traditional epochs like WBFT
func (p *Parser) IsEpochBoundary(block *types.Block) bool {
	// Clique uses "checkpoint" blocks at epoch boundaries (every N blocks)
	// where the extra data contains the full validator list
	// For simplicity, return false - can be enhanced if needed
	return false
}

// extractSigner recovers the signer address from block header
// This follows the Clique consensus signature format
func (p *Parser) extractSigner(header *types.Header) (common.Address, error) {
	if len(header.Extra) < ExtraVanity+ExtraSeal {
		return common.Address{}, errors.New("extra data too short for Clique signature")
	}

	// Extract signature from extra data
	// Format: [vanity (32 bytes)][validators (if checkpoint)]...[signature (65 bytes)]
	signature := header.Extra[len(header.Extra)-ExtraSeal:]

	// Recover the public key from signature
	// The signed message is the header hash without the signature
	sealHash := sealHash(header)
	pubkey, err := crypto.Ecrecover(sealHash.Bytes(), signature)
	if err != nil {
		return common.Address{}, err
	}

	// Convert public key to address
	var signer common.Address
	copy(signer[:], crypto.Keccak256(pubkey[1:])[12:])

	return signer, nil
}

// sealHash computes the hash to be signed for Clique consensus
// This is the header hash excluding the signature in extra data
func sealHash(header *types.Header) common.Hash {
	// Create a copy of the header with truncated extra data (remove signature)
	cpy := types.CopyHeader(header)
	if len(cpy.Extra) >= ExtraSeal {
		cpy.Extra = cpy.Extra[:len(cpy.Extra)-ExtraSeal]
	}
	return cpy.Hash()
}

// PoAExtraData holds PoA-specific consensus data
type PoAExtraData struct {
	// Signer is the address that signed this block
	Signer common.Address

	// Difficulty indicates in-turn (2) or out-of-turn (1) signing
	Difficulty uint64

	// Nonce is typically used for voting in Clique
	Nonce uint64

	// Coinbase is the miner/signer address from header
	Coinbase common.Address
}
