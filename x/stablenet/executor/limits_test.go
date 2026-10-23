package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestReplayRejectsTransactionCountBeforeProcessing(t *testing.T) {
	_, in := evmFixture(t)
	txs := make([]*types.Transaction, 65537)
	for i := range txs {
		txs[i] = in.Block.Transactions()[0]
	}
	in.Block = types.NewBlockWithHeader(in.Block.Header()).WithBody(txs, nil)
	if _, err := Replay(context.Background(), in); !errors.Is(err, execwire.ErrLimit) {
		t.Fatalf("missing early transaction bound: %v", err)
	}
}
