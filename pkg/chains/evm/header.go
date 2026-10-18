package evm

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// HeaderFields holds header commitments added after the upstream Header
// used by this build. The chain-neutral model stores them as an extension.
type HeaderFields struct {
	BlockAccessListHash *common.Hash `json:"blockAccessListHash,omitempty"`
	SlotNumber          *uint64      `json:"slotNumber,omitempty"`
}

var headerFieldsKey = model.NewExtKey("evm.header")

func init() {
	model.RegisterExtCodec(headerFieldsKey, model.ExtCodec{
		Encode: func(v any) ([]byte, error) {
			fields, ok := v.(*HeaderFields)
			if !ok {
				return nil, fmt.Errorf("evm: header extension has type %T", v)
			}
			return json.Marshal(fields)
		},
		Decode: func(data []byte) (any, error) {
			var fields HeaderFields
			if err := json.Unmarshal(data, &fields); err != nil {
				return nil, err
			}
			return &fields, nil
		},
	})
}

// HeaderFieldsOf returns the additional EVM header fields of a block.
func HeaderFieldsOf(b *model.Block) (*HeaderFields, bool) {
	fields, ok := b.Ext.Get(headerFieldsKey).(*HeaderFields)
	return fields, ok
}

const upstreamHeaderFields = 21 // through EIP-7685 requestsHash

// hashHeader keeps custom chain hash rules for existing headers. Extra
// commitments use Ethereum's canonical RLP layout; a custom hash rule
// must explicitly implement its newer header layout in its own profile.
func (p *Profile) hashHeader(h *types.Header, fields *HeaderFields) (common.Hash, error) {
	if fields != nil && p.customHeaderHash {
		return common.Hash{}, fmt.Errorf("evm: additional header fields require a matching custom hash rule")
	}
	if fields == nil {
		return p.headerHash(h), nil
	}
	raw, err := encodeHeader(h, fields)
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(raw), nil
}

func encodeHeader(h *types.Header, fields *HeaderFields) ([]byte, error) {
	raw, err := rlp.EncodeToBytes(h)
	if err != nil {
		return nil, err
	}
	if fields == nil {
		return raw, nil
	}
	var parts []rlp.RawValue
	if err := rlp.DecodeBytes(raw, &parts); err != nil {
		return nil, err
	}
	// Optional older fields retain their positions when a later field is
	// present. Empty byte strings encode their missing values.
	for len(parts) < upstreamHeaderFields {
		parts = append(parts, rlp.RawValue{0x80})
	}
	accessList := rlp.RawValue{0x80}
	if fields.BlockAccessListHash != nil {
		accessList, err = rlp.EncodeToBytes(*fields.BlockAccessListHash)
		if err != nil {
			return nil, err
		}
	}
	parts = append(parts, accessList)
	if fields.SlotNumber != nil {
		slot, err := rlp.EncodeToBytes(*fields.SlotNumber)
		if err != nil {
			return nil, err
		}
		parts = append(parts, slot)
	}
	return rlp.EncodeToBytes(parts)
}

// EncodeHeaderJSON ignores transaction bodies and requires an authenticated,
// bounded header. This is used only to bootstrap a tracked parent checkpoint.
func (p *Profile) EncodeHeaderJSON(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 4<<20 {
		return nil, fmt.Errorf("evm: header JSON size")
	}
	var h types.Header
	if e := json.Unmarshal(raw, &h); e != nil {
		return nil, e
	}
	// Do not decode/retain a potentially large transaction array.
	var fields struct {
		Hash           common.Hash     `json:"hash"`
		AccessListHash *common.Hash    `json:"blockAccessListHash"`
		SlotNumber     *hexutil.Uint64 `json:"slotNumber"`
	}
	if e := json.Unmarshal(raw, &fields); e != nil {
		return nil, e
	}
	var extra *HeaderFields
	if fields.AccessListHash != nil || fields.SlotNumber != nil {
		extra = &HeaderFields{BlockAccessListHash: fields.AccessListHash}
		if fields.SlotNumber != nil {
			slot := uint64(*fields.SlotNumber)
			extra.SlotNumber = &slot
		}
	}
	if h.Number == nil || !h.Number.IsUint64() {
		return nil, fmt.Errorf("evm: header number")
	}
	hash, e := p.hashHeader(&h, extra)
	if e != nil {
		return nil, e
	}
	if hash != fields.Hash {
		return nil, ErrHashMismatch
	}
	return encodeHeader(&h, extra)
}

func (p *Profile) DecodeHeaderRLP(raw []byte) (*model.Block, error) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return nil, fmt.Errorf("evm: header RLP size")
	}
	h, fields, e := p.decodeHeader(raw)
	if e != nil {
		return nil, e
	}
	if h.Number == nil || !h.Number.IsUint64() {
		return nil, fmt.Errorf("evm: header number")
	}
	b := headerToModel(h)
	b.Hash, e = p.hashHeader(h, fields)
	if e != nil {
		return nil, e
	}
	if fields != nil {
		b.Ext.Set(headerFieldsKey, fields)
	}
	return b, nil
}

// decodeHeader accepts the two known suffix fields without accepting an
// unknown future header layout. Existing upstream/custom headers retain
// their decoding and hashing rules.
func (p *Profile) decodeHeader(raw []byte) (*types.Header, *HeaderFields, error) {
	var parts []rlp.RawValue
	if err := rlp.DecodeBytes(raw, &parts); err != nil {
		return nil, nil, err
	}
	var fields *HeaderFields
	base := raw
	if len(parts) > upstreamHeaderFields {
		if len(parts) > upstreamHeaderFields+2 {
			return nil, nil, fmt.Errorf("evm: unsupported header field count %d", len(parts))
		}
		fields = &HeaderFields{}
		if len(parts[upstreamHeaderFields]) != 1 || parts[upstreamHeaderFields][0] != 0x80 {
			fields.BlockAccessListHash = new(common.Hash)
			if err := rlp.DecodeBytes(parts[upstreamHeaderFields], fields.BlockAccessListHash); err != nil {
				return nil, nil, err
			}
		}
		if len(parts) == upstreamHeaderFields+2 {
			fields.SlotNumber = new(uint64)
			if err := rlp.DecodeBytes(parts[upstreamHeaderFields+1], fields.SlotNumber); err != nil {
				return nil, nil, err
			}
		}
		var err error
		base, err = rlp.EncodeToBytes(parts[:upstreamHeaderFields])
		if err != nil {
			return nil, nil, err
		}
	}
	var h types.Header
	if err := rlp.DecodeBytes(base, &h); err != nil {
		return nil, nil, err
	}
	return &h, fields, nil
}
