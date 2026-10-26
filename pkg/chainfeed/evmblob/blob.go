// Package evmblob supplies and stores complete EIP-4844 blobs. Pure bounded
// decoding/KZG primitives live in proof so storage does not import this worker.
package evmblob

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob/proof"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/rlp"
)

const Codec = proof.Codec
const Format = proof.Format
const MaxBlobs = proof.MaxBlobs
const MaxResponseBytes = proof.MaxResponseBytes

var ErrBlob = proof.ErrBlob
var ErrUnavailable = proof.ErrUnavailable

type Entry = proof.Entry
type Response = proof.Response
type Reference = proof.Reference

func References(b *model.Block) ([]Reference, error) { return proof.References(b) }
func Decode(raw []byte) (Response, error)            { return proof.Decode(raw) }
func Verify(ctx context.Context, r Response, b model.FeedBlock, refs []Reference) error {
	return proof.Verify(ctx, r, b, refs)
}
func splitList(raw []byte, n int) ([]rlp.RawValue, error) { return proof.SplitList(raw, n) }
