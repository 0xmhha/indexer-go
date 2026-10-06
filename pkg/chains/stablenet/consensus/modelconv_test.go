package consensus

import (
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// modelBlock converts a go-ethereum test block to the model the storage
// stores.
func modelBlock(b *types.Block) *model.Block {
	m, err := gethconv.BlockFromGeth(b)
	if err != nil {
		panic(err)
	}
	return m
}
