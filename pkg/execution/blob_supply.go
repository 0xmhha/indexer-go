package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/exporter"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob/proof"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/blobdata"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/params"
)

const EngineBlobContract = "ethereum-cancun-full-blobs.engine-v1"
const DevEngineBlobContract = "evm-dev-cancun-full-blobs.engine-v1"

// BlobSupplyPolicy is hashed into spool.Options.Admission BEFORE opening or
// recovering a source. Changing any field requires a new source. The dev
// contract supports explicit chain 1337 native-library fixtures, never mainnet.
// Cancun only is deliberate: other forks need their own audited contract.
type BlobSupplyPolicy struct {
	Contract     string         `json:"contract"`
	Chain        execwire.Chain `json:"chain"`
	Provider     string         `json:"provider"`
	MaxLagBlocks uint64         `json:"maxLagBlocks"`
}

func (p BlobSupplyPolicy) Admission() (execwire.Hash, error) {
	if p.MaxLagBlocks == 0 || p.MaxLagBlocks > 8192 {
		return execwire.Hash{}, port.ErrInvalidData
	}
	switch p.Contract {
	case EngineBlobContract:
		if p.Chain.ID != "1" || p.Chain.Genesis != execwire.Hash(params.MainnetGenesisHash) {
			return execwire.Hash{}, evmblob.ErrUnsupported
		}
	case DevEngineBlobContract:
		if p.Chain.ID != "1337" || p.Chain.Genesis == (execwire.Hash{}) {
			return execwire.Hash{}, evmblob.ErrUnsupported
		}
	default:
		return execwire.Hash{}, evmblob.ErrUnsupported
	}
	if e := execwire.ValidateBlobBundle(&execwire.BlobBundle{Version: 1, Format: execwire.FullBlobFormat, Profile: "evm", Provider: p.Provider, Response: []byte{1}}); e != nil {
		return execwire.Hash{}, e
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return execwire.Hash{}, e
	}
	return execwire.Hash(sha256.Sum256(append([]byte("execution-blob-supply-v1\x00"), raw...))), nil
}

