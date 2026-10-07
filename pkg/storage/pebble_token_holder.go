package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
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
func tokenHolderToJSON(h *port.TokenHolder) *TokenHolderJSON {
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
func tokenHolderFromJSON(j *TokenHolderJSON) *port.TokenHolder {
	balance := big.NewInt(0)
	if j.Balance != "" {
		parsed, ok := new(big.Int).SetString(j.Balance, 10)
		if ok {
			balance = parsed
		}
	}
	return &port.TokenHolder{
		TokenAddress:  common.HexToAddress(j.TokenAddress),
		HolderAddress: common.HexToAddress(j.HolderAddress),
		Balance:       balance,
		LastUpdatedAt: j.LastUpdatedAt,
	}
}

// toJSON converts TokenHolderStats to JSON-serializable format
func tokenHolderStatsToJSON(s *port.TokenHolderStats) *TokenHolderStatsJSON {
	return &TokenHolderStatsJSON{
		TokenAddress:   s.TokenAddress.Hex(),
		HolderCount:    s.HolderCount,
		TransferCount:  s.TransferCount,
		LastActivityAt: s.LastActivityAt,
	}
}

// fromJSON converts JSON to TokenHolderStats
func tokenHolderStatsFromJSON(j *TokenHolderStatsJSON) *port.TokenHolderStats {
	return &port.TokenHolderStats{
		TokenAddress:   common.HexToAddress(j.TokenAddress),
		HolderCount:    j.HolderCount,
		TransferCount:  j.TransferCount,
		LastActivityAt: j.LastActivityAt,
	}
}

// GetTokenHolders returns one page of a token's holders, largest balance
// first. The by-token index key holds the inverted balance, so key order is
// balance order and the cursor is the last holder's index key.
func (s *PebbleStorage) GetTokenHolders(ctx context.Context, token common.Address, page port.Page) ([]*port.TokenHolder, string, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, "", err
	}
	prefix := TokenHolderByTokenIndexPrefix(token)
	// Format: /index/token/holder/token/{token}/{balanceHex}/{holder}
	return s.tokenHolderPage(ctx, prefix, page, func(key []byte) (common.Address, common.Address) {
		return token, lastKeyAddress(key)
	})
}

// tokenHolderPage reads one page of a holder index under prefix; holderOf
// returns the token and holder an index key names. Index entries whose
// holder record is missing are skipped.
func (s *PebbleStorage) tokenHolderPage(ctx context.Context, prefix []byte, page port.Page, holderOf func(key []byte) (common.Address, common.Address)) ([]*port.TokenHolder, string, error) {
	return scanLoadedPage(ctx, s, prefix, prefixUpperBound(prefix), false, page, page.Limit,
		func(ctx context.Context, key, _ []byte) (*port.TokenHolder, bool, error) {
			token, holder := holderOf(key)
			h, err := s.getTokenHolder(ctx, token, holder)
			if errors.Is(err, port.ErrNotFound) {
				return nil, false, nil
			}
			if err != nil {
				return nil, false, err
			}
			return h, true, nil
		})
}

// lastKeyAddress returns the address in the last "/" segment of key.
func lastKeyAddress(key []byte) common.Address {
	return common.HexToAddress(string(key[bytes.LastIndexByte(key, '/')+1:]))
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
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
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
func (s *PebbleStorage) GetTokenHolderStats(ctx context.Context, token common.Address) (*port.TokenHolderStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := TokenHolderStatsKey(token)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, port.ErrNotFound
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

// GetHolderTokens returns one page of the tokens an address holds, in the
// order of the by-holder index (token address).
func (s *PebbleStorage) GetHolderTokens(ctx context.Context, holder common.Address, page port.Page) ([]*port.TokenHolder, string, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, "", err
	}
	prefix := TokenHolderByHolderIndexPrefix(holder)
	// Format: /index/token/holder/holder/{holder}/{token}
	return s.tokenHolderPage(ctx, prefix, page, func(key []byte) (common.Address, common.Address) {
		return lastKeyAddress(key), holder
	})
}

// UpdateTokenHolder updates the balance for a token holder
func (s *PebbleStorage) UpdateTokenHolder(ctx context.Context, holder *port.TokenHolder) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}
	if holder == nil {
		return fmt.Errorf("token holder cannot be nil")
	}

	// Get existing holder data if any (for old index cleanup)
	oldHolder, err := s.getTokenHolder(ctx, holder.TokenAddress, holder.HolderAddress)
	hasOldHolder := err == nil && oldHolder != nil

	batch := s.newBatch(ctx)
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
			if err := s.updateHolderCountInBatch(ctx, batch, holder.TokenAddress, -1); err != nil {
				return err
			}
		}

		return s.commitBatch(ctx, batch, pebble.Sync)
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
		if err := s.updateHolderCountInBatch(ctx, batch, holder.TokenAddress, 1); err != nil {
			return err
		}
	}

	return s.commitBatch(ctx, batch, pebble.Sync)
}

// UpdateTokenHolderStats updates the statistics for a token
func (s *PebbleStorage) UpdateTokenHolderStats(ctx context.Context, stats *port.TokenHolderStats) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}
	if stats == nil {
		return fmt.Errorf("token holder stats cannot be nil")
	}

	jsonData := tokenHolderStatsToJSON(stats)
	data, err := json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to marshal token holder stats: %w", err)
	}

	key := TokenHolderStatsKey(stats.TokenAddress)
	if err := s.kv(ctx).Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set token holder stats: %w", err)
	}

	return nil
}

// ProcessERC20TransferForHolders processes an ERC20 transfer event and updates holder balances
func (s *PebbleStorage) ProcessERC20TransferForHolders(ctx context.Context, transfer *port.ERC20Transfer) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}
	if transfer == nil {
		return fmt.Errorf("transfer cannot be nil")
	}

	// Update sender balance (subtract)
	if transfer.From != (common.Address{}) {
		fromHolder, err := s.getTokenHolder(ctx, transfer.ContractAddress, transfer.From)
		if err != nil {
			// New holder with zero balance being subtracted - create with zero
			fromHolder = &port.TokenHolder{
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
			toHolder = &port.TokenHolder{
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
		stats = &port.TokenHolderStats{
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
func (s *PebbleStorage) getTokenHolder(ctx context.Context, token, holder common.Address) (*port.TokenHolder, error) {
	key := TokenHolderKey(token, holder)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, port.ErrNotFound
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
func (s *PebbleStorage) updateHolderCountInBatch(ctx context.Context, batch *pebble.Batch, token common.Address, delta int) error {
	// Get current stats
	key := TokenHolderStatsKey(token)
	value, closer, err := s.kv(ctx).Get(key)

	var stats *port.TokenHolderStats
	if err == nil {
		defer closer.Close()
		var jsonData TokenHolderStatsJSON
		if err := json.Unmarshal(value, &jsonData); err == nil {
			stats = tokenHolderStatsFromJSON(&jsonData)
		}
	}

	if stats == nil {
		stats = &port.TokenHolderStats{
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
