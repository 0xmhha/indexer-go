package fetch

import (
	"context"

	"go.uber.org/zap"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// Address indexing, balance tracking, token transfers and account
// abstraction moved to features (pkg/features).

// initializeGenesisTokenMetadata indexes the metadata of the token
// contracts the chain defines (storage.RegisterKnownToken, such as a native
// coin exposed as a token contract). They exist from genesis without a
// creation transaction, so contract-creation indexing never sees them.
func (f *Fetcher) initializeGenesisTokenMetadata(ctx context.Context) error {
	if f.tokenIndexer == nil {
		f.logger.Debug("No token indexer available, skipping genesis token metadata initialization")
		return nil
	}
	addresses := storagepkg.KnownTokenAddresses()
	if len(addresses) == 0 {
		return nil
	}

	var indexed, skipped int
	for _, addr := range addresses {
		// Use block height 0 for genesis contracts
		if err := f.tokenIndexer.IndexToken(ctx, addr, 0); err != nil {
			f.logger.Debug("Failed to index genesis token metadata",
				zap.String("address", addr.Hex()),
				zap.Error(err),
			)
			skipped++
			continue
		}
		indexed++
	}

	f.logger.Info("Completed genesis token metadata initialization",
		zap.Int("indexed", indexed),
		zap.Int("skipped", skipped),
		zap.Int("total", len(addresses)),
	)
	return nil
}
