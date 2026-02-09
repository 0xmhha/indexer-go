package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
)

// TokenHolderJSON is a JSON-serializable version of TokenHolder
type TokenHolderJSON struct {
	TokenAddress  string `json:"tokenAddress"`
	HolderAddress string `json:"holderAddress"`
	Balance       string `json:"balance"`
	LastUpdatedAt uint64 `json:"lastUpdatedAt"`
}

// TokenHolderStatsJSON is a JSON-serializable version of TokenHolderStats
type TokenHolderStatsJSON struct {
	TokenAddress   string `json:"tokenAddress"`
	HolderCount    int    `json:"holderCount"`
	TransferCount  int    `json:"transferCount"`
	LastActivityAt uint64 `json:"lastActivityAt"`
}

// toJSON converts TokenHolder to JSON-serializable format
func tokenHolderToJSON(h *TokenHolder) *TokenHolderJSON {
	balance := "0"
	if h.Balance != nil {
		balance = h.Balance.String()
	}
	return &TokenHolderJSON{
		TokenAddress:  h.TokenAddress.Hex(),
		HolderAddress: h.HolderAddress.Hex(),
		Balance:       balance,
		LastUpdatedAt: h.LastUpdatedAt,
	}
}

// fromJSON converts JSON to TokenHolder
func tokenHolderFromJSON(j *TokenHolderJSON) *TokenHolder {
	balance := big.NewInt(0)
	if j.Balance != "" {
		parsed, ok := new(big.Int).SetString(j.Balance, 10)
		if ok {
			balance = parsed
		}
	}
	return &TokenHolder{
		TokenAddress:  common.HexToAddress(j.TokenAddress),
		HolderAddress: common.HexToAddress(j.HolderAddress),
		Balance:       balance,
		LastUpdatedAt: j.LastUpdatedAt,
	}
}

// toJSON converts TokenHolderStats to JSON-serializable format
func tokenHolderStatsToJSON(s *TokenHolderStats) *TokenHolderStatsJSON {
	return &TokenHolderStatsJSON{
		TokenAddress:   s.TokenAddress.Hex(),
		HolderCount:    s.HolderCount,
		TransferCount:  s.TransferCount,
		LastActivityAt: s.LastActivityAt,
	}
}

// fromJSON converts JSON to TokenHolderStats
func tokenHolderStatsFromJSON(j *TokenHolderStatsJSON) *TokenHolderStats {
	return &TokenHolderStats{
		TokenAddress:   common.HexToAddress(j.TokenAddress),
		HolderCount:    j.HolderCount,
		TransferCount:  j.TransferCount,
		LastActivityAt: j.LastActivityAt,
	}
}

// GetTokenHolders retrieves token holders sorted by balance (descending) with pagination
func (s *PebbleStorage) GetTokenHolders(ctx context.Context, token common.Address, limit, offset int) ([]*TokenHolder, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	prefix := TokenHolderByTokenIndexPrefix(token)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var holders []*TokenHolder
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip for offset
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if limit > 0 && len(holders) >= limit {
			break
		}

		// Extract holder address from index key
		// Format: /index/token/holder/token/{token}/{balanceHex}/{holder}
		keyStr := string(iter.Key())
		parts := strings.Split(keyStr, "/")
		if len(parts) < 2 {
			continue
		}
		holderHex := parts[len(parts)-1]
		holderAddr := common.HexToAddress(holderHex)

		// Get the actual holder data
		holder, err := s.getTokenHolder(ctx, token, holderAddr)
		if err != nil {
			continue
		}
		holders = append(holders, holder)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return holders, nil
}

// GetTokenHolderCount returns the number of unique holders for a token
func (s *PebbleStorage) GetTokenHolderCount(ctx context.Context, token common.Address) (int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	// Try to get from stats first (faster)
	stats, err := s.GetTokenHolderStats(ctx, token)
	if err == nil && stats != nil {
		return stats.HolderCount, nil
	}

	// Fallback: count by iterating (slower)
	prefix := TokenHolderByTokenIndexPrefix(token)
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

// GetTokenBalance retrieves the balance of a specific holder for a token
func (s *PebbleStorage) GetTokenBalance(ctx context.Context, token, holder common.Address) (*big.Int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	h, err := s.getTokenHolder(ctx, token, holder)
	if err != nil {
		return nil, err
	}

	return h.Balance, nil
}

// GetTokenHolderStats retrieves aggregate statistics for a token
func (s *PebbleStorage) GetTokenHolderStats(ctx context.Context, token common.Address) (*TokenHolderStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := TokenHolderStatsKey(token)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get token holder stats: %w", err)
	}
	defer closer.Close()

	var jsonData TokenHolderStatsJSON
	if err := json.Unmarshal(value, &jsonData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token holder stats: %w", err)
	}

	return tokenHolderStatsFromJSON(&jsonData), nil
}

