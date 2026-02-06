package bls

import (
	"errors"
	"fmt"

	"github.com/0xmhha/indexer-go/internal/constants"
	consensustypes "github.com/0xmhha/indexer-go/pkg/types/consensus"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

var (
	// ErrNoValidators is returned when no validators are provided for verification
	ErrNoValidators = errors.New("no validators provided")

	// ErrNoBLSPublicKeys is returned when validators don't have BLS public keys
	ErrNoBLSPublicKeys = errors.New("no BLS public keys available")

	// ErrNoSigners is returned when seal has no signers
	ErrNoSigners = errors.New("no signers in seal")

	// ErrInsufficientSigners is returned when not enough validators signed
	ErrInsufficientSigners = errors.New("insufficient signers for quorum")
)

// Verifier handles BLS signature verification for WBFT consensus
type Verifier struct {
	logger *zap.Logger

	// Minimum participation threshold (percentage of validators that must sign)
	// Default is 2/3 (66.67%) as required by BFT consensus
	minParticipationPct float64

	// Whether to skip verification (useful for testing or when BLS keys are unavailable)
	skipVerification bool
}

// VerifierOption is a functional option for configuring the Verifier
type VerifierOption func(*Verifier)

// WithLogger sets the logger for the verifier
func WithLogger(logger *zap.Logger) VerifierOption {
	return func(v *Verifier) {
		v.logger = logger
	}
}

// WithMinParticipation sets the minimum participation threshold
func WithMinParticipation(pct float64) VerifierOption {
	return func(v *Verifier) {
		v.minParticipationPct = pct
	}
}

// WithSkipVerification disables actual signature verification
// This is useful for indexers that only need to track participation without
// cryptographic verification
func WithSkipVerification(skip bool) VerifierOption {
	return func(v *Verifier) {
		v.skipVerification = skip
	}
}

// NewVerifier creates a new BLS verifier
func NewVerifier(opts ...VerifierOption) *Verifier {
	v := &Verifier{
		logger:              zap.NewNop(),
		minParticipationPct: 66.67, // 2/3 for BFT
		skipVerification:    false,
	}

	for _, opt := range opts {
		opt(v)
	}

	return v
}

// VerifySealResult contains the result of seal verification
type VerifySealResult struct {
	// Valid indicates if the signature is cryptographically valid
	Valid bool

	// SignerCount is the number of validators who signed
	SignerCount int

	// TotalValidators is the total number of validators in the set
	TotalValidators int

	// ParticipationPct is the percentage of validators who signed
	ParticipationPct float64

	// HasQuorum indicates if enough validators signed (>= 2/3)
	HasQuorum bool

	// Signers is the list of validator addresses who signed
	Signers []common.Address

	// Error contains any error that occurred during verification
	Error error
}

// VerifySeal verifies a BLS aggregated seal against the validator set
func (v *Verifier) VerifySeal(
	header *types.Header,
	seal *consensustypes.WBFTAggregatedSeal,
	validators []consensustypes.ValidatorInfo,
	round uint32,
) *VerifySealResult {
	result := &VerifySealResult{
		Valid:           false,
		TotalValidators: len(validators),
	}

	// Validate inputs
	if seal == nil {
		result.Error = errors.New("seal is nil")
		return result
	}

	if len(seal.Signature) != SignatureLength {
		result.Error = fmt.Errorf("invalid seal signature length: %d, expected %d",
			len(seal.Signature), SignatureLength)
		return result
	}

	if len(validators) == 0 {
		result.Error = ErrNoValidators
		return result
	}

	// Extract signers from bitmap
	signers, signerIndices := v.extractSignersFromBitmap(seal.Sealers, validators)
	result.SignerCount = len(signers)
	result.Signers = signers

	if len(signers) == 0 {
		result.Error = ErrNoSigners
		return result
	}

	// Calculate participation
	result.ParticipationPct = float64(len(signers)) / float64(len(validators)) * 100.0
	result.HasQuorum = result.ParticipationPct >= v.minParticipationPct

	// Check quorum
	if !result.HasQuorum {
		result.Error = fmt.Errorf("%w: got %.2f%%, need %.2f%%",
			ErrInsufficientSigners, result.ParticipationPct, v.minParticipationPct)
		return result
	}

	// Skip cryptographic verification if configured
	if v.skipVerification {
		v.logger.Debug("Skipping BLS signature verification",
			zap.Uint64("block_number", header.Number.Uint64()),
			zap.Int("signers", len(signers)),
		)
		result.Valid = true
		return result
	}

	// Collect BLS public keys for signers
	blsKeys, err := v.collectBLSPublicKeys(validators, signerIndices)
	if err != nil {
		result.Error = fmt.Errorf("failed to collect BLS public keys: %w", err)
		return result
	}

	// Compute the message hash (seal hash)
	messageHash, err := SealHashForRound(header, round)
	if err != nil {
		result.Error = fmt.Errorf("failed to compute seal hash: %w", err)
		return result
	}

	// Parse the aggregated signature
	sig, err := SignatureFromBytes(seal.Signature)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse signature: %w", err)
		return result
	}

	// Verify the aggregated signature
	err = VerifyAggregated(sig, messageHash.Bytes(), blsKeys)
	if err != nil {
		v.logger.Debug("BLS signature verification failed",
			zap.Uint64("block_number", header.Number.Uint64()),
			zap.Uint32("round", round),
			zap.Int("signers", len(signers)),
			zap.Error(err),
		)
		result.Error = fmt.Errorf("signature verification failed: %w", err)
		return result
	}

	v.logger.Debug("BLS signature verification succeeded",
		zap.Uint64("block_number", header.Number.Uint64()),
		zap.Uint32("round", round),
		zap.Int("signers", len(signers)),
		zap.Float64("participation_pct", result.ParticipationPct),
	)

	result.Valid = true
	return result
}

