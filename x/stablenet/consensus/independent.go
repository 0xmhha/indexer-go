package consensus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/x/stablenet"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	blst "github.com/supranational/blst/bindings/go"
	"io"
	"math"
	"math/big"
)

const IndependentProtocol = "stablenet-wbft-4d17eea018c8"

var ErrIndependent = errors.New("WBFT independent consensus: invalid proof or trust policy")

type TrustedValidator struct {
	Address   common.Address
	PublicKey []byte
}
type TrustPolicy struct {
	Protocol                                         string
	Chain                                            execwire.Chain
	Fork                                             string
	EpochLength, EpochOffset, BlockPeriod, MaxHeight uint64
	AnchorHeader                                     []byte
	Applied, Next                                    []TrustedValidator
}
type IndependentState struct {
	Header        []byte
	Applied, Next []TrustedValidator
}
type Independent struct{ policy TrustPolicy }

// NewIndependent freezes an externally authenticated anchor and the entire
// supported fork interval. RPC never supplies or upgrades this trust policy.
func NewIndependent(p TrustPolicy) (*Independent, error) {
	encoded, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var frozen TrustPolicy
	if err = json.Unmarshal(encoded, &frozen); err != nil {
		return nil, err
	}
	p = frozen
	id, ok := new(big.Int).SetString(p.Chain.ID, 10)
	if !ok || id.Sign() <= 0 || id.String() != p.Chain.ID || p.Chain.Genesis == (execwire.Hash{}) || p.Protocol != IndependentProtocol || p.Fork != "anzeon-v1" || p.EpochLength == 0 || p.EpochLength > 1000000 || p.BlockPeriod == 0 || p.MaxHeight > math.MaxInt64 {
		return nil, ErrIndependent
	}
	h, err := independentHeader(p.AnchorHeader)
	if err != nil || h.Number.Uint64() >= p.MaxHeight || p.EpochOffset > h.Number.Uint64() {
		return nil, ErrIndependent
	}
	if h.Number.Sign() == 0 && execwire.Hash(stablenet.HeaderHash(h)) != p.Chain.Genesis {
		return nil, ErrIndependent
	}
	if err = validateValidators(context.Background(), p.Applied); err != nil {
		return nil, err
	}
	if err = validateValidators(context.Background(), p.Next); err != nil {
		return nil, err
	}

	var extra WBFTExtraRLP
	if err = rlp.DecodeBytes(h.Extra, &extra); err != nil {
		return nil, ErrIndependent
	}
	expected := p.Applied
	epoch := (h.Number.Uint64()-p.EpochOffset)%p.EpochLength == 0
	if epoch != (extra.EpochInfo != nil) {
		return nil, ErrIndependent
	}
	if epoch {
		expected, err = epochValidators(extra.EpochInfo)
		if err != nil {
			return nil, err
		}
	}
	expectedBytes, _ := json.Marshal(expected)
	nextBytes, _ := json.Marshal(p.Next)
	if !bytes.Equal(expectedBytes, nextBytes) {
		return nil, ErrIndependent
	}
	if h.Number.Sign() == 0 {
		appliedBytes, _ := json.Marshal(p.Applied)
		if !bytes.Equal(appliedBytes, nextBytes) {
			return nil, ErrIndependent
		}
	}
	return &Independent{policy: p}, nil
}
func (v *Independent) Anchor() IndependentState {
	raw, _ := json.Marshal(IndependentState{v.policy.AnchorHeader, v.policy.Applied, v.policy.Next})
	var state IndependentState
	_ = json.Unmarshal(raw, &state)
	return state
}