// GetHolderTokens retrieves all tokens held by a specific address with pagination
func (s *PebbleStorage) GetHolderTokens(ctx context.Context, holder common.Address, limit, offset int) ([]*TokenHolder, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	prefix := TokenHolderByHolderIndexPrefix(holder)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var holders []*TokenHolder
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip for offset
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if limit > 0 && len(holders) >= limit {
			break
		}

		// Extract token address from index key
		// Format: /index/token/holder/holder/{holder}/{token}
		keyStr := string(iter.Key())
		parts := strings.Split(keyStr, "/")
		if len(parts) < 2 {
			continue
		}
		tokenHex := parts[len(parts)-1]
		tokenAddr := common.HexToAddress(tokenHex)

		// Get the actual holder data
		h, err := s.getTokenHolder(ctx, tokenAddr, holder)
		if err != nil {
			continue
		}
		holders = append(holders, h)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return holders, nil
}

// UpdateTokenHolder updates the balance for a token holder
func (s *PebbleStorage) UpdateTokenHolder(ctx context.Context, holder *TokenHolder) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get existing holder data if any (for old index cleanup)
	oldHolder, err := s.getTokenHolder(ctx, holder.TokenAddress, holder.HolderAddress)
	hasOldHolder := err == nil && oldHolder != nil

	batch := s.db.NewBatch()
	defer batch.Close()

	// Delete old index if exists
	if hasOldHolder {
		oldIndexKey := TokenHolderByTokenIndexKey(holder.TokenAddress, holder.HolderAddress, oldHolder.Balance)
		if err := batch.Delete(oldIndexKey, nil); err != nil {
			return fmt.Errorf("failed to delete old token index: %w", err)
		}
	}

	// If balance is zero, remove the holder entirely
	if holder.Balance == nil || holder.Balance.Sign() == 0 {
		// Delete holder data
		dataKey := TokenHolderKey(holder.TokenAddress, holder.HolderAddress)
		if err := batch.Delete(dataKey, nil); err != nil {
			return fmt.Errorf("failed to delete holder data: %w", err)
		}

		// Delete holder-token index
		holderIndexKey := TokenHolderByHolderIndexKey(holder.HolderAddress, holder.TokenAddress)
		if err := batch.Delete(holderIndexKey, nil); err != nil {
			return fmt.Errorf("failed to delete holder index: %w", err)
		}

		// Update holder count (decrement)
		if hasOldHolder {
			if err := s.updateHolderCountInBatch(batch, holder.TokenAddress, -1); err != nil {
				return err
			}
		}

		return batch.Commit(pebble.Sync)
	}

	// Save holder data
	jsonData := tokenHolderToJSON(holder)
	data, err := json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to marshal token holder: %w", err)
	}

	dataKey := TokenHolderKey(holder.TokenAddress, holder.HolderAddress)
	if err := batch.Set(dataKey, data, nil); err != nil {
		return fmt.Errorf("failed to set holder data: %w", err)
	}

	// Save token-holder index (sorted by balance)
	newIndexKey := TokenHolderByTokenIndexKey(holder.TokenAddress, holder.HolderAddress, holder.Balance)
	if err := batch.Set(newIndexKey, []byte{1}, nil); err != nil {
		return fmt.Errorf("failed to set token index: %w", err)
	}

	// Save holder-token index
	holderIndexKey := TokenHolderByHolderIndexKey(holder.HolderAddress, holder.TokenAddress)
	if err := batch.Set(holderIndexKey, []byte{1}, nil); err != nil {
		return fmt.Errorf("failed to set holder index: %w", err)
	}

	// Update holder count (increment) if this is a new holder
	if !hasOldHolder {
		if err := s.updateHolderCountInBatch(batch, holder.TokenAddress, 1); err != nil {
			return err
		}
	}

	return batch.Commit(pebble.Sync)
}

