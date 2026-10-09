package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// ========== Contract Verification Reader Methods ==========

// GetContractVerification returns verification data for a contract
func (s *PebbleStorage) GetContractVerification(ctx context.Context, address common.Address) (*port.ContractVerification, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := ContractVerificationKey(address)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get contract verification: %w", err)
	}
	defer func() { _ = closer.Close() }()

	// Copy the value since it's only valid until closer is called
	data := make([]byte, len(value))
	copy(data, value)

	var verification port.ContractVerification
	if err := json.Unmarshal(data, &verification); err != nil {
		return nil, fmt.Errorf("failed to decode contract verification: %w", err)
	}

	return &verification, nil
}

// IsContractVerified checks if a contract is verified: it has a record
// whose IsVerified is set.
func (s *PebbleStorage) IsContractVerified(ctx context.Context, address common.Address) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}

	verification, err := s.GetContractVerification(ctx, address)
	if err != nil {
		if err == port.ErrNotFound {
			return false, nil
		}
		return false, fmt.Errorf("failed to check contract verification: %w", err)
	}

	return verification.IsVerified, nil
}

// ListVerifiedContracts returns one page of the verified contract addresses,
// in the order of the verified index (verification time). The cursor is the
// last address's index key.
func (s *PebbleStorage) ListVerifiedContracts(ctx context.Context, page port.Page) ([]common.Address, string, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, "", err
	}

	// Key format: /index/verification/verified/{verifiedAt_timestamp}/{address}
	prefix := VerifiedContractIndexKeyPrefix()
	return scanLoadedPage(ctx, s, prefix, prefixUpperBound(prefix), false, page, pageLimit(page, DefaultVerifiedContractsLimit),
		func(_ context.Context, key, _ []byte) (common.Address, bool, error) {
			rest := key[len(prefix):]
			addrHex := string(rest[bytes.LastIndexByte(rest, '/')+1:])
			if bytes.IndexByte(rest, '/') < 0 || !common.IsHexAddress(addrHex) {
				return common.Address{}, false, nil // Skip invalid keys
			}
			return common.HexToAddress(addrHex), true, nil
		})
}

// CountVerifiedContracts returns the total number of verified contracts
func (s *PebbleStorage) CountVerifiedContracts(ctx context.Context) (int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	prefix := VerifiedContractIndexKeyPrefix()
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

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
func (s *PebbleStorage) SetContractVerification(ctx context.Context, verification *port.ContractVerification) error {
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

	// Remove the list entry of the record being replaced; its verification
	// time may differ.
	previous, err := s.GetContractVerification(ctx, verification.Address)
	if err != nil && err != port.ErrNotFound {
		return fmt.Errorf("failed to get previous contract verification: %w", err)
	}
	if previous != nil {
		oldIndexKey := VerifiedContractIndexKey(previous.VerifiedAt.Unix(), verification.Address)
		if err := s.kv(ctx).Delete(oldIndexKey, nil); err != nil {
			return fmt.Errorf("failed to delete verified contract index: %w", err)
		}
	}

	// Store verification data
	key := ContractVerificationKey(verification.Address)
	if err := s.kv(ctx).Set(key, data, nil); err != nil {
		return fmt.Errorf("failed to set contract verification: %w", err)
	}

	// Store index entry for listing verified contracts; a record that is not
	// verified is not listed.
	if verification.IsVerified {
		indexKey := VerifiedContractIndexKey(verification.VerifiedAt.Unix(), verification.Address)
		if err := s.kv(ctx).Set(indexKey, []byte{1}, nil); err != nil {
			return fmt.Errorf("failed to set verified contract index: %w", err)
		}
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
		if err == port.ErrNotFound {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to get verification for deletion: %w", err)
	}

	// Delete verification data
	key := ContractVerificationKey(address)
	if err := s.kv(ctx).Delete(key, nil); err != nil {
		return fmt.Errorf("failed to delete contract verification: %w", err)
	}

	// Delete index entry
	indexKey := VerifiedContractIndexKey(verification.VerifiedAt.Unix(), address)
	if err := s.kv(ctx).Delete(indexKey, nil); err != nil {
		return fmt.Errorf("failed to delete verified contract index: %w", err)
	}

	return nil
}
