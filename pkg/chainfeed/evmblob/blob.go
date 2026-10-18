// Package evmblob authenticates complete EIP-4844 blobs against captured
// execution transactions. It does not implement beacon consensus, PeerDAS,
// execution or an ETH/68 blob transport.
package evmblob

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/rlp"
)

const Codec = "eth.full-blobs.rlp.v1"
const Format = "eip4844.full-blob-proof.v1"

// Operational bounds, not a chain's fork-dependent consensus limits. Larger
// blocks fail closed and require an explicitly versioned storage contract.
const MaxBlobs = 32
const MaxResponseBytes = MaxBlobs*(131072+128) + 1024

var (
	ErrBlob        = errors.New("invalid full blob input")
	ErrUnavailable = errors.New("full blob source unavailable")
)

// Entry.Index is the block-wide ordinal, in transaction/hash-list order.
// Repeated versioned hashes at different positions are legitimate.
type Entry struct {
	Index      uint64             `json:"index"`
	Blob       kzg4844.Blob       `json:"blob"`
	Commitment kzg4844.Commitment `json:"commitment"`
	Proof      kzg4844.Proof      `json:"proof"`
}

// Keep the blob's JSON representation hexadecimal even when an Entry is
// passed by value (Geth Blob's text marshaler has a pointer receiver).
func (v Entry) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Index      uint64             `json:"index"`
		Blob       hexutil.Bytes      `json:"blob"`
		Commitment kzg4844.Commitment `json:"commitment"`
		Proof      kzg4844.Proof      `json:"proof"`
	}{v.Index, v.Blob[:], v.Commitment, v.Proof})
}

// Response is this module's relay envelope, NOT a beacon BlobSidecar SSZ
// object or a pooled-transaction envelope. Version 1 contains full blobs and
// one blob proof each. Cells, samples and other proof versions are rejected.
type Response struct {
	Version uint64
	Block   model.FeedBlock
	Entries []Entry
}

type Reference struct {
	Index            uint32      `json:"index"`
	TransactionIndex uint32      `json:"transactionIndex"`
	TransactionHash  common.Hash `json:"transactionHash"`
	BlobIndex        uint32      `json:"blobIndex"`
	VersionedHash    common.Hash `json:"versionedHash"`
}

// References requires an already profile/commitment-verified captured block.
// This is not independent execution validation or fork applicability detection.
func References(b *model.Block) ([]Reference, error) {
	if b == nil || len(b.Transactions) > 32768 {
		return nil, ErrBlob
	}
	refs := make([]Reference, 0)
	for ti, tx := range b.Transactions {
		if tx == nil || tx.Opaque || tx.BlockHash != b.Hash || tx.BlockNumber != b.Number || tx.Index != uint(ti) {
			return nil, ErrBlob
		}
		if tx.Type != 3 {
			if len(tx.BlobHashes) != 0 {
				return nil, ErrBlob
			}
			continue
		}
		if len(tx.BlobHashes) == 0 || len(tx.BlobHashes) > MaxBlobs-len(refs) {
			return nil, ErrBlob
		}
		for bi, h := range tx.BlobHashes {
			if h[0] != 1 {
				return nil, ErrBlob
			}
			refs = append(refs, Reference{Index: uint32(len(refs)), TransactionIndex: uint32(ti), TransactionHash: tx.Hash, BlobIndex: uint32(bi), VersionedHash: h})
		}
	}
	if len(refs) > 0 && (b.BlobGasUsed == nil || b.ExcessBlobGas == nil) {
		return nil, ErrBlob
	}
	if b.BlobGasUsed != nil && *b.BlobGasUsed != uint64(len(refs))*131072 {
		return nil, ErrBlob
	}
	return refs, nil
}

// Decode bounds bytes and list cardinality before allocating any blob arrays.
// rlp.DecodeBytes then enforces canonical integers, exact fixed byte lengths,
// field counts and the absence of trailing data.
func Decode(raw []byte) (Response, error) {
	var out Response
	if len(raw) == 0 || len(raw) > MaxResponseBytes {
		return out, ErrBlob
	}
	fields, e := splitList(raw, 3)
	if e != nil || len(fields) != 3 {
		return out, ErrBlob
	}
	entries, e := splitList(fields[2], MaxBlobs)
	if e != nil {
		return out, e
	}
	for _, v := range entries {
		f, e := splitList(v, 4)
		if e != nil || len(f) != 4 {
			return out, ErrBlob
		}
		for i, size := range []int{131072, 48, 48} {
			kind, value, rest, e := rlp.Split(f[i+1])
			if e != nil || kind != rlp.String || len(rest) != 0 || len(value) != size {
				return out, ErrBlob
			}
		}
	}
	if e = rlp.DecodeBytes(raw, &out); e != nil || out.Version != 1 || out.Block.Validate() != nil {
		return Response{}, ErrBlob
	}
	return out, nil
}

func splitList(raw []byte, limit int) ([]rlp.RawValue, error) {
	content, tail, e := rlp.SplitList(raw)
	if e != nil || len(tail) != 0 {
		return nil, ErrBlob
	}
	out := make([]rlp.RawValue, 0)
	for len(content) > 0 {
		if len(out) == limit {
			return nil, ErrBlob
		}
		_, _, rest, e := rlp.Split(content)
		if e != nil {
			return nil, ErrBlob
		}
		out = append(out, content[:len(content)-len(rest)])
		content = rest
	}
	return out, nil
}

// Verify authenticates every position, including legitimate repeated hashes.
// KZG establishes blob/commitment correspondence, not consensus finality or
// data availability elsewhere. Cancellation is observed between bounded proofs.
func Verify(ctx context.Context, response Response, block model.FeedBlock, refs []Reference) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if response.Version != 1 || response.Block != block || block.Validate() != nil || len(refs) > MaxBlobs || len(response.Entries) != len(refs) {
		return ErrBlob
	}
	for i := range refs {
		if e := ctx.Err(); e != nil {
			return e
		}
		entry := &response.Entries[i]
		if refs[i].Index != uint32(i) || entry.Index != uint64(i) || refs[i].VersionedHash[0] != 1 || common.Hash(kzg4844.CalcBlobHashV1(sha256.New(), &entry.Commitment)) != refs[i].VersionedHash {
			return fmt.Errorf("%w: ordinal or versioned hash", ErrBlob)
		}
		if e := kzg4844.VerifyBlobProof(&entry.Blob, entry.Commitment, entry.Proof); e != nil {
			return fmt.Errorf("%w: KZG proof: %w", ErrBlob, e)
		}
	}
	return ctx.Err()
}
