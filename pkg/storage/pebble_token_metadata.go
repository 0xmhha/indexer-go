package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// TokenMetadataJSON is a JSON-serializable version of TokenMetadata
type TokenMetadataJSON struct {
	Address            string  `json:"address"`
	Standard           string  `json:"standard"`
	Name               string  `json:"name"`
	Symbol             string  `json:"symbol"`
	Decimals           uint8   `json:"decimals"`
	TotalSupply        string  `json:"totalSupply,omitempty"`
	BaseURI            string  `json:"baseURI,omitempty"`
	DetectedAt         uint64  `json:"detectedAt"`
	CreatedAt          int64   `json:"createdAt"`
	UpdatedAt          int64   `json:"updatedAt"`
	SupportsERC165     bool    `json:"supportsERC165"`
	SupportsMetadata   bool    `json:"supportsMetadata"`
	SupportsEnumerable bool    `json:"supportsEnumerable,omitempty"`
}

// toJSON converts TokenMetadata to JSON-serializable format
func tokenMetadataToJSON(m *TokenMetadata) *TokenMetadataJSON {
	var totalSupply string
	if m.TotalSupply != nil {
		totalSupply = m.TotalSupply.String()
	}

	return &TokenMetadataJSON{
		Address:            m.Address.Hex(),
		Standard:           string(m.Standard),
		Name:               m.Name,
		Symbol:             m.Symbol,
		Decimals:           m.Decimals,
		TotalSupply:        totalSupply,
		BaseURI:            m.BaseURI,
		DetectedAt:         m.DetectedAt,
		CreatedAt:          m.CreatedAt.UnixNano(),
		UpdatedAt:          m.UpdatedAt.UnixNano(),
		SupportsERC165:     m.SupportsERC165,
		SupportsMetadata:   m.SupportsMetadata,
		SupportsEnumerable: m.SupportsEnumerable,
	}
}

// fromJSON converts JSON-serializable format to TokenMetadata
func tokenMetadataFromJSON(j *TokenMetadataJSON) *TokenMetadata {
	var totalSupply *big.Int
	if j.TotalSupply != "" {
		totalSupply = new(big.Int)
		totalSupply.SetString(j.TotalSupply, 10)
	}

	return &TokenMetadata{
		Address:            common.HexToAddress(j.Address),
		Standard:           TokenStandard(j.Standard),
		Name:               j.Name,
		Symbol:             j.Symbol,
		Decimals:           j.Decimals,
		TotalSupply:        totalSupply,
		BaseURI:            j.BaseURI,
		DetectedAt:         j.DetectedAt,
		CreatedAt:          time.Unix(0, j.CreatedAt),
		UpdatedAt:          time.Unix(0, j.UpdatedAt),
		SupportsERC165:     j.SupportsERC165,
		SupportsMetadata:   j.SupportsMetadata,
		SupportsEnumerable: j.SupportsEnumerable,
	}
}

// GetTokenMetadata retrieves token metadata by contract address
func (s *PebbleStorage) GetTokenMetadata(ctx context.Context, address common.Address) (*TokenMetadata, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := TokenMetadataKey(address)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get token metadata: %w", err)
	}
	defer closer.Close()

	var jsonData TokenMetadataJSON
	if err := json.Unmarshal(value, &jsonData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token metadata: %w", err)
	}

	return tokenMetadataFromJSON(&jsonData), nil
}

// SaveTokenMetadata saves or updates token metadata
func (s *PebbleStorage) SaveTokenMetadata(ctx context.Context, metadata *TokenMetadata) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Check if we need to delete old indexes (for update case)
	oldMetadata, err := s.GetTokenMetadata(ctx, metadata.Address)
	if err == nil && oldMetadata != nil {
		// Delete old indexes if name/symbol changed
		if oldMetadata.Name != metadata.Name && oldMetadata.Name != "" {
			oldNameKey := TokenNameIndexKey(oldMetadata.Name, metadata.Address)
			if err := s.db.Delete(oldNameKey, pebble.Sync); err != nil && err != pebble.ErrNotFound {
				s.logger.Warn("Failed to delete old name index", zap.Error(err))
			}
		}
		if oldMetadata.Symbol != metadata.Symbol && oldMetadata.Symbol != "" {
			oldSymbolKey := TokenSymbolIndexKey(oldMetadata.Symbol, metadata.Address)
			if err := s.db.Delete(oldSymbolKey, pebble.Sync); err != nil && err != pebble.ErrNotFound {
				s.logger.Warn("Failed to delete old symbol index", zap.Error(err))
			}
		}
	}

	// Serialize metadata
	jsonData := tokenMetadataToJSON(metadata)
	data, err := json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to marshal token metadata: %w", err)
	}

	// Use batch for atomic writes
	batch := s.db.NewBatch()
	defer batch.Close()

	// Save main data
	key := TokenMetadataKey(metadata.Address)
	if err := batch.Set(key, data, nil); err != nil {
		return fmt.Errorf("failed to set token metadata: %w", err)
	}

	// Save standard index
	standardKey := TokenStandardIndexKey(string(metadata.Standard), metadata.Address)
	if err := batch.Set(standardKey, []byte{1}, nil); err != nil {
		return fmt.Errorf("failed to set standard index: %w", err)
	}

	// Save name index (for search)
	if metadata.Name != "" {
		nameKey := TokenNameIndexKey(metadata.Name, metadata.Address)
		if err := batch.Set(nameKey, []byte{1}, nil); err != nil {
			return fmt.Errorf("failed to set name index: %w", err)
		}
	}

	// Save symbol index (for search)
	if metadata.Symbol != "" {
		symbolKey := TokenSymbolIndexKey(metadata.Symbol, metadata.Address)
		if err := batch.Set(symbolKey, []byte{1}, nil); err != nil {
			return fmt.Errorf("failed to set symbol index: %w", err)
		}
	}

	return batch.Commit(pebble.Sync)
}

