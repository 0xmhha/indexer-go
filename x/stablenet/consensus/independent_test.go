package consensus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

func TestSeedPriority3E6CryptographicConsensus(t *testing.T) {
	t.Run("signature_bitmap_quorum_transitions_trust", testIndependentRejectsSignatureBitmapQuorumAndTransitions)
	data, err := os.ReadFile("../../../testdata/execution/consensus/native.json")
	require.NoError(t, err)
	var fixture struct {
		Policy  TrustPolicy
		Headers [][]byte
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	v, err := NewIndependent(fixture.Policy)
	require.NoError(t, err)
	state := v.Anchor()
	for _, raw := range fixture.Headers {
		next, e := v.Verify(context.Background(), state, raw)
		require.NoError(t, e)
		for _, mutate := range []func(*types.Header){
			func(h *types.Header) { h.ParentHash[0] ^= 1 },
			func(h *types.Header) { h.Root[0] ^= 1 },
			func(h *types.Header) { h.Number.SetUint64(h.Number.Uint64() + 1) },
			func(h *types.Header) { h.WithdrawalsHash = &h.Root },
			func(h *types.Header) { h.Extra[len(h.Extra)/2] ^= 1 },
		} {
			var h types.Header
			require.NoError(t, rlp.DecodeBytes(raw, &h))
			mutate(&h)
			bad, e := rlp.EncodeToBytes(&h)
			require.NoError(t, e)
			_, e = v.Verify(context.Background(), state, bad)
			require.Error(t, e)
		}
		encoded, e := json.Marshal(next)
		require.NoError(t, e)
		restored, e := v.RestorePoint(t.Context(), encoded)
		require.NoError(t, e)
		require.NotEmpty(t, restored.State)
		var corrupt IndependentState
		require.NoError(t, json.Unmarshal(encoded, &corrupt))
		corrupt.Next[0].PublicKey[0] ^= 1
		_, e = v.RestorePoint(t.Context(), mustJSON(t, corrupt))
		require.Error(t, e)

		state = IndependentState{}
		require.NoError(t, json.Unmarshal(encoded, &state))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = v.Verify(ctx, state, fixture.Headers[0])
	require.ErrorIs(t, err, context.Canceled)
	_, err = v.Verify(context.Background(), state, fixture.Headers[0])
	require.Error(t, err)
}

func testIndependentRejectsSignatureBitmapQuorumAndTransitions(t *testing.T) {
	data, e := os.ReadFile("../../../testdata/execution/consensus/native.json")
	require.NoError(t, e)
	var f struct {
		Policy  TrustPolicy
		Headers [][]byte
	}
	require.NoError(t, json.Unmarshal(data, &f))
	v, e := NewIndependent(f.Policy)
	require.NoError(t, e)
	state := v.Anchor()
	for i, raw := range f.Headers {
		var original types.Header
		require.NoError(t, rlp.DecodeBytes(raw, &original))
		var extra WBFTExtraRLP
		require.NoError(t, rlp.DecodeBytes(original.Extra, &extra))
		for name, mutate := range map[string]func(*WBFTExtraRLP){
			"signature":        func(x *WBFTExtraRLP) { x.CommittedSeal.Signature[0] ^= 1 },
			"prepare":          func(x *WBFTExtraRLP) { x.PreparedSeal.Signature[0] ^= 1 },
			"quorum":           func(x *WBFTExtraRLP) { x.CommittedSeal.Sealers = []byte{3} },
			"outsider":         func(x *WBFTExtraRLP) { x.CommittedSeal.Sealers = []byte{0x87} },
			"oversized_bitmap": func(x *WBFTExtraRLP) { x.PreparedSeal.Sealers = []byte{7, 0} },
			"wrong_round":      func(x *WBFTExtraRLP) { x.Round++ },
			"wrong_domain":     func(x *WBFTExtraRLP) { x.CommittedSeal = x.PreparedSeal },
			"unauthenticated_epoch": func(x *WBFTExtraRLP) {
				if x.EpochInfo != nil {
					x.EpochInfo.BLSPublicKeys[0][2] ^= 1
				} else {
					x.EpochInfo = &EpochInfoRLP{}
				}
			},
		} {
			t.Run(fmt.Sprintf("height%d/%s", i+1, name), func(t *testing.T) {
				var x WBFTExtraRLP
				require.NoError(t, rlp.DecodeBytes(original.Extra, &x))
				mutate(&x)
				h := types.CopyHeader(&original)
				h.Extra, e = rlp.EncodeToBytes(&x)
				require.NoError(t, e)
				bad, e := rlp.EncodeToBytes(h)
				require.NoError(t, e)
				_, e = v.Verify(t.Context(), state, bad)
				require.Error(t, e)
			})
		}
		next, e := v.Verify(t.Context(), state, raw)
		require.NoError(t, e)
		if i == 1 {
			require.NotEqual(t, state.Next, next.Next, "epoch certificate must authenticate the replacement committee")
		}
		if i == 2 {
			require.Equal(t, state.Next, next.Applied, "new committee starts after epoch block")
		}
		state = next
	}
	for _, mutate := range []func(*TrustPolicy){
		func(p *TrustPolicy) { p.Protocol = "arc" }, func(p *TrustPolicy) { p.Fork = "cancun" },
		func(p *TrustPolicy) { p.Chain.ID = "1" }, func(p *TrustPolicy) { p.Chain.Genesis[0] ^= 1 },
		func(p *TrustPolicy) { p.Applied[1] = p.Applied[0] }, func(p *TrustPolicy) { p.Next[0].PublicKey = []byte{1} },
	} {
		var p TrustPolicy
		require.NoError(t, json.Unmarshal(mustJSON(t, f.Policy), &p))
		mutate(&p)
		candidate, e := NewIndependent(p)
		if e == nil {
			_, e = candidate.Verify(t.Context(), candidate.Anchor(), f.Headers[0])
		}
		require.Error(t, e)
	}
}
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, e := json.Marshal(value)
	require.NoError(t, e)
	return raw
}

func TestIndependentTrustAnchorCommitteeConsistency(t *testing.T) {
	data, e := os.ReadFile("../../../testdata/execution/consensus/native.json")
	require.NoError(t, e)
	var f struct {
		Policy  TrustPolicy
		Headers [][]byte
	}
	require.NoError(t, json.Unmarshal(data, &f))
	// These are all well-formed keys: admission must bind the trusted committee
	// to the authenticated epoch header, before it ever sees a descendant.
	f.Policy.Next[0], f.Policy.Next[1] = f.Policy.Next[1], f.Policy.Next[0]
	_, e = NewIndependent(f.Policy)
	require.Error(t, e)
}
