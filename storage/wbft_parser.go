package storage

import (
	"fmt"
	"io"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// SealerSet represents a bitmap of validators who signed
// This is a simplified version compatible with go-stablenet's SealerSet
type SealerSet []byte

// WBFTAggregatedSealRLP represents an aggregated BLS signature for RLP decoding
// This matches the structure in go-stablenet/core/types/istanbul.go
type WBFTAggregatedSealRLP struct {
	Sealers   []byte // Bitmap of validators who signed
	Signature []byte // Aggregated BLS signature (96 bytes)
}

// WBFTExtraRLP represents header extradata for WBFT protocol
// This matches the structure in go-stablenet/core/types/istanbul.go
type WBFTExtraRLP struct {
	VanityData        []byte
	RandaoReveal      []byte
	PrevRound         uint32
	PrevPreparedSeal  *WBFTAggregatedSealRLP
	PrevCommittedSeal *WBFTAggregatedSealRLP
	Round             uint32
	PreparedSeal      *WBFTAggregatedSealRLP
	CommittedSeal     *WBFTAggregatedSealRLP
	GasTip            *big.Int
	EpochInfo         *EpochInfoRLP
}

// CandidateRLP represents a validator candidate for RLP decoding
type CandidateRLP struct {
	Addr      []byte // Address as bytes (20 bytes)
	Diligence uint64
}

// EpochInfoRLP represents epoch information for RLP decoding
type EpochInfoRLP struct {
	Candidates    []*CandidateRLP
	Validators    []uint32
	BLSPublicKeys [][]byte
}

// EncodeRLP implements rlp.Encoder
func (as *WBFTAggregatedSealRLP) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, []interface{}{
		as.Sealers,
		as.Signature,
	})
}

// DecodeRLP implements rlp.Decoder
func (as *WBFTAggregatedSealRLP) DecodeRLP(s *rlp.Stream) error {
	var aggregatedSeal struct {
		Sealers   []byte
		Signature []byte
	}

	if err := s.Decode(&aggregatedSeal); err != nil {
		return err
	}
	as.Sealers, as.Signature = aggregatedSeal.Sealers, aggregatedSeal.Signature
	return nil
}

// EncodeRLP implements rlp.Encoder
func (qst *WBFTExtraRLP) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, []interface{}{
		qst.VanityData,
		qst.RandaoReveal,
		qst.PrevRound,
		qst.PrevPreparedSeal,
		qst.PrevCommittedSeal,
		qst.Round,
		qst.PreparedSeal,
		qst.CommittedSeal,
		qst.GasTip,
		qst.EpochInfo,
	})
}

// DecodeRLP implements rlp.Decoder
func (qst *WBFTExtraRLP) DecodeRLP(s *rlp.Stream) error {
	var wbftExtra struct {
		VanityData        []byte
		RandaoReveal      []byte
		PrevRound         uint32
		PrevPreparedSeal  *WBFTAggregatedSealRLP
		PrevCommittedSeal *WBFTAggregatedSealRLP
		Round             uint32
		PreparedSeal      *WBFTAggregatedSealRLP
		CommittedSeal     *WBFTAggregatedSealRLP
		GasTip            *big.Int
		EpochInfo         *EpochInfoRLP
	}

	if err := s.Decode(&wbftExtra); err != nil {
		return err
	}

	qst.VanityData = wbftExtra.VanityData
	qst.RandaoReveal = wbftExtra.RandaoReveal
	qst.PrevRound = wbftExtra.PrevRound
	qst.PrevPreparedSeal = wbftExtra.PrevPreparedSeal
	qst.PrevCommittedSeal = wbftExtra.PrevCommittedSeal
	qst.Round = wbftExtra.Round
	qst.PreparedSeal = wbftExtra.PreparedSeal
	qst.CommittedSeal = wbftExtra.CommittedSeal
	qst.GasTip = wbftExtra.GasTip
	qst.EpochInfo = wbftExtra.EpochInfo

	return nil
}