// DeleteTokenMetadata removes token metadata by address
func (s *PebbleStorage) DeleteTokenMetadata(ctx context.Context, address common.Address) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get existing metadata to delete indexes
	metadata, err := s.GetTokenMetadata(ctx, address)
	if err != nil {
		if err == ErrNotFound {
			return nil // Already deleted
		}
		return err
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	// Delete main data
	key := TokenMetadataKey(address)
	if err := batch.Delete(key, nil); err != nil {
		return fmt.Errorf("failed to delete token metadata: %w", err)
	}

	// Delete standard index
	standardKey := TokenStandardIndexKey(string(metadata.Standard), address)
	if err := batch.Delete(standardKey, nil); err != nil {
		return fmt.Errorf("failed to delete standard index: %w", err)
	}

	// Delete name index
	if metadata.Name != "" {
		nameKey := TokenNameIndexKey(metadata.Name, address)
		if err := batch.Delete(nameKey, nil); err != nil {
			return fmt.Errorf("failed to delete name index: %w", err)
		}
	}

	// Delete symbol index
	if metadata.Symbol != "" {
		symbolKey := TokenSymbolIndexKey(metadata.Symbol, address)
		if err := batch.Delete(symbolKey, nil); err != nil {
			return fmt.Errorf("failed to delete symbol index: %w", err)
		}
	}

	return batch.Commit(pebble.Sync)
}

// ListTokensByStandard retrieves tokens filtered by standard with pagination
func (s *PebbleStorage) ListTokensByStandard(ctx context.Context, standard TokenStandard, limit, offset int) ([]*TokenMetadata, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	var prefix []byte
	if standard != "" && standard != TokenStandardUnknown {
		// Use standard index
		prefix = TokenStandardIndexKeyPrefix(string(standard))
	} else {
		// Scan all tokens
		prefix = TokenMetadataKeyPrefix()
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var tokens []*TokenMetadata
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip for offset
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if limit > 0 && len(tokens) >= limit {
			break
		}

		var address common.Address
		if standard != "" && standard != TokenStandardUnknown {
			// Extract address from index key
			keyStr := string(iter.Key())
			parts := strings.Split(keyStr, "/")
			if len(parts) > 0 {
				address = common.HexToAddress(parts[len(parts)-1])
			}
		} else {
			// Parse from metadata key
			keyStr := string(iter.Key())
			parts := strings.Split(keyStr, "/")
			if len(parts) > 0 {
				address = common.HexToAddress(parts[len(parts)-1])
			}
		}

		// Fetch full metadata
		metadata, err := s.GetTokenMetadata(ctx, address)
		if err != nil {
			continue
		}

		tokens = append(tokens, metadata)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return tokens, nil
}

// GetTokensCount returns the count of tokens, optionally filtered by standard
func (s *PebbleStorage) GetTokensCount(ctx context.Context, standard TokenStandard) (int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	var prefix []byte
	if standard != "" && standard != TokenStandardUnknown {
		prefix = TokenStandardIndexKeyPrefix(string(standard))
	} else {
		prefix = TokenMetadataKeyPrefix()
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}

	if err := iter.Error(); err != nil {
		return 0, fmt.Errorf("iterator error: %w", err)
	}

	return count, nil
}

// SearchTokens searches for tokens by name or symbol (case-insensitive partial match)
func (s *PebbleStorage) SearchTokens(ctx context.Context, query string, limit int) ([]*TokenMetadata, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if query == "" {
		return nil, nil
	}

	query = strings.ToLower(query)
	addressSet := make(map[common.Address]bool)
	var tokens []*TokenMetadata

	// Search by name prefix
	namePrefix := TokenNameIndexKeyPrefix(query)
	nameIter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: namePrefix,
		UpperBound: prefixUpperBound(namePrefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create name iterator: %w", err)
	}

	for nameIter.First(); nameIter.Valid(); nameIter.Next() {
		if limit > 0 && len(tokens) >= limit {
			break
		}

		keyStr := string(nameIter.Key())
		parts := strings.Split(keyStr, "/")
		if len(parts) > 0 {
			address := common.HexToAddress(parts[len(parts)-1])
			if !addressSet[address] {
				addressSet[address] = true
				metadata, err := s.GetTokenMetadata(ctx, address)
				if err == nil {
					tokens = append(tokens, metadata)
				}
			}
		}
	}
	nameIter.Close()

	// Search by symbol prefix
	if limit <= 0 || len(tokens) < limit {
		symbolPrefix := TokenSymbolIndexKeyPrefix(query)
		symbolIter, err := s.db.NewIter(&pebble.IterOptions{
			LowerBound: symbolPrefix,
			UpperBound: prefixUpperBound(symbolPrefix),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create symbol iterator: %w", err)
		}

		for symbolIter.First(); symbolIter.Valid(); symbolIter.Next() {
			if limit > 0 && len(tokens) >= limit {
				break
			}

			keyStr := string(symbolIter.Key())
			parts := strings.Split(keyStr, "/")
			if len(parts) > 0 {
				address := common.HexToAddress(parts[len(parts)-1])
				if !addressSet[address] {
					addressSet[address] = true
					metadata, err := s.GetTokenMetadata(ctx, address)
					if err == nil {
						tokens = append(tokens, metadata)
					}
				}
			}
		}
		symbolIter.Close()
	}

	return tokens, nil
}

// Note: prefixUpperBound is already defined in pebble.go