// AcquireEngineBlobInput produces a new immutable EXR1, before admission. The
// supplied client must bypass caches, share the chain's bounded HTTP pool, and
// honor ctx. This grants no ACK, rewind, generation or deletion authority.
func AcquireEngineBlobInput(ctx context.Context, profile chains.BinaryProfile, policy BlobSupplyPolicy, payload []byte, caller evmblob.EngineBlobCaller) ([]byte, error) {
	if _, e := policy.Admission(); e != nil {
		return nil, e
	}
	if profile == nil || caller == nil {
		return nil, port.ErrInvalidData
	}
	capability, e := chains.BlobCapabilityOf(profile)
	if e != nil {
		return nil, e
	}
	if capability.Status == model.BlobUnsupported || profile.ID() != "evm" {
		return nil, evmblob.ErrUnsupported
	}
	prepared, e := PrepareContext(ctx, profile, policy.Chain, payload, false)
	if e != nil {
		return nil, e
	}
	if prepared.result.BlobBundle != nil {
		return nil, port.ErrExecutionConflict
	}
	if policy.Contract == EngineBlobContract && (prepared.block.Time < *params.MainnetChainConfig.CancunTime || prepared.block.Time >= *params.MainnetChainConfig.PragueTime) {
		return nil, evmblob.ErrUnsupported
	}
	// Cancun has exactly 20 header fields. Reject both older missing fork
	// commitments and Prague/future suffixes even on the explicit dev chain.
	parts, e := proof.SplitList(prepared.result.BlockRLP, 4)
	if e != nil || len(parts) < 3 {
		return nil, evmblob.ErrUnsupported
	}
	fields, e := proof.SplitList(parts[0], 20)
	block := prepared.block
	if e != nil || len(fields) != 20 || block.Difficulty == nil || block.Difficulty.Sign() != 0 || block.WithdrawalsRoot == nil || block.BlobGasUsed == nil || block.ExcessBlobGas == nil || block.ParentBeaconRoot == nil || block.RequestsHash != nil {
		return nil, evmblob.ErrUnsupported
	}
	refs, e := proof.References(prepared.block)
	if e != nil {
		return nil, e
	}
	if len(refs) > 6 {
		return nil, evmblob.ErrUnsupported
	}
	// Verify chain/genesis, bounded head window and fresh canonical identity.
	// No hash-only RPC response can change the original header/body commitment.
	var id hexutil.Big
	if e := caller.CallContext(ctx, &id, "eth_chainId"); e != nil {
		return nil, e
	}
	if id.ToInt().String() != policy.Chain.ID {
		return nil, port.ErrExecutionConflict
	}
	hashAt := func(selector string) (common.Hash, uint64, error) {
		var raw json.RawMessage
		if e := caller.CallContext(ctx, &raw, "eth_getBlockByNumber", selector, false); e != nil {
			return common.Hash{}, 0, e
		}
		codec, ok := profile.(chains.HeaderCodec)
		if !ok {
			return common.Hash{}, 0, port.ErrInvalidData
		}
		encoded, e := codec.EncodeHeaderJSON(raw)
		if e != nil {
			return common.Hash{}, 0, e
		}
		b, e := codec.DecodeHeaderRLP(encoded)
		if e != nil {
			return common.Hash{}, 0, e
		}
		return b.Hash, b.Number, nil
	}
	genesis, number, e := hashAt("0x0")
	if e != nil {
		return nil, e
	}
	if number != 0 || execwire.Hash(genesis) != policy.Chain.Genesis {
		return nil, port.ErrExecutionConflict
	}
	_, head, e := hashAt("latest")
	if e != nil {
		return nil, e
	}
	if head < prepared.Ref().Height || head-prepared.Ref().Height > policy.MaxLagBlocks {
		return nil, proof.ErrUnavailable
	}
	raw, e := evmblob.ReadProvider(ctx, evmblob.EngineV1{Caller: caller}, blobdata.Block(policy.Chain, prepared.Ref()), refs)
	if e != nil {
		return nil, e
	}
	out, e := AttachBlobInput(ctx, profile, policy.Chain, payload, policy.Provider, raw)
	if e != nil {
		return nil, e
	}
	// Acquisition's final uncached check is additional to, and cannot replace,
	// the receiver's existing NEW fresh -> generation fence -> COMMIT.
	hash, number, e := hashAt(fmt.Sprintf("0x%x", prepared.Ref().Height))
	if e != nil {
		return nil, e
	}
	if number != prepared.Ref().Height || hash != prepared.block.Hash {
		return nil, exporter.ErrCanonicalChanged
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	return out, nil
}

// EngineBlobBackend decorates an existing two-pass producer. Collector's head,
// range, post-Replay canonical check, quota, restart and exact ACK stay owned by
// exporter/spool. Replay enrichment finishes before Collector.Append seals bytes.
type EngineBlobBackend struct {
	backend exporter.Backend
	profile chains.BinaryProfile
	policy  BlobSupplyPolicy
	caller  evmblob.EngineBlobCaller
}

func NewEngineBlobBackend(profile chains.BinaryProfile, policy BlobSupplyPolicy, caller evmblob.EngineBlobCaller, backend exporter.Backend) (*EngineBlobBackend, error) {
	if _, e := policy.Admission(); e != nil {
		return nil, e
	}
	if profile == nil || caller == nil || backend == nil {
		return nil, port.ErrInvalidData
	}
	capability, e := chains.BlobCapabilityOf(profile)
	if e != nil {
		return nil, e
	}
	if profile.ID() != "evm" || capability.Status == model.BlobUnsupported {
		return nil, evmblob.ErrUnsupported
	}
	return &EngineBlobBackend{backend: backend, profile: profile, policy: policy, caller: caller}, nil
}
func (b *EngineBlobBackend) Head(ctx context.Context) (execwire.Ref, error) {
	return b.backend.Head(ctx)
}
func (b *EngineBlobBackend) Canonical(ctx context.Context, height uint64) (execwire.Hash, error) {
	return b.backend.Canonical(ctx, height)
}

// OpenSpool pins the complete copied policy before any recovery or mutation.
func (b *EngineBlobBackend) OpenSpool(ctx context.Context, options spool.Options) (*spool.Store, error) {
	admission, e := b.policy.Admission()
	if e != nil {
		return nil, e
	}
	if options.Chain != b.policy.Chain || options.Admission != nil && *options.Admission != admission {
		return nil, spool.ErrConflict
	}
	options.Admission = &admission
	return spool.Open(ctx, options)
}

func (b *EngineBlobBackend) Replay(ctx context.Context, height, generation uint64) (execwire.Result, error) {
	if b == nil || b.backend == nil {
		return execwire.Result{}, port.ErrInvalidData
	}
	if _, e := b.policy.Admission(); e != nil {
		return execwire.Result{}, e
	}
	r, e := b.backend.Replay(ctx, height, generation)
	if e != nil {
		return execwire.Result{}, e
	}
	if r.Ref.Height != height || r.Ref.Generation != generation {
		return execwire.Result{}, port.ErrExecutionConflict
	}
	raw, e := execwire.MarshalResult(r)
	if e != nil {
		return execwire.Result{}, e
	}
	raw, e = AcquireEngineBlobInput(ctx, b.profile, b.policy, raw, b.caller)
	if e != nil {
		return execwire.Result{}, errors.Join(proof.ErrUnavailable, e)
	}
	return execwire.UnmarshalResult(raw)
}

func AcquireEngineBlobFiles(ctx context.Context, profile chains.BinaryProfile, policy BlobSupplyPolicy, input, output string, caller evmblob.EngineBlobCaller) (InputAttachment, error) {
	return composePrivateInput(ctx, input, "", output, 0, func(payload, _ []byte) ([]byte, error) {
		return AcquireEngineBlobInput(ctx, profile, policy, payload, caller)
	})
}

// ReadEngineJWTSecret uses the same private-file identity checks as EXR1 input.
func ReadEngineJWTSecret(ctx context.Context, path string) ([32]byte, error) {
	var secret [32]byte
	raw, _, e := readPrivateBlobFile(ctx, path, 128)
	if e != nil {
		return secret, e
	}
	text := strings.TrimPrefix(strings.TrimSpace(string(raw)), "0x")
	if len(text) != 64 {
		return secret, port.ErrInvalidData
	}
	decoded, e := hex.DecodeString(text)
	if e != nil {
		return secret, port.ErrInvalidData
	}
	copy(secret[:], decoded)
	return secret, nil
}
