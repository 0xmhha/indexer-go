package storage

import (
	"context"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
)

// ========== ABI Reader Methods ==========

// GetABI returns the ABI for a contract
func (s *PebbleStorage) GetABI(ctx context.Context, address common.Address) ([]byte, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := ABIKey(address)
	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get ABI: %w", err)
	}
	defer closer.Close()

	// Copy the value since it's only valid until closer is called
	result := make([]byte, len(value))
	copy(result, value)

	return result, nil
}

// HasABI checks if an ABI exists for a contract
func (s *PebbleStorage) HasABI(ctx context.Context, address common.Address) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}

	key := ABIKey(address)
	_, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, fmt.Errorf("failed to check ABI: %w", err)
	}
	closer.Close()

	return true, nil
}

// ListABIs returns all contract addresses that have ABIs
func (s *PebbleStorage) ListABIs(ctx context.Context) ([]common.Address, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	prefix := ABIKeyPrefix()
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: append(prefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var addresses []common.Address

	for iter.First(); iter.Valid(); iter.Next() {
		// Extract address from key
		// Key format: /data/abi/{address}
		key := string(iter.Key())
		addrHex := key[len(string(prefix)):]

		if !common.IsHexAddress(addrHex) {
			continue // Skip invalid keys
		}

		addresses = append(addresses, common.HexToAddress(addrHex))
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return addresses, nil
}

// ========== ABI Writer Methods ==========

// SetABI stores an ABI for a contract
func (s *PebbleStorage) SetABI(ctx context.Context, address common.Address, abiJSON []byte) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	if len(abiJSON) == 0 {
		return fmt.Errorf("ABI JSON cannot be empty")
	}

	key := ABIKey(address)
	if err := s.db.Set(key, abiJSON, nil); err != nil {
		return fmt.Errorf("failed to set ABI: %w", err)
	}

	return nil
}

// DeleteABI removes an ABI for a contract
func (s *PebbleStorage) DeleteABI(ctx context.Context, address common.Address) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := ABIKey(address)
	if err := s.db.Delete(key, nil); err != nil {
		return fmt.Errorf("failed to delete ABI: %w", err)
	}

	return nil
}
