package storage

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// ============================================================================
// Token Balance Helpers
// ============================================================================

// applyTokenMetadata applies metadata to a TokenBalance from various sources
func (s *PebbleStorage) applyTokenMetadata(ctx context.Context, tb *port.TokenBalance, contract common.Address) {
	// Priority: 1) Metadata the chain defines, 2) Database, 3) On-demand fetch from chain
	if metadata, ok := knownToken(contract); ok {
		// 1. Token metadata registered by the chain package
		tb.Name = metadata.Name
		tb.Symbol = metadata.Symbol
		decimals := metadata.Decimals
		tb.Decimals = &decimals
		return
	}

	if dbMetadata, err := s.GetTokenMetadata(ctx, contract); err == nil && dbMetadata != nil {
		// 2. Database token metadata
		tb.Name = dbMetadata.Name
		tb.Symbol = dbMetadata.Symbol
		decimals := int(dbMetadata.Decimals)
		tb.Decimals = &decimals
		if dbMetadata.Standard != "" {
			tb.TokenType = string(dbMetadata.Standard)
		}
		tb.Metadata = buildTokenMetadataJSON(dbMetadata)
		return
	}

	// 3. On-demand fetch from chain and cache
	if s.tokenMetadataFetcher != nil {
		s.fetchAndCacheTokenMetadata(ctx, tb, contract)
	}
}

// fetchAndCacheTokenMetadata fetches token metadata from chain and caches it
func (s *PebbleStorage) fetchAndCacheTokenMetadata(ctx context.Context, tb *port.TokenBalance, contract common.Address) {
	fetchedMetadata, err := s.tokenMetadataFetcher.FetchTokenMetadata(ctx, contract)
	if err != nil || fetchedMetadata == nil {
		return
	}

	tb.Name = fetchedMetadata.Name
	tb.Symbol = fetchedMetadata.Symbol
	decimals := int(fetchedMetadata.Decimals)
	tb.Decimals = &decimals
	if fetchedMetadata.Standard != "" {
		tb.TokenType = string(fetchedMetadata.Standard)
	}
	tb.Metadata = buildTokenMetadataJSON(fetchedMetadata)

	// Cache the fetched metadata
	if saveErr := s.SaveTokenMetadata(ctx, fetchedMetadata); saveErr != nil {
		s.logger.Warn("Failed to cache fetched token metadata",
			zap.String("contract", contract.Hex()),
			zap.Error(saveErr),
		)
	} else {
		s.logger.Info("Cached on-demand fetched token metadata",
			zap.String("contract", contract.Hex()),
			zap.String("name", fetchedMetadata.Name),
			zap.String("symbol", fetchedMetadata.Symbol),
			zap.Uint8("decimals", fetchedMetadata.Decimals),
		)
	}
}
