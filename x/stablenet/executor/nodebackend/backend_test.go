package nodebackend

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/stablenet/executor"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/exporter"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"testing"
)

type backendChain struct {
	chainReader
	parent, head *types.Header
	block        *types.Block
	receipts     types.Receipts
}

func (c *backendChain) CurrentBlock() *types.Header { return c.head }
func (c *backendChain) GetHeaderByNumber(n uint64) *types.Header {
	if n == 0 {
		return c.parent
	}
	if n == 1 {
		return c.head
	}
	return nil
}
func (c *backendChain) GetBlock(h common.Hash, n uint64) *types.Block {
	if n == 1 && h == c.head.Hash() {
		return c.block
	}
	return nil
}
func (c *backendChain) GetReceiptsByHash(common.Hash) types.Receipts { return c.receipts }
func (c *backendChain) StateCache() state.Database                   { return nil }
func TestBackendUsesImportedCanonicalBlockAndStoredReceipts(t *testing.T) {
	p := &types.Header{Number: big.NewInt(0), Root: common.Hash{1}}
	h := &types.Header{Number: big.NewInt(1), ParentHash: p.Hash(), Root: common.Hash{2}}
	c := &backendChain{parent: p, head: h, block: types.NewBlockWithHeader(h), receipts: types.Receipts{&types.Receipt{Status: 1}}}
	called := false
	b := &Backend{chain: c, replay: func(ctx context.Context, in executor.ReplayInput) (execwire.Result, error) {
		called = true
		if in.Parent.Hash() != p.Hash() || in.Block.Hash() != h.Hash() || in.Generation != 4 || len(in.Receipts) != 1 {
			t.Fatal("wrong replay input")
		}
		return execwire.Result{Ref: execwire.Ref{Generation: 4, Height: 1, Hash: execwire.Hash(h.Hash())}}, nil
	}}
	ref, err := b.Head(t.Context())
	if err != nil || ref.Height != 1 || ref.Hash != execwire.Hash(h.Hash()) {
		t.Fatalf("head: %v %v", ref, err)
	}
	hash, err := b.Canonical(t.Context(), 0)
	if err != nil || hash != execwire.Hash(p.Hash()) {
		t.Fatalf("canonical: %v %v", hash, err)
	}
	r, err := b.Replay(t.Context(), 1, 4)
	if err != nil || !called || r.Ref.Generation != 4 {
		t.Fatalf("replay: %v %v", r, err)
	}
	c.parent = nil
	if _, err = b.Replay(t.Context(), 1, 4); err == nil {
		t.Fatal("missing parent accepted")
	}
	if _, err = b.Canonical(t.Context(), 9); err == nil {
		t.Fatal("missing header accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = b.Head(ctx); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	_ = exporter.ErrCheckpointRecovery
}
