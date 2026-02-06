// Package bls provides BLS (Boneh-Lynn-Shacham) signature verification for WBFT consensus.
// It uses the BLS12-381 curve via the supranational/blst library for cryptographic operations.
package bls

import (
	"errors"
	"fmt"

	blst "github.com/supranational/blst/bindings/go"
)

const (
	// PublicKeyLength is the length of a BLS public key in bytes (48 bytes for BLS12-381 G1)
	PublicKeyLength = 48

	// SignatureLength is the length of a BLS signature in bytes (96 bytes for BLS12-381 G2)
	SignatureLength = 96

	// Domain Separation Tag for WBFT consensus signatures
	// This should match the DST used by go-stablenet
	DSTSignature = "BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_"
)

var (
	// ErrInvalidPublicKey is returned when a public key is malformed
	ErrInvalidPublicKey = errors.New("invalid BLS public key")

	// ErrInvalidSignature is returned when a signature is malformed
	ErrInvalidSignature = errors.New("invalid BLS signature")

	// ErrVerificationFailed is returned when signature verification fails
	ErrVerificationFailed = errors.New("BLS signature verification failed")

	// ErrEmptyPublicKeys is returned when trying to aggregate zero public keys
	ErrEmptyPublicKeys = errors.New("cannot aggregate empty public key list")

	// ErrPublicKeyLengthMismatch is returned when public key has wrong length
	ErrPublicKeyLengthMismatch = errors.New("public key length mismatch")

	// ErrSignatureLengthMismatch is returned when signature has wrong length
	ErrSignatureLengthMismatch = errors.New("signature length mismatch")
)

// PublicKey represents a BLS public key (G1 point on BLS12-381)
type PublicKey struct {
	p *blst.P1Affine
}

// Signature represents a BLS signature (G2 point on BLS12-381)
type Signature struct {
	s *blst.P2Affine
}

// PublicKeyFromBytes deserializes a BLS public key from its compressed representation
func PublicKeyFromBytes(data []byte) (*PublicKey, error) {
	if len(data) != PublicKeyLength {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrPublicKeyLengthMismatch, PublicKeyLength, len(data))
	}

	p := new(blst.P1Affine).Uncompress(data)
	if p == nil {
		return nil, ErrInvalidPublicKey
	}

	// Validate the point is on the curve and in the correct subgroup
	if !p.KeyValidate() {
		return nil, ErrInvalidPublicKey
	}

	return &PublicKey{p: p}, nil
}

// Bytes returns the compressed representation of the public key
func (pk *PublicKey) Bytes() []byte {
	return pk.p.Compress()
}

// SignatureFromBytes deserializes a BLS signature from its compressed representation
func SignatureFromBytes(data []byte) (*Signature, error) {
	if len(data) != SignatureLength {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrSignatureLengthMismatch, SignatureLength, len(data))
	}

	s := new(blst.P2Affine).Uncompress(data)
	if s == nil {
		return nil, ErrInvalidSignature
	}

	// Validate the signature is on the curve and in the correct subgroup
	if !s.SigValidate(false) {
		return nil, ErrInvalidSignature
	}

	return &Signature{s: s}, nil
}

// Bytes returns the compressed representation of the signature
func (sig *Signature) Bytes() []byte {
	return sig.s.Compress()
}

// AggregatePublicKeys aggregates multiple BLS public keys into a single public key
// This is used to verify aggregated signatures from multiple validators
func AggregatePublicKeys(pubkeys []*PublicKey) (*PublicKey, error) {
	if len(pubkeys) == 0 {
		return nil, ErrEmptyPublicKeys
	}

	// Convert to slice of P1Affine pointers for blst aggregation
	affines := make([]*blst.P1Affine, len(pubkeys))
	for i, pk := range pubkeys {
		if pk == nil || pk.p == nil {
			return nil, fmt.Errorf("nil public key at index %d", i)
		}
		affines[i] = pk.p
	}

	// Aggregate the public keys
	aggregator := new(blst.P1Aggregate)
	if !aggregator.AggregateCompressed(compressP1Affines(affines), true) {
		return nil, errors.New("failed to aggregate public keys")
	}

	return &PublicKey{p: aggregator.ToAffine()}, nil
}

// Verify verifies a BLS signature against a message and public key
func Verify(sig *Signature, msg []byte, pubkey *PublicKey) error {
	if sig == nil || sig.s == nil {
		return ErrInvalidSignature
	}
	if pubkey == nil || pubkey.p == nil {
		return ErrInvalidPublicKey
	}

	// Verify the signature
	// Using the standard DST for BLS signatures
	if !sig.s.Verify(true, pubkey.p, false, msg, []byte(DSTSignature)) {
		return ErrVerificationFailed
	}

	return nil
}

// VerifyAggregated verifies an aggregated BLS signature against a message and multiple public keys
// All signers must have signed the same message
func VerifyAggregated(sig *Signature, msg []byte, pubkeys []*PublicKey) error {
	if sig == nil || sig.s == nil {
		return ErrInvalidSignature
	}

	// Aggregate the public keys
	aggregatedPubkey, err := AggregatePublicKeys(pubkeys)
	if err != nil {
		return fmt.Errorf("failed to aggregate public keys: %w", err)
	}

	// Verify against the aggregated public key
	return Verify(sig, msg, aggregatedPubkey)
}

// compressP1Affines converts P1Affine slice to compressed bytes for aggregation
func compressP1Affines(affines []*blst.P1Affine) [][]byte {
	compressed := make([][]byte, len(affines))
	for i, a := range affines {
		compressed[i] = a.Compress()
	}
	return compressed
}

// BatchVerify verifies multiple signatures against their respective messages and public keys
// This is more efficient than verifying each signature individually
func BatchVerify(sigs []*Signature, msgs [][]byte, pubkeys []*PublicKey) error {
	if len(sigs) != len(msgs) || len(sigs) != len(pubkeys) {
		return errors.New("mismatched lengths of signatures, messages, and public keys")
	}

	if len(sigs) == 0 {
		return nil // Nothing to verify
	}

	// Convert to slices for batch verification
	p2Affines := make([]*blst.P2Affine, len(sigs))
	p1Affines := make([]*blst.P1Affine, len(pubkeys))

	for i := range sigs {
		if sigs[i] == nil || sigs[i].s == nil {
			return fmt.Errorf("nil signature at index %d", i)
		}
		if pubkeys[i] == nil || pubkeys[i].p == nil {
			return fmt.Errorf("nil public key at index %d", i)
		}
		p2Affines[i] = sigs[i].s
		p1Affines[i] = pubkeys[i].p
	}

	// Use blst's batch verification
	// Note: blst's CoreVerify doesn't have a built-in batch mode for different messages,
	// so we verify individually for now. For same-message aggregated verification,
	// use VerifyAggregated instead.
	for i := range sigs {
		if !p2Affines[i].Verify(true, p1Affines[i], false, msgs[i], []byte(DSTSignature)) {
			return fmt.Errorf("%w: signature at index %d", ErrVerificationFailed, i)
		}
	}

	return nil
}
