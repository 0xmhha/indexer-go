package fetch

import (
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// The test doubles here keep go-ethereum values; these helpers convert what
// they return to the model the storage ports use.

func modelBlockOf(b *types.Block, err error) (*model.Block, error) {
	if err != nil || b == nil {
		return nil, err
	}
	return gethconv.BlockFromGeth(b)
}

func modelReceiptOf(r *types.Receipt, err error) (*model.Receipt, error) {
	if err != nil || r == nil {
		return nil, err
	}
	return gethconv.ReceiptFromGeth(r), nil
}