// Verify consumes exactly one descendant. The caller persists the returned
// state only in the transaction that publishes the same block. No mutable
// verifier cache can move a finality boundary after a failed fresh read.
func (v *Independent) Verify(ctx context.Context, s IndependentState, raw []byte) (IndependentState, error) {
	if err := ctx.Err(); err != nil {
		return IndependentState{}, err
	}
	parent, err := independentHeader(s.Header)
	if err != nil {
		return IndependentState{}, err
	}
	h, err := independentHeader(raw)
	if err != nil {
		return IndependentState{}, err
	}
	n := h.Number.Uint64()
	if n != parent.Number.Uint64()+1 || n > v.policy.MaxHeight || h.ParentHash != stablenet.HeaderHash(parent) || parent.Time > math.MaxUint64-v.policy.BlockPeriod || h.Time < parent.Time+v.policy.BlockPeriod {
		return IndependentState{}, ErrIndependent
	}
	if err = validateValidators(ctx, s.Applied); err != nil {
		return IndependentState{}, err
	}
	if err = validateValidators(ctx, s.Next); err != nil {
		return IndependentState{}, err
	}
	var extra WBFTExtraRLP
	if err = rlp.DecodeBytes(h.Extra, &extra); err != nil {
		return IndependentState{}, ErrIndependent
	}
	if err = verifyIndependentSeal(ctx, h, extra.Round, extra.PreparedSeal, s.Next, 0); err != nil {
		return IndependentState{}, err
	}
	if err = verifyIndependentSeal(ctx, h, extra.Round, extra.CommittedSeal, s.Next, 1); err != nil {
		return IndependentState{}, err
	}
	if n > 1 {
		if err = verifyIndependentSeal(ctx, parent, extra.PrevRound, extra.PrevPreparedSeal, s.Applied, 0); err != nil {
			return IndependentState{}, err
		}
		if err = verifyIndependentSeal(ctx, parent, extra.PrevRound, extra.PrevCommittedSeal, s.Applied, 1); err != nil {
			return IndependentState{}, err
		}
	}
	// The native backend signs keccak(makeRandaoData); the latter is itself
	// keccak(chainID bytes || anzeon version 1 || number bytes).
	id, _ := new(big.Int).SetString(v.policy.Chain.ID, 10)
	message := crypto.Keccak256(append(append(id.Bytes(), byte(1)), h.Number.Bytes()...))
	pub, err := crypto.SigToPub(crypto.Keccak256(message), extra.RandaoReveal)
	if err != nil || crypto.PubkeyToAddress(*pub) != h.Coinbase {
		return IndependentState{}, ErrIndependent
	}
	authorized := false
	for _, val := range s.Next {
		authorized = authorized || val.Address == h.Coinbase
	}
	if !authorized {
		return IndependentState{}, ErrIndependent
	}
	mix := new(big.Int).Xor(new(big.Int).SetBytes(parent.MixDigest[:]), new(big.Int).SetBytes(crypto.Keccak256(extra.RandaoReveal)))
	if common.BigToHash(mix) != h.MixDigest {
		return IndependentState{}, ErrIndependent
	}
	next := s.Next
	epoch := (n-v.policy.EpochOffset)%v.policy.EpochLength == 0
	if epoch != (extra.EpochInfo != nil) {
		return IndependentState{}, ErrIndependent
	}
	if epoch {
		next, err = epochValidators(extra.EpochInfo)
		if err != nil {
			return IndependentState{}, err
		}
		if err = validateValidators(ctx, next); err != nil {
			return IndependentState{}, err
		}
	}
	if err = ctx.Err(); err != nil {
		return IndependentState{}, err
	}
	result := IndependentState{Header: bytes.Clone(raw), Applied: s.Next, Next: next}
	encoded, err := json.Marshal(result)
	if err != nil {
		return IndependentState{}, err
	}
	var owned IndependentState
	err = json.Unmarshal(encoded, &owned)
	return owned, err
}

const MaxIndependentHeaderBytes = 256 << 10
const MaxIndependentValidators = 256

var independentDST = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")

