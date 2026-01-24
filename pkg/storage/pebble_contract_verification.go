package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
)

// ========== Contract Verification Reader Methods ==========

// GetContractVerification returns verification data for a contract
func (s *PebbleStorage) GetContractVerification(ctx context.Context, address common.Address) (*ContractVerification, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := ContractVerificationKey(address)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get contract verification: %w", err)
	}
	defer closer.Close()

	// Copy the value since it's only valid until closer is called
	data := make([]byte, len(value))
	copy(data, value)

	var verification ContractVerification
	if err := json.Unmarshal(data, &verification); err != nil {
		return nil, fmt.Errorf("failed to decode contract verification: %w", err)
	}

	return &verification, nil
}

// IsContractVerified checks if a contract is verified
func (s *PebbleStorage) IsContractVerified(ctx context.Context, address common.Address) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}

	key := ContractVerificationKey(address)
	_, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, fmt.Errorf("failed to check contract verification: %w", err)
	}
	closer.Close()

	return true, nil
}

// ListVerifiedContracts returns all verified contract addresses with pagination
func (s *PebbleStorage) ListVerifiedContracts(ctx context.Context, limit, offset int) ([]common.Address, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = DefaultVerifiedContractsLimit
	}
	if offset < 0 {
		offset = 0
	}

	prefix := VerifiedContractIndexKeyPrefix()
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var addresses []common.Address
	current := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset entries
		if current < offset {
			current++
			continue
		}

		// Stop at limit
		if len(addresses) >= limit {
			break
		}

		// Extract address from key
		// Key format: /index/verification/verified/{verifiedAt_timestamp}/{address}
		key := string(iter.Key())
		prefixLen := len(string(prefix))

		// Skip timestamp part and extract address
		// Format: {timestamp}/{address}
		parts := key[prefixLen:]
		var addrHex string
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] == '/' {
				addrHex = parts[i+1:]
				break
			}
		}

		if !common.IsHexAddress(addrHex) {
			continue // Skip invalid keys
		}

		addresses = append(addresses, common.HexToAddress(addrHex))
		current++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return addresses, nil
}

// CountVerifiedContracts returns the total number of verified contracts
func (s *PebbleStorage) CountVerifiedContracts(ctx context.Context) (int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	prefix := VerifiedContractIndexKeyPrefix()
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
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

// ========== Contract Verification Writer Methods ==========

// SetContractVerification stores contract verification data
func (s *PebbleStorage) SetContractVerification(ctx context.Context, verification *ContractVerification) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	if verification == nil {
		return fmt.Errorf("verification cannot be nil")
	}

	// Encode verification data as JSON
	data, err := json.Marshal(verification)
	if err != nil {
		return fmt.Errorf("failed to encode contract verification: %w", err)
	}

	// Store verification data
	key := ContractVerificationKey(verification.Address)
	if err := s.db.Set(key, data, nil); err != nil {
		return fmt.Errorf("failed to set contract verification: %w", err)
	}

	// Store index entry for listing verified contracts
	indexKey := VerifiedContractIndexKey(verification.VerifiedAt.Unix(), verification.Address)
	if err := s.db.Set(indexKey, []byte{1}, nil); err != nil {
		return fmt.Errorf("failed to set verified contract index: %w", err)
	}

	return nil
}

// DeleteContractVerification removes contract verification data
func (s *PebbleStorage) DeleteContractVerification(ctx context.Context, address common.Address) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get verification data to find the index key
	verification, err := s.GetContractVerification(ctx, address)
	if err != nil {
		if err == ErrNotFound {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to get verification for deletion: %w", err)
	}

	// Delete verification data
	key := ContractVerificationKey(address)
	if err := s.db.Delete(key, nil); err != nil {
		return fmt.Errorf("failed to delete contract verification: %w", err)
	}

	// Delete index entry
	indexKey := VerifiedContractIndexKey(verification.VerifiedAt.Unix(), address)
	if err := s.db.Delete(indexKey, nil); err != nil {
		return fmt.Errorf("failed to delete verified contract index: %w", err)
	}

	return nil
}
