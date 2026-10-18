package capture_test

import (
	"crypto/sha256"
	"encoding/json"
	"math"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestSourceSearchFilterCanonicalORAndLegacyCursor(t *testing.T) {
	a, b := common.Address{2}, common.Address{1}
	h, z := common.Hash{3}, common.Hash{}
	q := capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 2, Address: &a, Topics: [4]*common.Hash{nil, &z}}
	// Exactly the old six-field encoding, without the new optional fields.
	legacy, e := json.Marshal(struct {
		Kind    capture.RowKind `json:"kind"`
		From    uint64          `json:"from"`
		To      uint64          `json:"to"`
		Address *common.Address `json:"address,omitempty"`
		Role    string          `json:"role,omitempty"`
		Topics  [4]*common.Hash `json:"topics"`
	}{q.Kind, q.From, q.To, q.Address, q.Role, q.Topics})
	require.NoError(t, e)
	require.Equal(t, common.Hash(sha256.Sum256(legacy)), q.Digest())
	repeated := capture.SearchFilter{Kind: q.Kind, From: 1, To: 2, Addresses: []common.Address{a, a}, TopicAny: [][]common.Hash{nil, {z, z}, nil, nil}}
	require.Equal(t, q.Digest(), repeated.Digest())
	q = capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 2, Addresses: []common.Address{a, b, a}, TopicAny: [][]common.Hash{{h, z, h}, nil, nil, nil}}
	normal, e := q.Normalize()
	require.NoError(t, e)
	require.Equal(t, []common.Address{b, a}, normal.Addresses)
	require.Equal(t, []common.Hash{z, h}, normal.TopicAny[0])
	require.Equal(t, []common.Address{a, b, a}, q.Addresses, "normalization must not change caller-owned slices")
	require.Equal(t, []common.Hash{h, z, h}, q.TopicAny[0])
	require.Equal(t, q.Digest(), normal.Digest())
	normal.Addresses[0] = common.Address{99}
	require.NotEqual(t, normal.Digest(), q.Digest())
	zero, max := uint64(0), uint64(math.MaxUint64)
	q.FromTime, q.ToTime = &zero, &max
	require.NoError(t, q.Validate())
	require.NotEqual(t, q.Digest(), repeated.Digest())
	q.FromTime, q.ToTime = &max, &zero
	require.ErrorIs(t, q.Validate(), port.ErrInvalidData)
}

func TestSourceSearchFilterAlternativeBounds(t *testing.T) {
	base := capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 2}
	a, h := common.Address{}, common.Hash{}
	for _, q := range []capture.SearchFilter{
		{Kind: capture.RowLog, Address: &a, Addresses: []common.Address{a}},
		{Kind: capture.RowLog, Addresses: make([]common.Address, capture.MaxSearchAlternatives+1)},
		{Kind: capture.RowLog, TopicAny: make([][]common.Hash, 3)},
		{Kind: capture.RowLog, Topics: [4]*common.Hash{&h}, TopicAny: [][]common.Hash{{h}, nil, nil, nil}},
		{Kind: capture.RowLog, TopicAny: [][]common.Hash{make([]common.Hash, capture.MaxSearchAlternatives+1), nil, nil, nil}},
		{Kind: capture.RowTransaction, TopicAny: [][]common.Hash{nil, {h}, nil, nil}},
		{Kind: capture.RowTransaction, Role: "from"},
	} {
		require.ErrorIs(t, q.Validate(), port.ErrInvalidData)
	}
	base.TopicAny = make([][]common.Hash, 4)
	for i := range base.TopicAny {
		base.TopicAny[i] = make([]common.Hash, capture.MaxSearchAlternatives)
	}
	require.NoError(t, base.Validate())
	base.Address = &a
	require.ErrorIs(t, base.Validate(), port.ErrInvalidData, "bound total input before deduplication")
	base = capture.SearchFilter{Kind: capture.RowTransaction, Role: "from", Addresses: []common.Address{a}}
	require.NoError(t, base.Validate())
}