// ParseWBFTExtra parses WBFT Extra field from block header
func ParseWBFTExtra(header *types.Header) (*WBFTBlockExtra, error) {
	if header == nil {
		return nil, fmt.Errorf("header cannot be nil")
	}

	if len(header.Extra) == 0 {
		return nil, fmt.Errorf("header extra data is empty")
	}

	// Decode RLP
	wbftExtraRLP := new(WBFTExtraRLP)
	if err := rlp.DecodeBytes(header.Extra, wbftExtraRLP); err != nil {
		return nil, fmt.Errorf("failed to decode WBFT extra: %w", err)
	}

	// Convert to WBFTBlockExtra
	extra := &WBFTBlockExtra{
		BlockNumber:  header.Number.Uint64(),
		BlockHash:    header.Hash(),
		RandaoReveal: wbftExtraRLP.RandaoReveal,
		PrevRound:    wbftExtraRLP.PrevRound,
		Round:        wbftExtraRLP.Round,
		GasTip:       wbftExtraRLP.GasTip,
		Timestamp:    header.Time,
	}

	// Convert PrevPreparedSeal
	if wbftExtraRLP.PrevPreparedSeal != nil {
		extra.PrevPreparedSeal = &WBFTAggregatedSeal{
			Sealers:   wbftExtraRLP.PrevPreparedSeal.Sealers,
			Signature: wbftExtraRLP.PrevPreparedSeal.Signature,
		}
	}

	// Convert PrevCommittedSeal
	if wbftExtraRLP.PrevCommittedSeal != nil {
		extra.PrevCommittedSeal = &WBFTAggregatedSeal{
			Sealers:   wbftExtraRLP.PrevCommittedSeal.Sealers,
			Signature: wbftExtraRLP.PrevCommittedSeal.Signature,
		}
	}

	// Convert PreparedSeal
	if wbftExtraRLP.PreparedSeal != nil {
		extra.PreparedSeal = &WBFTAggregatedSeal{
			Sealers:   wbftExtraRLP.PreparedSeal.Sealers,
			Signature: wbftExtraRLP.PreparedSeal.Signature,
		}
	}

	// Convert CommittedSeal
	if wbftExtraRLP.CommittedSeal != nil {
		extra.CommittedSeal = &WBFTAggregatedSeal{
			Sealers:   wbftExtraRLP.CommittedSeal.Sealers,
			Signature: wbftExtraRLP.CommittedSeal.Signature,
		}
	}

	// Convert EpochInfo
	if wbftExtraRLP.EpochInfo != nil {
		// Calculate epoch number based on block number
		// The chain stores EpochInfo at epoch boundary blocks (e.g., block 10, 20, 30 with epoch length 10)
		// The EpochInfo at block N contains information for epoch N/epochLength
		blockNumber := header.Number.Uint64()
		epochNumber := blockNumber / constants.DefaultEpochLength

		epochInfo := &EpochInfo{
			EpochNumber:   epochNumber,
			BlockNumber:   blockNumber,
			Validators:    wbftExtraRLP.EpochInfo.Validators,
			BLSPublicKeys: wbftExtraRLP.EpochInfo.BLSPublicKeys,
		}

		// Convert candidates
		candidates := make([]Candidate, len(wbftExtraRLP.EpochInfo.Candidates))
		for i, c := range wbftExtraRLP.EpochInfo.Candidates {
			if len(c.Addr) != common.AddressLength {
				return nil, fmt.Errorf("invalid candidate address length: %d", len(c.Addr))
			}
			var addr [common.AddressLength]byte
			copy(addr[:], c.Addr)
			candidates[i] = Candidate{
				Address:   addr,
				Diligence: c.Diligence,
			}
		}
		epochInfo.Candidates = candidates

		extra.EpochInfo = epochInfo
	}

	return extra, nil
}

// ExtractSigners extracts validator addresses from a WBFTAggregatedSeal bitmap
// The Sealers field is a bitmap where each bit represents a validator index
func ExtractSigners(sealers []byte, validators []uint32, candidates []Candidate) ([]common.Address, error) {
	if sealers == nil || len(sealers) == 0 {
		return []common.Address{}, nil
	}

	var signers []common.Address

	// Iterate through each bit in the bitmap
	for byteIdx := 0; byteIdx < len(sealers); byteIdx++ {
		for bitIdx := 0; bitIdx < constants.BitsPerByte; bitIdx++ {
			validatorIdx := byteIdx*constants.BitsPerByte + bitIdx

			// Check if this validator index is in the validators list
			if validatorIdx >= len(validators) {
				break
			}

			// Check if the bit is set
			if sealers[byteIdx]&(1<<uint(bitIdx)) != 0 {
				candidateIdx := validators[validatorIdx]
				if int(candidateIdx) >= len(candidates) {
					return nil, fmt.Errorf("invalid candidate index: %d", candidateIdx)
				}
				signers = append(signers, candidates[candidateIdx].Address)
			}
		}
	}

	return signers, nil
}
