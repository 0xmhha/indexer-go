package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.ABIReader                  = (*Store)(nil)
	_ port.ABIWriter                  = (*Store)(nil)
	_ port.ContractVerificationReader = (*Store)(nil)
	_ port.ContractVerificationWriter = (*Store)(nil)
	_ port.SearchReader               = (*Store)(nil)
)

// ========== ABIs ==========

// GetABI implements port.ABIReader.
func (s *Store) GetABI(ctx context.Context, address common.Address) ([]byte, error) {
	var abi []byte
	if err := s.q(ctx).QueryRow(ctx, "SELECT abi FROM abis WHERE address = $1", address.Bytes()).Scan(&abi); err != nil {
		return nil, notFound(err)
	}
	return abi, nil
}

// HasABI implements port.ABIReader.
func (s *Store) HasABI(ctx context.Context, address common.Address) (bool, error) {
	return s.exists(ctx, "SELECT EXISTS (SELECT 1 FROM abis WHERE address = $1)", address.Bytes())
}

// ListABIs implements port.ABIReader: in address order.
func (s *Store) ListABIs(ctx context.Context) ([]common.Address, error) {
	rows, err := s.q(ctx).Query(ctx, "SELECT address FROM abis ORDER BY address")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAddress)
}

// scanAddress reads a row of one address column.
func scanAddress(row pgx.CollectableRow) (common.Address, error) {
	var b []byte
	err := row.Scan(&b)
	return common.BytesToAddress(b), err
}

// SetABI implements port.ABIWriter.
func (s *Store) SetABI(ctx context.Context, address common.Address, abiJSON []byte) error {
	if len(abiJSON) == 0 {
		return fmt.Errorf("ABI JSON cannot be empty")
	}
	if err := s.write(); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, `INSERT INTO abis (address, abi) VALUES ($1, $2)
		ON CONFLICT (address) DO UPDATE SET abi = EXCLUDED.abi`, address.Bytes(), bytes.Clone(abiJSON))
	return err
}

// DeleteABI implements port.ABIWriter.
func (s *Store) DeleteABI(ctx context.Context, address common.Address) error {
	if err := s.write(); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, "DELETE FROM abis WHERE address = $1", address.Bytes())
	return err
}

// ========== Contract verification ==========

// defaultVerifiedContractsLimit is the page size of the verified contract
// list when none is asked for, as in the Pebble store.
const defaultVerifiedContractsLimit = 100

// GetContractVerification implements port.ContractVerificationReader.
func (s *Store) GetContractVerification(ctx context.Context, address common.Address) (*port.ContractVerification, error) {
	return getJSON[port.ContractVerification](ctx, s.q(ctx),
		"SELECT data FROM contract_verifications WHERE address = $1", address.Bytes())
}

// IsContractVerified implements port.ContractVerificationReader.
func (s *Store) IsContractVerified(ctx context.Context, address common.Address) (bool, error) {
	var ok bool
	err := s.q(ctx).QueryRow(ctx, "SELECT is_verified FROM contract_verifications WHERE address = $1", address.Bytes()).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

// ListVerifiedContracts implements port.ContractVerificationReader: by
// verification time (seconds), then address.
func (s *Store) ListVerifiedContracts(ctx context.Context, page port.Page) ([]common.Address, string, error) {
	if page.Limit <= 0 {
		page.Limit = defaultVerifiedContractsLimit
	}
	type entry struct {
		address    common.Address
		verifiedAt int64
	}
	entries, next, err := listQuery[entry]{
		list: "verified",
		sql:  "SELECT address, verified_at FROM contract_verifications WHERE is_verified",
		keys: []keyCol{{"verified_at", kindInt, false}, {"address", kindBytes, false}},
		scan: func(row pgx.CollectableRow) (entry, error) {
			var (
				b  []byte
				at int64
			)
			err := row.Scan(&b, &at)
			return entry{common.BytesToAddress(b), at}, err
		},
		keyOf: func(e entry) []string {
			return []string{strconv.FormatInt(e.verifiedAt, 10), hexOf(e.address.Bytes())}
		},
	}.run(ctx, s.q(ctx), page)
	if err != nil {
		return nil, "", err
	}
	out := make([]common.Address, len(entries))
	for i, e := range entries {
		out[i] = e.address
	}
	return out, next, nil
}

// CountVerifiedContracts implements port.ContractVerificationReader.
func (s *Store) CountVerifiedContracts(ctx context.Context) (int, error) {
	return s.count(ctx, "SELECT count(*) FROM contract_verifications WHERE is_verified")
}

// SetContractVerification implements port.ContractVerificationWriter: a
// record replaces the address's earlier one.
func (s *Store) SetContractVerification(ctx context.Context, v *port.ContractVerification) error {
	if v == nil {
		return fmt.Errorf("verification cannot be nil")
	}
	if err := s.write(); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode contract verification: %w", err)
	}
	_, err = s.q(ctx).Exec(ctx, `INSERT INTO contract_verifications (address, is_verified, verified_at, data) VALUES ($1, $2, $3, $4)
		ON CONFLICT (address) DO UPDATE SET is_verified = EXCLUDED.is_verified, verified_at = EXCLUDED.verified_at, data = EXCLUDED.data`,
		v.Address.Bytes(), v.IsVerified, v.VerifiedAt.Unix(), data)
	return err
}

// DeleteContractVerification implements port.ContractVerificationWriter.
func (s *Store) DeleteContractVerification(ctx context.Context, address common.Address) error {
	if err := s.write(); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, "DELETE FROM contract_verifications WHERE address = $1", address.Bytes())
	return err
}

// ========== Search ==========

// Search implements port.SearchReader (port.RunSearch).
func (s *Store) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]port.SearchResult, error) {
	return port.RunSearch(ctx, s, query, resultTypes, limit)
}

// CountAddressTransactions returns the number of transactions in an
// address's transaction index (port.SearchSource).
func (s *Store) CountAddressTransactions(ctx context.Context, address common.Address) (int, error) {
	return s.count(ctx, "SELECT count(*) FROM address_transactions WHERE address = $1", address.Bytes())
}
