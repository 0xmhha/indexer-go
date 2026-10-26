// Package execution connects bounded execution results to unpublished writers.
package execution

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	nativecalls "github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace/native"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/blobdata"
	"github.com/0xmhha/indexer-go/pkg/execution/proofdata"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"io"
	"math"
	"math/big"
)

type Prepared struct {
	result    execwire.Result
	block     *model.Block
	receipts  []*model.Receipt
	creations []port.ExecutionCreation
	calls     []port.ExecutionCall
	chain     execwire.Chain
	blobs     *port.ExecutionBlobRecord
	digest    execwire.Hash
	bytes     int64
}

// Prepare owns decoded input and authenticates all encoded commitments through
// the selected profile. It runs neither EVM nor RPC and proves no new consensus.
func Prepare(profile chains.BinaryProfile, chain execwire.Chain, payload []byte) (*Prepared, error) {
	return PrepareContext(context.Background(), profile, chain, payload, false)
}

type Requirements struct {
	BlobSidecars bool
	StateProofs  *stateproof.Selection
}

func PrepareContext(ctx context.Context, profile chains.BinaryProfile, chain execwire.Chain, payload []byte, requireBlobs bool) (*Prepared, error) {
	return PrepareWithRequirements(ctx, profile, chain, payload, Requirements{BlobSidecars: requireBlobs})
}
func PrepareWithRequirements(ctx context.Context, profile chains.BinaryProfile, chain execwire.Chain, payload []byte, requirements Requirements) (*Prepared, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	r, err := execwire.UnmarshalResult(payload)
	if err != nil {
		return nil, err
	}
	if err = execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: r.Ref}}); err != nil {
		return nil, err
	}
	if r.Time > math.MaxInt64 {
		return nil, port.ErrInvalidData
	}
	parts, err := evmdata.Items(r.BlockRLP, 4)
	if err != nil || len(parts) < 3 {
		return nil, fmt.Errorf("execution: invalid encoded block")
	}
	body, err := rlp.EncodeToBytes(parts[1:])
	if err != nil {
		return nil, err
	}
	b, rs, err := evmdata.DecodeBounded(profile, model.FeedBlock{Chain: model.FeedChain{ID: chain.ID, GenesisHash: common.Hash(chain.Genesis)}, Number: r.Ref.Height, Hash: common.Hash(r.Ref.Hash)}, parts[0], body, r.ReceiptsRLP, evmdata.DecodeLimits{PartBytes: 64 << 20, Items: 65536, Logs: 262144})
	if err != nil {
		return nil, err
	}
	if execwire.Hash(b.ParentHash) != r.Parent || execwire.Hash(b.StateRoot) != r.StateRoot || execwire.Hash(b.ReceiptRoot) != r.ReceiptsRoot || b.Time != r.Time || len(b.Transactions) != int(r.TxCount) {
		return nil, port.ErrExecutionConflict
	}
	p := &Prepared{chain: chain, result: r, block: b, receipts: rs, digest: execwire.Hash(sha256.Sum256(payload)), bytes: int64(len(payload))*4 + 4096}
	p.calls, err = nativecalls.Verify(profile.ID(), r, b, rs)
	if err != nil {
		return nil, err
	}
	for _, f := range p.calls {
		p.bytes += 512 + int64(len(f.Path))*4
	}
	p.blobs, err = blobdata.Validate(ctx, profile, chain, r, b, requirements.BlobSidecars)
	if err != nil {
		return nil, err
	}
	if err = proofdata.Validate(ctx, profile.ID(), chain, r, requirements.StateProofs); err != nil {
		return nil, err
	}
	chainID, _ := new(big.Int).SetString(chain.ID, 10)
	for _, s := range r.Steps {
		if s.Cause == execwire.Baseline || s.Cause == execwire.Genesis {
			return nil, port.ErrExecutionConflict
		}
		p.bytes += int64(len(s.Accounts)+len(s.Storage)+len(s.Code))*512 + 1024
		if s.Cause != execwire.Transaction {
			continue
		}
		tx := b.Transactions[s.TxIndex]
		if execwire.Hash(tx.Hash) != s.TxHash || tx.ChainID != nil && tx.ChainID.Sign() != 0 && tx.ChainID.Cmp(chainID) != 0 {
			return nil, port.ErrExecutionConflict
		}
		p.bytes += 8192
		receipt := rs[s.TxIndex]
		p.bytes += int64(len(receipt.Logs)) * 512
		if tx.To == nil && receipt.Status == model.ReceiptStatusSuccessful && receipt.ContractAddress != nil && *receipt.ContractAddress != (common.Address{}) {
			p.creations = append(p.creations, port.ExecutionCreation{Address: execwire.Address(*receipt.ContractAddress), Creator: execwire.Address(tx.From), TxIndex: uint32(s.TxIndex), TxHash: s.TxHash})
		}
	}
	return p, nil
}
