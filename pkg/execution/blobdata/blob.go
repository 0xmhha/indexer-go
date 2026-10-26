// Package blobdata binds complete blob proofs to already verified execution
// input. It starts no worker, EVM, provider or RPC and imports no storage.
package blobdata

import (
	"context"
	"crypto/sha256"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob/proof"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

func Block(chain execwire.Chain, ref execwire.Ref) model.FeedBlock {
	return model.FeedBlock{Chain: model.FeedChain{ID: chain.ID, GenesisHash: common.Hash(chain.Genesis)}, Number: ref.Height, Hash: common.Hash(ref.Hash)}
}
func Validate(ctx context.Context, p chains.BinaryProfile, chain execwire.Chain, r execwire.Result, b *model.Block, required bool) (*port.ExecutionBlobRecord, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if r.BlobBundle == nil {
		if !required {
			return nil, nil
		}
		refs, e := proof.References(b)
		if e != nil {
			return nil, e
		}
		if len(refs) > 0 {
			return nil, proof.ErrUnavailable
		}
		return nil, nil
	}
	if p == nil {
		return nil, port.ErrInvalidData
	}
	out, e := Decode(ctx, p.ID(), chain, r, b)
	if e != nil {
		return nil, e
	}
	c, e := chains.BlobCapabilityOf(p)
	if e != nil {
		return nil, e
	}
	if len(out.References) > 0 && c.Status == model.BlobUnsupported {
		return nil, proof.ErrUnavailable
	}
	return out, nil
}

// Decode rechecks the exact transaction/response contract at the part boundary.
// Selected-profile admission policy is checked by Validate before scheduling.
func Decode(ctx context.Context, profile string, chain execwire.Chain, r execwire.Result, b *model.Block) (*port.ExecutionBlobRecord, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if execwire.ValidateBlobBundle(r.BlobBundle) != nil || r.BlobBundle == nil || r.BlobBundle.Profile != profile || b == nil || r.Ref.Height != b.Number || r.Ref.Hash != execwire.Hash(b.Hash) || r.Parent != execwire.Hash(b.ParentHash) {
		return nil, proof.ErrBlob
	}
	refs, e := proof.References(b)
	if e != nil {
		return nil, e
	}
	response, e := proof.Decode(r.BlobBundle.Response)
	if e != nil {
		return nil, e
	}
	if e = proof.Verify(ctx, response, Block(chain, r.Ref), refs); e != nil {
		return nil, e
	}
	out := &port.ExecutionBlobRecord{Chain: chain, Ref: r.Ref, Parent: r.Parent, InputDigest: execwire.Hash(sha256.Sum256(r.BlobBundle.Response)), Profile: profile, Provider: r.BlobBundle.Provider, References: make([]port.ExecutionBlobReference, 0, len(refs)), Entries: make([]port.ExecutionBlob, 0, len(refs))}
	for i, ref := range refs {
		out.References = append(out.References, port.ExecutionBlobReference{Index: ref.Index, TransactionIndex: ref.TransactionIndex, TransactionHash: execwire.Hash(ref.TransactionHash), BlobIndex: ref.BlobIndex, VersionedHash: execwire.Hash(ref.VersionedHash)})
		entry := &response.Entries[i]
		out.Entries = append(out.Entries, port.ExecutionBlob{Index: uint32(entry.Index), Blob: entry.Blob[:], Commitment: entry.Commitment[:], Proof: entry.Proof[:]})
	}
	return out, nil
}
func References(in []port.ExecutionBlobReference) []proof.Reference {
	out := make([]proof.Reference, len(in))
	for i, v := range in {
		out[i] = proof.Reference{Index: v.Index, TransactionIndex: v.TransactionIndex, TransactionHash: common.Hash(v.TransactionHash), BlobIndex: v.BlobIndex, VersionedHash: common.Hash(v.VersionedHash)}
	}
	return out
}
func Entry(in port.ExecutionBlob) (proof.Entry, error) {
	var out proof.Entry
	if in.Index >= proof.MaxBlobs || len(in.Blob) != len(out.Blob) || len(in.Commitment) != len(out.Commitment) || len(in.Proof) != len(out.Proof) {
		return out, proof.ErrBlob
	}
	out.Index = uint64(in.Index)
	copy(out.Blob[:], in.Blob)
	copy(out.Commitment[:], in.Commitment)
	copy(out.Proof[:], in.Proof)
	return out, nil
}
func VerifyEntry(ctx context.Context, in port.ExecutionBlob, ref port.ExecutionBlobReference) error {
	e, e1 := Entry(in)
	if e1 != nil {
		return e1
	}
	if in.Index != ref.Index {
		return proof.ErrBlob
	}
	return proof.VerifyEntry(ctx, &e, References([]port.ExecutionBlobReference{ref})[0])
}

// VerifyAll authenticates stored rows/proofs and reconstructs the exact relay
// digest before publication/lost ACK; public pages verify requested proofs only.
func VerifyAll(ctx context.Context, r port.ExecutionBlobRecord) error {
	if len(r.Entries) != len(r.References) || len(r.References) > proof.MaxBlobs {
		return proof.ErrBlob
	}
	response := proof.Response{Version: 1, Block: Block(r.Chain, r.Ref), Entries: make([]proof.Entry, 0, len(r.Entries))}
	for _, entry := range r.Entries {
		e, err := Entry(entry)
		if err != nil {
			return err
		}
		response.Entries = append(response.Entries, e)
	}
	if e := proof.Verify(ctx, response, response.Block, References(r.References)); e != nil {
		return e
	}
	raw, e := rlp.EncodeToBytes(response)
	if e != nil {
		return e
	}
	if execwire.Hash(sha256.Sum256(raw)) != r.InputDigest {
		return port.ErrExecutionConflict
	}
	return ctx.Err()
}