// VerifyCommittedSeal is a convenience method for verifying the committed seal
func (v *Verifier) VerifyCommittedSeal(
	header *types.Header,
	wbftExtra *consensustypes.WBFTExtra,
	validators []consensustypes.ValidatorInfo,
) *VerifySealResult {
	if wbftExtra == nil || wbftExtra.CommittedSeal == nil {
		return &VerifySealResult{
			Error: errors.New("no committed seal in WBFT extra data"),
		}
	}

	return v.VerifySeal(header, wbftExtra.CommittedSeal, validators, wbftExtra.Round)
}

// extractSignersFromBitmap extracts signer addresses and their indices from the bitmap
func (v *Verifier) extractSignersFromBitmap(
	bitmap []byte,
	validators []consensustypes.ValidatorInfo,
) ([]common.Address, []int) {
	signers := make([]common.Address, 0)
	indices := make([]int, 0)

	for i, validator := range validators {
		byteIndex := i / constants.BitsPerByte
		bitIndex := uint(i % constants.BitsPerByte)

		if byteIndex >= len(bitmap) {
			break
		}

		if (bitmap[byteIndex] & (1 << bitIndex)) != 0 {
			signers = append(signers, validator.Address)
			indices = append(indices, i)
		}
	}

	return signers, indices
}

// collectBLSPublicKeys collects BLS public keys for the specified validator indices
func (v *Verifier) collectBLSPublicKeys(
	validators []consensustypes.ValidatorInfo,
	indices []int,
) ([]*PublicKey, error) {
	keys := make([]*PublicKey, 0, len(indices))

	for _, idx := range indices {
		if idx >= len(validators) {
			return nil, fmt.Errorf("validator index %d out of range", idx)
		}

		validator := validators[idx]
		if len(validator.BLSPubKey) == 0 {
			return nil, fmt.Errorf("%w: validator %s at index %d has no BLS key",
				ErrNoBLSPublicKeys, validator.Address.Hex(), idx)
		}

		pubkey, err := PublicKeyFromBytes(validator.BLSPubKey)
		if err != nil {
			return nil, fmt.Errorf("invalid BLS public key for validator %s: %w",
				validator.Address.Hex(), err)
		}

		keys = append(keys, pubkey)
	}

	return keys, nil
}

// CheckParticipation checks if the seal has sufficient participation without
// verifying the cryptographic signature
func (v *Verifier) CheckParticipation(
	seal *consensustypes.WBFTAggregatedSeal,
	validatorCount int,
) (signerCount int, participationPct float64, hasQuorum bool) {
	if seal == nil || validatorCount == 0 {
		return 0, 0, false
	}

	// Count set bits in bitmap
	for i := 0; i < validatorCount; i++ {
		byteIndex := i / constants.BitsPerByte
		bitIndex := uint(i % constants.BitsPerByte)

		if byteIndex >= len(seal.Sealers) {
			break
		}

		if (seal.Sealers[byteIndex] & (1 << bitIndex)) != 0 {
			signerCount++
		}
	}

	participationPct = float64(signerCount) / float64(validatorCount) * 100.0
	hasQuorum = participationPct >= v.minParticipationPct

	return signerCount, participationPct, hasQuorum
}
