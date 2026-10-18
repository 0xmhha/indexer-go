package port

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// LatestBlockWindowReader optionally reads the indexed tip and its height
// window together. Offset/limit count heights, including missing blocks, not
// rows. Blocks are ascending, as with BlockReader.GetBlocks. A missing indexed
// tip returns ErrNotFound; a window before genesis returns an empty list.
// Implementations must honor a transaction bound to the same Store/context.
type LatestBlockWindowReader interface {
	GetLatestBlockWindow(ctx context.Context, offset, limit int) (uint64, []*model.Block, error)
}
