package bls

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicKeyFromBytes(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
		errType error
	}{
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: true,
			errType: ErrPublicKeyLengthMismatch,
		},
		{
			name:    "wrong length - too short",
			data:    make([]byte, 32),
			wantErr: true,
			errType: ErrPublicKeyLengthMismatch,
		},
		{
			name:    "wrong length - too long",
			data:    make([]byte, 64),
			wantErr: true,
			errType: ErrPublicKeyLengthMismatch,
		},
		{
			name:    "correct length but invalid key",
			data:    make([]byte, PublicKeyLength), // All zeros is not a valid point
			wantErr: true,
			errType: ErrInvalidPublicKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pk, err := PublicKeyFromBytes(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, pk)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, pk)
			}
		})
	}
}

func TestSignatureFromBytes(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
		errType error
	}{
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: true,
			errType: ErrSignatureLengthMismatch,
		},
		{
			name:    "wrong length - too short",
			data:    make([]byte, 48),
			wantErr: true,
			errType: ErrSignatureLengthMismatch,
		},
		{
			name:    "wrong length - too long",
			data:    make([]byte, 128),
			wantErr: true,
			errType: ErrSignatureLengthMismatch,
		},
		{
			name:    "correct length but invalid signature",
			data:    make([]byte, SignatureLength), // All zeros is not a valid point
			wantErr: true,
			errType: ErrInvalidSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig, err := SignatureFromBytes(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, sig)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, sig)
			}
		})
	}
}

func TestAggregatePublicKeys(t *testing.T) {
	t.Run("empty list returns error", func(t *testing.T) {
		_, err := AggregatePublicKeys([]*PublicKey{})
		assert.ErrorIs(t, err, ErrEmptyPublicKeys)
	})

	t.Run("nil key in list returns error", func(t *testing.T) {
		_, err := AggregatePublicKeys([]*PublicKey{nil})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil public key")
	})
}

func TestVerify(t *testing.T) {
	t.Run("nil signature returns error", func(t *testing.T) {
		err := Verify(nil, []byte("message"), &PublicKey{})
		assert.ErrorIs(t, err, ErrInvalidSignature)
	})

	t.Run("nil public key returns error", func(t *testing.T) {
		// Note: Signature with nil inner pointer is checked first
		err := Verify(&Signature{}, []byte("message"), nil)
		assert.Error(t, err) // Either invalid signature or invalid public key
	})
}

func TestVerifyAggregated(t *testing.T) {
	t.Run("nil signature returns error", func(t *testing.T) {
		err := VerifyAggregated(nil, []byte("message"), []*PublicKey{})
		assert.ErrorIs(t, err, ErrInvalidSignature)
	})

	t.Run("empty public keys returns error", func(t *testing.T) {
		sig := &Signature{} // Will fail due to nil inner pointer
		err := VerifyAggregated(sig, []byte("message"), []*PublicKey{})
		assert.Error(t, err)
	})
}

func TestBatchVerify(t *testing.T) {
	t.Run("mismatched lengths returns error", func(t *testing.T) {
		sigs := []*Signature{{}}
		msgs := [][]byte{[]byte("msg1"), []byte("msg2")}
		pubkeys := []*PublicKey{{}}

		err := BatchVerify(sigs, msgs, pubkeys)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "mismatched lengths")
	})

	t.Run("empty inputs returns no error", func(t *testing.T) {
		err := BatchVerify([]*Signature{}, [][]byte{}, []*PublicKey{})
		assert.NoError(t, err)
	})
}

func TestConstants(t *testing.T) {
	t.Run("PublicKeyLength is 48", func(t *testing.T) {
		assert.Equal(t, 48, PublicKeyLength)
	})

	t.Run("SignatureLength is 96", func(t *testing.T) {
		assert.Equal(t, 96, SignatureLength)
	})
}

// BenchmarkAggregatePublicKeys benchmarks public key aggregation
// Skipped in short mode since we need valid keys
func BenchmarkAggregatePublicKeys(b *testing.B) {
	b.Skip("requires valid BLS keys for meaningful benchmark")
}

// TestRealBLSOperations tests with actual BLS key generation
// This is an integration test that requires the blst library to generate keys
func TestRealBLSOperations(t *testing.T) {
	t.Skip("requires BLS key generation - see integration tests")

	// To implement real tests:
	// 1. Generate a BLS keypair using blst
	// 2. Sign a message
	// 3. Verify the signature
	// 4. Test aggregation with multiple keys
}

// TestCompressP1Affines verifies the helper function
func TestCompressP1Affines(t *testing.T) {
	t.Run("empty input returns empty output", func(t *testing.T) {
		result := compressP1Affines(nil)
		require.NotNil(t, result)
		assert.Len(t, result, 0)
	})
}