// UpdateTokenHolderStats updates the statistics for a token
func (s *PebbleStorage) UpdateTokenHolderStats(ctx context.Context, stats *TokenHolderStats) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	jsonData := tokenHolderStatsToJSON(stats)
	data, err := json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to marshal token holder stats: %w", err)
	}

	key := TokenHolderStatsKey(stats.TokenAddress)
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set token holder stats: %w", err)
	}

	return nil
}

// ProcessERC20TransferForHolders processes an ERC20 transfer event and updates holder balances
func (s *PebbleStorage) ProcessERC20TransferForHolders(ctx context.Context, transfer *ERC20Transfer) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Update sender balance (subtract)
	if transfer.From != (common.Address{}) {
		fromHolder, err := s.getTokenHolder(ctx, transfer.ContractAddress, transfer.From)
		if err != nil {
			// New holder with zero balance being subtracted - create with zero
			fromHolder = &TokenHolder{
				TokenAddress:  transfer.ContractAddress,
				HolderAddress: transfer.From,
				Balance:       big.NewInt(0),
				LastUpdatedAt: transfer.BlockNumber,
			}
		}

		newBalance := new(big.Int).Sub(fromHolder.Balance, transfer.Value)
		if newBalance.Sign() < 0 {
			newBalance = big.NewInt(0)
		}

		fromHolder.Balance = newBalance
		fromHolder.LastUpdatedAt = transfer.BlockNumber

		if err := s.UpdateTokenHolder(ctx, fromHolder); err != nil {
			return fmt.Errorf("failed to update sender balance: %w", err)
		}
	}

	// Update receiver balance (add)
	if transfer.To != (common.Address{}) {
		toHolder, err := s.getTokenHolder(ctx, transfer.ContractAddress, transfer.To)
		if err != nil {
			// New holder
			toHolder = &TokenHolder{
				TokenAddress:  transfer.ContractAddress,
				HolderAddress: transfer.To,
				Balance:       big.NewInt(0),
				LastUpdatedAt: transfer.BlockNumber,
			}
		}

		newBalance := new(big.Int).Add(toHolder.Balance, transfer.Value)
		toHolder.Balance = newBalance
		toHolder.LastUpdatedAt = transfer.BlockNumber

		if err := s.UpdateTokenHolder(ctx, toHolder); err != nil {
			return fmt.Errorf("failed to update receiver balance: %w", err)
		}
	}

	// Update transfer count in stats
	stats, err := s.GetTokenHolderStats(ctx, transfer.ContractAddress)
	if err != nil {
		stats = &TokenHolderStats{
			TokenAddress:   transfer.ContractAddress,
			HolderCount:    0,
			TransferCount:  0,
			LastActivityAt: transfer.BlockNumber,
		}
	}
	stats.TransferCount++
	stats.LastActivityAt = transfer.BlockNumber

	if err := s.UpdateTokenHolderStats(ctx, stats); err != nil {
		return fmt.Errorf("failed to update token stats: %w", err)
	}

	return nil
}

// getTokenHolder retrieves a single token holder record
func (s *PebbleStorage) getTokenHolder(ctx context.Context, token, holder common.Address) (*TokenHolder, error) {
	key := TokenHolderKey(token, holder)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get token holder: %w", err)
	}
	defer closer.Close()

	var jsonData TokenHolderJSON
	if err := json.Unmarshal(value, &jsonData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token holder: %w", err)
	}

	return tokenHolderFromJSON(&jsonData), nil
}

// updateHolderCountInBatch updates the holder count in a batch
func (s *PebbleStorage) updateHolderCountInBatch(batch *pebble.Batch, token common.Address, delta int) error {
	// Get current stats
	key := TokenHolderStatsKey(token)
	value, closer, err := s.db.Get(key)

	var stats *TokenHolderStats
	if err == nil {
		defer closer.Close()
		var jsonData TokenHolderStatsJSON
		if err := json.Unmarshal(value, &jsonData); err == nil {
			stats = tokenHolderStatsFromJSON(&jsonData)
		}
	}

	if stats == nil {
		stats = &TokenHolderStats{
			TokenAddress:   token,
			HolderCount:    0,
			TransferCount:  0,
			LastActivityAt: 0,
		}
	}

	stats.HolderCount += delta
	if stats.HolderCount < 0 {
		stats.HolderCount = 0
	}

	jsonData := tokenHolderStatsToJSON(stats)
	data, err := json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to marshal token holder stats: %w", err)
	}

	if err := batch.Set(key, data, nil); err != nil {
		return fmt.Errorf("failed to set token holder stats: %w", err)
	}

	return nil
}
