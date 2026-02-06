package fetch

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/constants"
	consensustypes "github.com/0xmhha/indexer-go/pkg/types/consensus"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"go.uber.org/zap"
)

const (
	// WBFTExtraVanity is the fixed number of extra-data prefix bytes reserved for signer vanity
	WBFTExtraVanity = 32

	// WBFTExtraSeal is the fixed number of extra-data suffix bytes reserved for signer seal
	// This is a BLS signature (96 bytes) in WBFT
	WBFTExtraSeal = 96
)

// WBFTParser handles parsing of WBFT consensus data from block headers
type WBFTParser struct {
	logger *zap.Logger
}

// NewWBFTParser creates a new WBFT parser instance
func NewWBFTParser(logger *zap.Logger) *WBFTParser {
	return &WBFTParser{
		logger: logger,
	}
}

// ParseWBFTExtra decodes the WBFT extra data from a block header
// The extra data structure follows go-stablenet's WBFTExtra format
func (p *WBFTParser) ParseWBFTExtra(header *types.Header) (*consensustypes.WBFTExtra, error) {
	if header == nil {
		return nil, fmt.Errorf("header is nil")
	}

	extraData := header.Extra
	if len(extraData) < WBFTExtraVanity {
		return nil, fmt.Errorf("invalid extra data length: %d, expected at least %d", len(extraData), WBFTExtraVanity)
	}

	// Extract vanity data (first 32 bytes)
	vanityData := extraData[:WBFTExtraVanity]

	// The rest is RLP-encoded WBFT data
	rlpData := extraData[WBFTExtraVanity:]

	// Decode RLP data into wbftExtraRLP structure
	var wbftRLP wbftExtraRLP
	if err := rlp.DecodeBytes(rlpData, &wbftRLP); err != nil {
		p.logger.Error("Failed to decode WBFT extra RLP data",
			zap.Uint64("block_number", header.Number.Uint64()),
			zap.Int("rlp_data_length", len(rlpData)),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to decode WBFT extra RLP: %w", err)
	}

	// Convert RLP structure to consensus types
	wbftExtra := &consensustypes.WBFTExtra{
		VanityData:   vanityData,
		RandaoReveal: wbftRLP.RandaoReveal,
		PrevRound:    wbftRLP.PrevRound,
		Round:        wbftRLP.Round,
		GasTip:       wbftRLP.GasTip,
	}

	// Convert previous prepared seal
	if wbftRLP.PrevPreparedSeal != nil {
		wbftExtra.PrevPreparedSeal = &consensustypes.WBFTAggregatedSeal{
			Sealers:   wbftRLP.PrevPreparedSeal.Sealers,
			Signature: wbftRLP.PrevPreparedSeal.Signature,
		}
	}

	// Convert previous committed seal
	if wbftRLP.PrevCommittedSeal != nil {
		wbftExtra.PrevCommittedSeal = &consensustypes.WBFTAggregatedSeal{
			Sealers:   wbftRLP.PrevCommittedSeal.Sealers,
			Signature: wbftRLP.PrevCommittedSeal.Signature,
		}
	}

	// Convert current prepared seal
	if wbftRLP.PreparedSeal != nil {
		wbftExtra.PreparedSeal = &consensustypes.WBFTAggregatedSeal{
			Sealers:   wbftRLP.PreparedSeal.Sealers,
			Signature: wbftRLP.PreparedSeal.Signature,
		}
	}

	// Convert current committed seal
	if wbftRLP.CommittedSeal != nil {
		wbftExtra.CommittedSeal = &consensustypes.WBFTAggregatedSeal{
			Sealers:   wbftRLP.CommittedSeal.Sealers,
			Signature: wbftRLP.CommittedSeal.Signature,
		}
	}

	// Convert epoch info if present
	if wbftRLP.EpochInfo != nil {
		wbftExtra.EpochInfo = &consensustypes.EpochInfoRaw{
			Validators:    wbftRLP.EpochInfo.Validators,
			BLSPublicKeys: wbftRLP.EpochInfo.BLSPublicKeys,
			Candidates:    make([]*consensustypes.CandidateRaw, len(wbftRLP.EpochInfo.Candidates)),
		}

		// Convert candidates
		for i, candidate := range wbftRLP.EpochInfo.Candidates {
			wbftExtra.EpochInfo.Candidates[i] = &consensustypes.CandidateRaw{
				Address:   candidate.Address,
				Diligence: candidate.Diligence,
			}
		}
	}

	return wbftExtra, nil
}

// ExtractValidators extracts the validator addresses from WBFT extra data
// Validators are derived from the epoch info or from historical data
func (p *WBFTParser) ExtractValidators(wbftExtra *consensustypes.WBFTExtra) ([]common.Address, error) {
	if wbftExtra == nil {
		return nil, fmt.Errorf("wbftExtra is nil")
	}

	// If epoch info is present, extract validators from there
	if wbftExtra.EpochInfo != nil && len(wbftExtra.EpochInfo.Candidates) > 0 {
		validators := make([]common.Address, 0, len(wbftExtra.EpochInfo.Validators))

		// Map validator indices to addresses
		for _, validatorIndex := range wbftExtra.EpochInfo.Validators {
			if int(validatorIndex) < len(wbftExtra.EpochInfo.Candidates) {
				validators = append(validators, wbftExtra.EpochInfo.Candidates[validatorIndex].Address)
			}
		}

		return validators, nil
	}

	// If no epoch info, try to extract from committed seal sealers bitmap
	// This requires knowing the total validator set, which we may not have here
	// For now, return empty list - this will be populated from storage in later phases
	return []common.Address{}, nil
}

// ExtractSignersFromSeal extracts signer addresses from an aggregated seal using the bitmap
func (p *WBFTParser) ExtractSignersFromSeal(
	seal *consensustypes.WBFTAggregatedSeal,
	validators []common.Address,
) ([]common.Address, error) {
	if seal == nil || len(seal.Sealers) == 0 {
		return []common.Address{}, nil
	}

	if len(validators) == 0 {
		// Cannot extract signers without knowing the validator set
		return []common.Address{}, nil
	}

	signers := make([]common.Address, 0)

	// The sealers field is a bitmap indicating which validators signed
	// Each byte contains 8 bits, each bit represents a validator
	for i, validator := range validators {
		byteIndex := i / constants.BitsPerByte
		bitIndex := uint(i % constants.BitsPerByte)

		if byteIndex >= len(seal.Sealers) {
			break
		}

		// Check if the bit is set for this validator
		if (seal.Sealers[byteIndex] & (1 << bitIndex)) != 0 {
			signers = append(signers, validator)
		}
	}

	return signers, nil
}

// ParseEpochInfo parses epoch information from raw epoch data
func (p *WBFTParser) ParseEpochInfo(header *types.Header, epochInfoRaw *consensustypes.EpochInfoRaw) (*consensustypes.EpochData, error) {
	if epochInfoRaw == nil {
		return nil, fmt.Errorf("epochInfoRaw is nil")
	}

	// Calculate epoch number from block number
	// The chain stores EpochInfo at epoch boundary blocks (e.g., block 10, 20, 30 with epoch length 10)
	epochNumber := header.Number.Uint64() / constants.DefaultEpochLength

	epochData := &consensustypes.EpochData{
		EpochNumber:    epochNumber,
		ValidatorCount: len(epochInfoRaw.Validators),
		CandidateCount: len(epochInfoRaw.Candidates),
		Validators:     make([]consensustypes.ValidatorInfo, 0, len(epochInfoRaw.Validators)),
		Candidates:     make([]consensustypes.CandidateInfo, 0, len(epochInfoRaw.Candidates)),
	}

	// Build validator info list
	for i, validatorIndex := range epochInfoRaw.Validators {
		if int(validatorIndex) >= len(epochInfoRaw.Candidates) {
			continue
		}

		candidate := epochInfoRaw.Candidates[validatorIndex]
		var blsPubKey []byte
		if i < len(epochInfoRaw.BLSPublicKeys) {
			blsPubKey = epochInfoRaw.BLSPublicKeys[i]
		}

		epochData.Validators = append(epochData.Validators, consensustypes.ValidatorInfo{
			Address:   candidate.Address,
			Index:     validatorIndex,
			BLSPubKey: blsPubKey,
		})
	}

	// Build candidate info list
	for _, candidate := range epochInfoRaw.Candidates {
		epochData.Candidates = append(epochData.Candidates, consensustypes.CandidateInfo{
			Address:   candidate.Address,
			Diligence: candidate.Diligence,
		})
	}

	return epochData, nil
}

// VerifySeal verifies the BLS signature in an aggregated seal
// This method validates the seal structure and delegates to the BLS verifier for
// cryptographic verification when BLS public keys are available.
//
// Parameters:
// - header: The block header containing the seal
// - seal: The aggregated BLS seal to verify
// - validators: List of validator addresses (for basic validation)
// - validatorInfos: Optional list of validators with BLS public keys (for full verification)
// - round: The consensus round number
//
// Returns nil if verification passes, error otherwise.
func (p *WBFTParser) VerifySeal(
	header *types.Header,
	seal *consensustypes.WBFTAggregatedSeal,
	validators []common.Address,
) error {
	if seal == nil {
		return fmt.Errorf("seal is nil")
	}

	if len(seal.Signature) != WBFTExtraSeal {
		return fmt.Errorf("invalid seal signature length: %d, expected %d", len(seal.Signature), WBFTExtraSeal)
	}

	// Basic validation: check that signature is not empty
	if isEmptySignature(seal.Signature) {
		return fmt.Errorf("seal signature is empty (all zeros)")
	}

	// Basic validation: check sealers bitmap is not empty
	if len(seal.Sealers) == 0 {
		return fmt.Errorf("seal has no sealers")
	}

	// Count signers from bitmap
	signerCount := countSignersFromBitmap(seal.Sealers, len(validators))
	if signerCount == 0 {
		return fmt.Errorf("no signers in seal bitmap")
	}

	// Check minimum quorum (2/3 of validators)
	minSigners := (len(validators)*2)/3 + 1
	if signerCount < minSigners {
		return fmt.Errorf("insufficient signers: got %d, need at least %d for quorum", signerCount, minSigners)
	}

	p.logger.Debug("Seal structure validated",
		zap.Uint64("block_number", header.Number.Uint64()),
		zap.Int("signer_count", signerCount),
		zap.Int("total_validators", len(validators)),
		zap.Float64("participation_pct", float64(signerCount)/float64(len(validators))*100.0),
	)

	// Note: Full BLS signature verification requires ValidatorInfo with BLS public keys.
	// Use VerifySealWithBLS for cryptographic verification when keys are available.
	return nil
}

// VerifySealWithBLS performs full BLS signature verification using the BLS verifier
// This requires validators with BLS public keys
func (p *WBFTParser) VerifySealWithBLS(
	header *types.Header,
	seal *consensustypes.WBFTAggregatedSeal,
	validatorInfos []consensustypes.ValidatorInfo,
	round uint32,
	verifier BLSVerifier,
) error {
	if verifier == nil {
		return fmt.Errorf("BLS verifier is nil")
	}

	result := verifier.VerifySeal(header, seal, validatorInfos, round)
	if result.Error != nil {
		return result.Error
	}

	if !result.Valid {
		return fmt.Errorf("BLS signature verification failed")
	}

	return nil
}

// BLSVerifier interface for BLS signature verification
// This allows for dependency injection and testing
type BLSVerifier interface {
	VerifySeal(
		header *types.Header,
		seal *consensustypes.WBFTAggregatedSeal,
		validators []consensustypes.ValidatorInfo,
		round uint32,
	) *BLSVerifyResult
}

// BLSVerifyResult contains the result of BLS seal verification
type BLSVerifyResult struct {
	Valid            bool
	SignerCount      int
	TotalValidators  int
	ParticipationPct float64
	HasQuorum        bool
	Signers          []common.Address
	Error            error
}

// isEmptySignature checks if a signature is all zeros
func isEmptySignature(sig []byte) bool {
	for _, b := range sig {
		if b != 0 {
			return false
		}
	}
	return true
}

// countSignersFromBitmap counts the number of set bits in the sealers bitmap
func countSignersFromBitmap(bitmap []byte, validatorCount int) int {
	count := 0
	for i := 0; i < validatorCount; i++ {
		byteIndex := i / constants.BitsPerByte
		bitIndex := uint(i % constants.BitsPerByte)

		if byteIndex >= len(bitmap) {
			break
		}

		if (bitmap[byteIndex] & (1 << bitIndex)) != 0 {
			count++
		}
	}
	return count
}

// wbftExtraRLP is the RLP structure for WBFT extra data
// This matches the encoding used in go-stablenet
type wbftExtraRLP struct {
	RandaoReveal      []byte
	PrevRound         uint32
	PrevPreparedSeal  *sealRLP `rlp:"nil"`
	PrevCommittedSeal *sealRLP `rlp:"nil"`
	Round             uint32
	PreparedSeal      *sealRLP `rlp:"nil"`
	CommittedSeal     *sealRLP `rlp:"nil"`
	GasTip            *big.Int
	EpochInfo         *epochInfoRLP `rlp:"nil"`
}

// sealRLP is the RLP structure for an aggregated seal
type sealRLP struct {
	Sealers   []byte
	Signature []byte
}

// epochInfoRLP is the RLP structure for epoch information
type epochInfoRLP struct {
	Candidates    []*candidateRLP
	Validators    []uint32
	BLSPublicKeys [][]byte
}

// candidateRLP is the RLP structure for a candidate validator
type candidateRLP struct {
	Address   common.Address
	Diligence uint64
}

// DecodeSealersFromBitmap is a utility function to decode the sealers bitmap
// into a list of validator indices
func DecodeSealersFromBitmap(bitmap []byte, totalValidators int) []int {
	indices := make([]int, 0)

	for i := 0; i < totalValidators; i++ {
		byteIndex := i / constants.BitsPerByte
		bitIndex := uint(i % constants.BitsPerByte)

		if byteIndex >= len(bitmap) {
			break
		}

		if (bitmap[byteIndex] & (1 << bitIndex)) != 0 {
			indices = append(indices, i)
		}
	}

	return indices
}

// EncodeSealersToBitmap is a utility function to encode validator indices
// into a bitmap for the sealers field
func EncodeSealersToBitmap(indices []int, totalValidators int) []byte {
	// Calculate required bytes for bitmap
	bitmapSize := (totalValidators + constants.BitsPerByte - 1) / constants.BitsPerByte
	bitmap := make([]byte, bitmapSize)

	for _, index := range indices {
		if index >= totalValidators {
			continue
		}

		byteIndex := index / constants.BitsPerByte
		bitIndex := uint(index % constants.BitsPerByte)

		bitmap[byteIndex] |= 1 << bitIndex
	}

	return bitmap
}

// CompareSeals compares two aggregated seals for equality
func CompareSeals(seal1, seal2 *consensustypes.WBFTAggregatedSeal) bool {
	if seal1 == nil && seal2 == nil {
		return true
	}
	if seal1 == nil || seal2 == nil {
		return false
	}

	return bytes.Equal(seal1.Sealers, seal2.Sealers) &&
		bytes.Equal(seal1.Signature, seal2.Signature)
}
