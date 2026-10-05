package fetch

import (
	"context"

	"go.uber.org/zap"
)

// Address indexing, balance tracking, token transfers and account
// abstraction moved to features (pkg/features).

// initializeGenesisTokenMetadata indexes token metadata for genesis system contracts
// This is called only for block 0 to ensure system contracts deployed at genesis
// have their token metadata properly indexed.
func (f *Fetcher) initializeGenesisTokenMetadata(ctx context.Context) error {
	// Check if we have a chain adapter with system contracts
	if f.chainAdapter == nil {
		f.logger.Debug("No chain adapter available, skipping genesis token metadata initialization")
		return nil
	}

	systemContracts := f.chainAdapter.SystemContracts()
	if systemContracts == nil {
		f.logger.Debug("No system contracts handler available, skipping genesis token metadata initialization")
		return nil
	}

	// Check if we have a token indexer
	if f.tokenIndexer == nil {
		f.logger.Debug("No token indexer available, skipping genesis token metadata initialization")
		return nil
	}

	// Get all system contract addresses
	addresses := systemContracts.GetSystemContractAddresses()
	if len(addresses) == 0 {
		f.logger.Debug("No system contract addresses found")
		return nil
	}

	f.logger.Info("Indexing genesis system contract token metadata",
		zap.Int("contract_count", len(addresses)),
	)

	// Index token metadata for each system contract
	var indexed, skipped int
	for _, addr := range addresses {
		// Use block height 0 for genesis contracts
		if err := f.tokenIndexer.IndexToken(ctx, addr, 0); err != nil {
			f.logger.Debug("Failed to index genesis contract token metadata (may not be a token)",
				zap.String("address", addr.Hex()),
				zap.String("name", systemContracts.GetSystemContractName(addr)),
				zap.Error(err),
			)
			skipped++
		} else {
			f.logger.Info("Indexed genesis system contract token metadata",
				zap.String("address", addr.Hex()),
				zap.String("name", systemContracts.GetSystemContractName(addr)),
			)
			indexed++
		}
	}

	f.logger.Info("Completed genesis token metadata initialization",
		zap.Int("indexed", indexed),
		zap.Int("skipped", skipped),
		zap.Int("total", len(addresses)),
	)

	return nil
}
