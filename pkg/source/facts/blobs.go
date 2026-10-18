package facts

import (
	"context"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Blobs reads verified full blobs from permanent source storage only. The
// Source's epoch pins every page; another epoch requires a new Source.
func (s *Source) Blobs(ctx context.Context, n uint64, d model.SupplementStreamDescriptor, after uint32, limit int) (evmblob.Page, error) {
	id, e := s.identity(ctx, n)
	if e != nil {
		return evmblob.Page{}, e
	}
	out, e := evmblob.ReadPage(ctx, s.cfg.Store, s.cfg.Profile, s.epoch, id, d, after, limit)
	if e == port.ErrNotFound {
		return evmblob.Page{}, fmt.Errorf("%w: full blob input: %w", ErrCoverage, e)
	}
	return out, e
}