func independentHeader(raw []byte) (*types.Header, error) {
	if len(raw) == 0 || len(raw) > MaxIndependentHeaderBytes {
		return nil, ErrIndependent
	}
	var h types.Header
	if err := rlp.DecodeBytes(raw, &h); err != nil {
		return nil, ErrIndependent
	}
	if h.Number == nil || !h.Number.IsUint64() || h.Number.Uint64() > math.MaxInt64 || h.Difficulty == nil || h.Difficulty.Cmp(big.NewInt(1)) != 0 || h.UncleHash != types.EmptyUncleHash || h.WithdrawalsHash != nil || h.ExcessBlobGas != nil || h.BlobGasUsed != nil || h.ParentBeaconRoot != nil || h.RequestsHash != nil || h.GasUsed > h.GasLimit || h.GasLimit > math.MaxInt64 {
		return nil, ErrIndependent
	}
	var extra WBFTExtraRLP
	if err := rlp.DecodeBytes(h.Extra, &extra); err != nil {
		return nil, ErrIndependent
	}
	return &h, nil
}
func validateValidators(ctx context.Context, vs []TrustedValidator) error {
	if len(vs) == 0 || len(vs) > MaxIndependentValidators {
		return ErrIndependent
	}
	addresses := make(map[common.Address]bool, len(vs))
	keys := make(map[string]bool, len(vs))
	for _, v := range vs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if v.Address == (common.Address{}) || addresses[v.Address] || keys[string(v.PublicKey)] || len(v.PublicKey) != 48 {
			return ErrIndependent
		}
		pk := new(blst.P1Affine).Uncompress(v.PublicKey)
		if pk == nil || !pk.KeyValidate() {
			return ErrIndependent
		}
		addresses[v.Address] = true
		keys[string(v.PublicKey)] = true
	}
	return nil
}
func epochValidators(e *EpochInfoRLP) ([]TrustedValidator, error) {
	if e == nil || len(e.Candidates) == 0 || len(e.Candidates) > MaxIndependentValidators || len(e.Validators) == 0 || len(e.Validators) > MaxIndependentValidators || len(e.BLSPublicKeys) != len(e.Validators) {
		return nil, ErrIndependent
	}
	seen := map[string]bool{}
	for _, c := range e.Candidates {
		if c == nil || len(c.Addr) != 20 || seen[string(c.Addr)] {
			return nil, ErrIndependent
		}
		seen[string(c.Addr)] = true
	}
	vs := make([]TrustedValidator, len(e.Validators))
	for i, index := range e.Validators {
		if int(index) >= len(e.Candidates) {
			return nil, ErrIndependent
		}
		vs[i] = TrustedValidator{common.BytesToAddress(e.Candidates[index].Addr), e.BLSPublicKeys[i]}
	}
	return vs, nil
}
func verifyIndependentSeal(ctx context.Context, h *types.Header, round uint32, seal *WBFTAggregatedSealRLP, vs []TrustedValidator, kind byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if seal == nil || len(seal.Signature) != 96 || len(seal.Sealers) == 0 || len(seal.Sealers) > (len(vs)+7)/8 {
		return ErrIndependent
	}
	var keys []*blst.P1Affine
	for i, b := range seal.Sealers {
		for bit := 0; bit < 8; bit++ {
			if b&(1<<bit) == 0 {
				continue
			}
			index := i*8 + bit
			if index >= len(vs) {
				return ErrIndependent
			}
			keys = append(keys, new(blst.P1Affine).Uncompress(vs[index].PublicKey))
		}
	}
	// Native QuorumSize = ceil(N - (N-1)/3), not floor(2N/3).
	if len(keys) < (2*len(vs)+3)/3 {
		return ErrIndependent
	}
	var fields []rlp.RawValue
	if err := rlp.DecodeBytes(h.Extra, &fields); err != nil || len(fields) != 10 {
		return ErrIndependent
	}
	fields[5], _ = rlp.EncodeToBytes(round)
	fields[6] = rlp.RawValue{0xc0}
	fields[7] = rlp.RawValue{0xc0}
	copyHeader := types.CopyHeader(h)
	copyHeader.Extra, _ = rlp.EncodeToBytes(fields)
	hash := copyHeader.Hash()
	message := crypto.Keccak256(append(hash.Bytes(), kind))
	signature := new(blst.P2Affine).Uncompress(seal.Signature)
	if signature == nil || !signature.SigValidate(true) || !signature.FastAggregateVerify(true, keys, message, independentDST) {
		return ErrIndependent
	}
	return ctx.Err()
}

// DecodeTrustPolicy accepts one strict JSON object, including no unknown keys.
func DecodeTrustPolicy(raw []byte) (TrustPolicy, error) {
	var p TrustPolicy
	if len(raw) > 1<<20 {
		return p, ErrIndependent
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return p, err
	}
	var tail any
	if err := decoder.Decode(&tail); err != io.EOF {
		return p, ErrIndependent
	}
	return p, nil
}

