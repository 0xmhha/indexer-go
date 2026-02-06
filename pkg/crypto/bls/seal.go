package bls

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	// WBFTExtraVanity is the fixed number of bytes for the vanity data
	WBFTExtraVanity = 32
)

// SealHash computes the hash used for WBFT seal verification
// This is the block header hash without the committed seal signature
// The message that validators sign is: keccak256(header_without_seal)
func SealHash(header *types.Header) (common.Hash, error) {
	if header == nil {
		return common.Hash{}, fmt.Errorf("header is nil")
	}

	// Create a copy of the header to avoid modifying the original
	sealHeader := CopyHeader(header)

	// For WBFT, the seal hash is computed from the header with extra data
	// truncated to just the vanity bytes (removing all RLP-encoded consensus data)
	if len(sealHeader.Extra) > WBFTExtraVanity {
		sealHeader.Extra = sealHeader.Extra[:WBFTExtraVanity]
	}

	// RLP encode the header without the seal
	return rlpHash(sealHeader)
}

// SealHashForRound computes the seal hash including the round number
// This is used when the consensus protocol includes round information in the signed message
func SealHashForRound(header *types.Header, round uint32) (common.Hash, error) {
	if header == nil {
		return common.Hash{}, fmt.Errorf("header is nil")
	}

	// Get the base seal hash
	baseHash, err := SealHash(header)
	if err != nil {
		return common.Hash{}, err
	}

	// If round is 0, just return the base hash
	if round == 0 {
		return baseHash, nil
	}

	// Include round in the hash: keccak256(baseHash || round)
	roundBytes := make([]byte, 4)
	roundBytes[0] = byte(round >> 24)
	roundBytes[1] = byte(round >> 16)
	roundBytes[2] = byte(round >> 8)
	roundBytes[3] = byte(round)

	combined := append(baseHash.Bytes(), roundBytes...)
	return crypto.Keccak256Hash(combined), nil
}

// PrepareCommitHash computes the hash used for the prepare/commit phase
// In PBFT-style consensus, validators sign a hash that includes phase information
func PrepareCommitHash(header *types.Header, round uint32, phase string) (common.Hash, error) {
	if header == nil {
		return common.Hash{}, fmt.Errorf("header is nil")
	}

	// Get the base seal hash
	baseHash, err := SealHash(header)
	if err != nil {
		return common.Hash{}, err
	}

	// Create the message: keccak256(baseHash || round || phase)
	buf := new(bytes.Buffer)
	buf.Write(baseHash.Bytes())

	// Encode round
	roundBytes := make([]byte, 4)
	roundBytes[0] = byte(round >> 24)
	roundBytes[1] = byte(round >> 16)
	roundBytes[2] = byte(round >> 8)
	roundBytes[3] = byte(round)
	buf.Write(roundBytes)

	// Encode phase
	buf.WriteString(phase)

	return crypto.Keccak256Hash(buf.Bytes()), nil
}

// CopyHeader creates a deep copy of a block header
func CopyHeader(header *types.Header) *types.Header {
	cpy := &types.Header{
		ParentHash:  header.ParentHash,
		UncleHash:   header.UncleHash,
		Coinbase:    header.Coinbase,
		Root:        header.Root,
		TxHash:      header.TxHash,
		ReceiptHash: header.ReceiptHash,
		Difficulty:  header.Difficulty,
		Number:      header.Number,
		GasLimit:    header.GasLimit,
		GasUsed:     header.GasUsed,
		Time:        header.Time,
		Nonce:       header.Nonce,
		MixDigest:   header.MixDigest,
	}

	// Deep copy Bloom
	cpy.Bloom = header.Bloom

	// Deep copy Extra
	if len(header.Extra) > 0 {
		cpy.Extra = make([]byte, len(header.Extra))
		copy(cpy.Extra, header.Extra)
	}

	// Copy optional fields if present
	if header.BaseFee != nil {
		cpy.BaseFee = new(big.Int).Set(header.BaseFee)
	}
	if header.WithdrawalsHash != nil {
		h := *header.WithdrawalsHash
		cpy.WithdrawalsHash = &h
	}
	if header.BlobGasUsed != nil {
		u := *header.BlobGasUsed
		cpy.BlobGasUsed = &u
	}
	if header.ExcessBlobGas != nil {
		u := *header.ExcessBlobGas
		cpy.ExcessBlobGas = &u
	}
	if header.ParentBeaconRoot != nil {
		h := *header.ParentBeaconRoot
		cpy.ParentBeaconRoot = &h
	}
	if header.RequestsHash != nil {
		h := *header.RequestsHash
		cpy.RequestsHash = &h
	}

	return cpy
}

// rlpHash computes the Keccak256 hash of the RLP encoding of a value
func rlpHash(x interface{}) (common.Hash, error) {
	var buf bytes.Buffer
	if err := rlp.Encode(&buf, x); err != nil {
		return common.Hash{}, fmt.Errorf("failed to RLP encode: %w", err)
	}
	return crypto.Keccak256Hash(buf.Bytes()), nil
}
