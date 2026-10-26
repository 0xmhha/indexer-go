package evmblob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/rlp"
)

// EngineBlobCaller is borrowed from the chain's existing uncached client.
// HTTP callers must use its shared rpcpool (5 physical attempts/s and bounded
// responses). This adapter never dials, retries or chooses another endpoint.
type EngineBlobCaller interface {
	CallContext(context.Context, interface{}, string, ...interface{}) error
}

// EngineV1 is the Cancun/Prague full-blob pool transport specified by
// execution-apis/src/engine/cancun.md. Pool retention has no minimum guarantee;
// a null, pruned or unsupported response is unavailable, never empty evidence.
// It is not an Engine forkchoice/payload writer or a beacon/PeerDAS verifier.
type EngineV1 struct{ Caller EngineBlobCaller }

func (p EngineV1) Open(ctx context.Context, block model.FeedBlock, refs []Reference) (io.ReadCloser, error) {
	if p.Caller == nil || block.Validate() != nil || len(refs) > MaxBlobs {
		return nil, ErrBlob
	}
	hashes := make([]common.Hash, len(refs))
	for i, ref := range refs {
		if ref.Index != uint32(i) || ref.VersionedHash[0] != 1 {
			return nil, ErrBlob
		}
		hashes[i] = ref.VersionedHash
	}
	response := Response{Version: 1, Block: block, Entries: make([]Entry, len(refs))}
	if len(refs) != 0 {
		request, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		var raw json.RawMessage
		if e := p.Caller.CallContext(request, &raw, "engine_getBlobsV1", hashes); e != nil {
			return nil, errors.Join(ErrUnavailable, e)
		}
		// Bound JSON before decoding any blob arrays. The shared HTTP transport
		// separately bounds the entire physical envelope before RPC decoding.
		if len(raw) == 0 || len(raw) > 2*MaxResponseBytes+4096 {
			return nil, ErrBlob
		}
		var entries []json.RawMessage
		if e := json.Unmarshal(raw, &entries); e != nil || len(entries) != len(refs) {
			return nil, ErrBlob
		}
		for i, raw := range entries {
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return nil, ErrUnavailable
			}
			var item struct {
				Blob  hexutil.Bytes `json:"blob"`
				Proof hexutil.Bytes `json:"proof"`
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if e := decoder.Decode(&item); e != nil || len(item.Blob) != len(kzg4844.Blob{}) || len(item.Proof) != len(kzg4844.Proof{}) {
				return nil, ErrBlob
			}
			entry := &response.Entries[i]
			entry.Index = uint64(i)
			copy(entry.Blob[:], item.Blob)
			copy(entry.Proof[:], item.Proof)
			if e := ctx.Err(); e != nil {
				return nil, e
			}
			commitment, e := kzg4844.BlobToCommitment(&entry.Blob)
			if e != nil {
				return nil, errors.Join(ErrBlob, e)
			}
			entry.Commitment = commitment
		}
	}
	if e := Verify(ctx, response, block, refs); e != nil {
		return nil, e
	}
	raw, e := rlp.EncodeToBytes(response)
	if e != nil {
		return nil, e
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

// ReadProvider bounds, cancels and joins the borrowed reader. Callers must
// independently bind the decoded response to their verified execution input.
func ReadProvider(ctx context.Context, p Provider, block model.FeedBlock, refs []Reference) ([]byte, error) {
	if p == nil {
		return nil, ErrUnavailable
	}
	return readProvider(ctx, p, block, refs)
}