func init() {
	chains.RegisterConsensus(IndependentProtocol, func(chain execwire.Chain, policy chains.ConsensusPolicy) (chains.ConsensusVerifier, error) {
		if policy.Profile != "stablenet" {
			return nil, ErrIndependent
		}
		p, err := DecodeTrustPolicy([]byte(policy.Parameters))
		if err != nil {
			return nil, err
		}
		if p.Chain != chain || p.Protocol != policy.Protocol {
			return nil, ErrIndependent
		}
		return NewIndependent(p)
	})
}
func (v *Independent) AnchorPoint() (chains.ConsensusPoint, error) {
	return independentPoint(v.Anchor())
}
func independentPoint(s IndependentState) (chains.ConsensusPoint, error) {
	h, err := independentHeader(s.Header)
	if err != nil {
		return chains.ConsensusPoint{}, err
	}
	raw, err := json.Marshal(s)
	return chains.ConsensusPoint{Height: h.Number.Uint64(), Hash: execwire.Hash(stablenet.HeaderHash(h)), Root: execwire.Hash(h.Root), State: raw}, err
}
func (v *Independent) VerifyPoint(ctx context.Context, state json.RawMessage, header []byte) (chains.ConsensusPoint, error) {
	if len(state) > 1<<20 {
		return chains.ConsensusPoint{}, ErrIndependent
	}
	var s IndependentState
	if err := json.Unmarshal(state, &s); err != nil {
		return chains.ConsensusPoint{}, err
	}
	next, err := v.Verify(ctx, s, header)
	if err != nil {
		return chains.ConsensusPoint{}, err
	}
	return independentPoint(next)
}

// RestorePoint validates persisted state before any admission or ACK-only
// recovery. PostgreSQL additionally binds the exact bytes with a checksum.
func (v *Independent) RestorePoint(ctx context.Context, raw json.RawMessage) (chains.ConsensusPoint, error) {
	if err := ctx.Err(); err != nil {
		return chains.ConsensusPoint{}, err
	}
	if len(raw) == 0 || len(raw) > 1<<20 {
		return chains.ConsensusPoint{}, ErrIndependent
	}
	var s IndependentState
	if err := json.Unmarshal(raw, &s); err != nil {
		return chains.ConsensusPoint{}, err
	}
	point, err := independentPoint(s)
	if err != nil {
		return chains.ConsensusPoint{}, err
	}
	anchor, err := v.AnchorPoint()
	if err != nil {
		return chains.ConsensusPoint{}, err
	}
	if point.Height < anchor.Height || point.Height > v.policy.MaxHeight {
		return chains.ConsensusPoint{}, ErrIndependent
	}
	if point.Height == anchor.Height {
		expected, _ := json.Marshal(v.Anchor())
		actual, _ := json.Marshal(s)
		if !bytes.Equal(expected, actual) {
			return chains.ConsensusPoint{}, ErrIndependent
		}
		return point, nil
	}
	if err = validateValidators(ctx, s.Applied); err != nil {
		return chains.ConsensusPoint{}, err
	}
	if err = validateValidators(ctx, s.Next); err != nil {
		return chains.ConsensusPoint{}, err
	}
	h, err := independentHeader(s.Header)
	if err != nil {
		return chains.ConsensusPoint{}, err
	}
	var extra WBFTExtraRLP
	if err = rlp.DecodeBytes(h.Extra, &extra); err != nil {
		return chains.ConsensusPoint{}, err
	}
	if err = verifyIndependentSeal(ctx, h, extra.Round, extra.PreparedSeal, s.Applied, 0); err != nil {
		return chains.ConsensusPoint{}, err
	}
	if err = verifyIndependentSeal(ctx, h, extra.Round, extra.CommittedSeal, s.Applied, 1); err != nil {
		return chains.ConsensusPoint{}, err
	}
	next := s.Applied
	epoch := (point.Height-v.policy.EpochOffset)%v.policy.EpochLength == 0
	if epoch != (extra.EpochInfo != nil) {
		return chains.ConsensusPoint{}, ErrIndependent
	}
	if epoch {
		next, err = epochValidators(extra.EpochInfo)
		if err != nil {
			return chains.ConsensusPoint{}, err
		}
	}
	expected, _ := json.Marshal(next)
	actual, _ := json.Marshal(s.Next)
	if !bytes.Equal(expected, actual) {
		return chains.ConsensusPoint{}, ErrIndependent
	}
	return point, ctx.Err()
}
