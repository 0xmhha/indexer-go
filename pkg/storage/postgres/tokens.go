package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.TokenHolderIndexReader = (*Store)(nil)
	_ port.TokenHolderIndexWriter = (*Store)(nil)
	_ port.TokenMetadataReader    = (*Store)(nil)
	_ port.TokenMetadataWriter    = (*Store)(nil)
)

// ========== Token holders ==========

// The holder count of a token is kept in its stats row, as in the Pebble
// store: a new holder adds one, a holder whose balance becomes zero is
// removed and subtracts one.

const holderColumns = "token, holder, balance::text, last_updated_at"

func scanHolder(row pgx.CollectableRow) (*port.TokenHolder, error) {
	var (
		token, holder []byte
		balance       string
		updated       int64
	)
	if err := row.Scan(&token, &holder, &balance, &updated); err != nil {
		return nil, err
	}
	b, err := bigOf(balance)
	if err != nil {
		return nil, err
	}
	return &port.TokenHolder{TokenAddress: common.BytesToAddress(token), HolderAddress: common.BytesToAddress(holder),
		Balance: b, LastUpdatedAt: uint64(updated)}, nil
}

// UpdateTokenHolder implements port.TokenHolderIndexWriter.
func (s *Store) UpdateTokenHolder(ctx context.Context, h *port.TokenHolder) error {
	if h == nil {
		return fmt.Errorf("token holder cannot be nil")
	}
	return s.inTx(ctx, func(q querier) error { return updateTokenHolder(ctx, q, h) })
}

func updateTokenHolder(ctx context.Context, q querier, h *port.TokenHolder) error {
	var exists bool
	if err := q.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM token_holders WHERE token = $1 AND holder = $2)",
		h.TokenAddress.Bytes(), h.HolderAddress.Bytes()).Scan(&exists); err != nil {
		return err
	}
	if h.Balance == nil || h.Balance.Sign() == 0 {
		if !exists {
			return nil
		}
		if _, err := q.Exec(ctx, "DELETE FROM token_holders WHERE token = $1 AND holder = $2",
			h.TokenAddress.Bytes(), h.HolderAddress.Bytes()); err != nil {
			return err
		}
		return addHolderCount(ctx, q, h.TokenAddress, -1)
	}
	if _, err := q.Exec(ctx, `INSERT INTO token_holders (token, holder, balance, last_updated_at) VALUES ($1, $2, $3::numeric, $4)
		ON CONFLICT (token, holder) DO UPDATE SET balance = EXCLUDED.balance, last_updated_at = EXCLUDED.last_updated_at`,
		h.TokenAddress.Bytes(), h.HolderAddress.Bytes(), h.Balance.String(), i64(h.LastUpdatedAt)); err != nil {
		return err
	}
	if exists {
		return nil
	}
	return addHolderCount(ctx, q, h.TokenAddress, 1)
}

// addHolderCount changes a token's holder count, never below zero,
// creating its stats when missing.
func addHolderCount(ctx context.Context, q querier, token common.Address, delta int) error {
	_, err := q.Exec(ctx, `INSERT INTO token_holder_stats (token, holder_count, transfer_count, last_activity_at)
		VALUES ($1, greatest($2::integer, 0), 0, 0)
		ON CONFLICT (token) DO UPDATE SET holder_count = greatest(token_holder_stats.holder_count + $2::integer, 0)`,
		token.Bytes(), delta)
	return err
}

// UpdateTokenHolderStats implements port.TokenHolderIndexWriter.
func (s *Store) UpdateTokenHolderStats(ctx context.Context, st *port.TokenHolderStats) error {
	if st == nil {
		return fmt.Errorf("token holder stats cannot be nil")
	}
	if err := s.write(); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, `INSERT INTO token_holder_stats (token, holder_count, transfer_count, last_activity_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (token) DO UPDATE SET holder_count = EXCLUDED.holder_count, transfer_count = EXCLUDED.transfer_count,
			last_activity_at = EXCLUDED.last_activity_at`,
		st.TokenAddress.Bytes(), st.HolderCount, st.TransferCount, i64(st.LastActivityAt))
	return err
}

// ProcessERC20TransferForHolders implements port.TokenHolderIndexWriter:
// the sender's balance goes down (never below zero), the recipient's up;
// the zero address (mint, burn) holds nothing.
func (s *Store) ProcessERC20TransferForHolders(ctx context.Context, t *port.ERC20Transfer) error {
	if t == nil {
		return fmt.Errorf("transfer cannot be nil")
	}
	value := t.Value
	if value == nil {
		value = new(big.Int)
	}
	return s.inTx(ctx, func(q querier) error {
		move := func(holder common.Address, sign int) error {
			if holder == (common.Address{}) {
				return nil
			}
			balance, err := tokenBalance(ctx, q, t.ContractAddress, holder)
			if errors.Is(err, port.ErrNotFound) {
				balance, err = new(big.Int), nil
			}
			if err != nil {
				return err
			}
			if sign < 0 {
				balance.Sub(balance, value)
				if balance.Sign() < 0 {
					balance.SetInt64(0)
				}
			} else {
				balance.Add(balance, value)
			}
			return updateTokenHolder(ctx, q, &port.TokenHolder{
				TokenAddress: t.ContractAddress, HolderAddress: holder, Balance: balance, LastUpdatedAt: t.BlockNumber,
			})
		}
		if err := move(t.From, -1); err != nil {
			return fmt.Errorf("update sender balance: %w", err)
		}
		if err := move(t.To, 1); err != nil {
			return fmt.Errorf("update recipient balance: %w", err)
		}
		_, err := q.Exec(ctx, `INSERT INTO token_holder_stats (token, holder_count, transfer_count, last_activity_at)
			VALUES ($1, 0, 1, $2)
			ON CONFLICT (token) DO UPDATE SET transfer_count = token_holder_stats.transfer_count + 1,
				last_activity_at = EXCLUDED.last_activity_at`,
			t.ContractAddress.Bytes(), i64(t.BlockNumber))
		return err
	})
}

func tokenBalance(ctx context.Context, q querier, token, holder common.Address) (*big.Int, error) {
	var balance string
	if err := q.QueryRow(ctx, "SELECT balance::text FROM token_holders WHERE token = $1 AND holder = $2",
		token.Bytes(), holder.Bytes()).Scan(&balance); err != nil {
		return nil, notFound(err)
	}
	return bigOf(balance)
}

// GetTokenBalance implements port.TokenHolderIndexReader.
func (s *Store) GetTokenBalance(ctx context.Context, token, holder common.Address) (*big.Int, error) {
	return tokenBalance(ctx, s.q(ctx), token, holder)
}

// GetTokenHolders implements port.TokenHolderIndexReader: largest balance
// first, equal balances in holder address order.
func (s *Store) GetTokenHolders(ctx context.Context, token common.Address, page port.Page) ([]*port.TokenHolder, string, error) {
	return listQuery[*port.TokenHolder]{
		list:       "holders:" + token.Hex(),
		sql:        "SELECT " + holderColumns + " FROM token_holders WHERE token = $1",
		args:       []any{token.Bytes()},
		keys:       []keyCol{{"token_holders.balance", kindNumeric, true}, {"token_holders.holder", kindBytes, false}},
		defaultAll: true,
		scan:       scanHolder,
		keyOf: func(h *port.TokenHolder) []string {
			return []string{h.Balance.String(), hexOf(h.HolderAddress.Bytes())}
		},
	}.run(ctx, s.q(ctx), page)
}

// GetHolderTokens implements port.TokenHolderIndexReader.
func (s *Store) GetHolderTokens(ctx context.Context, holder common.Address, page port.Page) ([]*port.TokenHolder, string, error) {
	return listQuery[*port.TokenHolder]{
		list:       "held:" + holder.Hex(),
		sql:        "SELECT " + holderColumns + " FROM token_holders WHERE holder = $1",
		args:       []any{holder.Bytes()},
		keys:       []keyCol{{"token", kindBytes, false}},
		defaultAll: true,
		scan:       scanHolder,
		keyOf:      func(h *port.TokenHolder) []string { return []string{hexOf(h.TokenAddress.Bytes())} },
	}.run(ctx, s.q(ctx), page)
}

// GetTokenHolderCount implements port.TokenHolderIndexReader: the count of
// the token's stats, or its holders when it has none.
func (s *Store) GetTokenHolderCount(ctx context.Context, token common.Address) (int, error) {
	var n int
	err := s.q(ctx).QueryRow(ctx, `SELECT COALESCE(
		(SELECT holder_count FROM token_holder_stats WHERE token = $1),
		(SELECT count(*) FROM token_holders WHERE token = $1))`, token.Bytes()).Scan(&n)
	return n, err
}

// GetTokenHolderStats implements port.TokenHolderIndexReader.
func (s *Store) GetTokenHolderStats(ctx context.Context, token common.Address) (*port.TokenHolderStats, error) {
	var (
		holders, transfers int32
		last               int64
	)
	err := s.q(ctx).QueryRow(ctx, "SELECT holder_count, transfer_count, last_activity_at FROM token_holder_stats WHERE token = $1",
		token.Bytes()).Scan(&holders, &transfers, &last)
	if err != nil {
		return nil, notFound(err)
	}
	return &port.TokenHolderStats{TokenAddress: token, HolderCount: int(holders), TransferCount: int(transfers), LastActivityAt: uint64(last)}, nil
}

// ========== Token metadata ==========

const metadataColumns = `address, standard, name, symbol, decimals, total_supply::text, base_uri, detected_at,
	created_at, updated_at, supports_erc165, supports_metadata, supports_enumerable`

func scanMetadata(row pgx.CollectableRow) (*port.TokenMetadata, error) {
	var (
		addr                       []byte
		standard, name, symbol     string
		decimals                   int16
		supply                     *string
		baseURI                    string
		detected, created, updated int64
		erc165, meta, enumerable   bool
	)
	if err := row.Scan(&addr, &standard, &name, &symbol, &decimals, &supply, &baseURI, &detected,
		&created, &updated, &erc165, &meta, &enumerable); err != nil {
		return nil, err
	}
	// Times read back in the local zone, as the Pebble store returns the
	// times it wrote with time.Unix: the API renders them the same way.
	m := &port.TokenMetadata{
		Address: common.BytesToAddress(addr), Standard: port.TokenStandard(standard), Name: name, Symbol: symbol,
		Decimals: uint8(decimals), BaseURI: baseURI, DetectedAt: uint64(detected),
		CreatedAt: time.Unix(0, created).UTC(), UpdatedAt: time.Unix(0, updated).UTC(),
		SupportsERC165: erc165, SupportsMetadata: meta, SupportsEnumerable: enumerable,
	}
	if supply != nil {
		v, err := bigOf(*supply)
		if err != nil {
			return nil, err
		}
		m.TotalSupply = v
	}
	return m, nil
}

// SaveTokenMetadata implements port.TokenMetadataWriter.
func (s *Store) SaveTokenMetadata(ctx context.Context, m *port.TokenMetadata) error {
	if m == nil {
		return fmt.Errorf("token metadata cannot be nil")
	}
	if err := s.write(); err != nil {
		return err
	}
	var supply *string
	if m.TotalSupply != nil {
		v := m.TotalSupply.String()
		supply = &v
	}
	_, err := s.q(ctx).Exec(ctx, `INSERT INTO token_metadata (address, standard, name, symbol, decimals, total_supply,
			base_uri, detected_at, created_at, updated_at, supports_erc165, supports_metadata, supports_enumerable)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (address) DO UPDATE SET standard = EXCLUDED.standard, name = EXCLUDED.name, symbol = EXCLUDED.symbol,
			decimals = EXCLUDED.decimals, total_supply = EXCLUDED.total_supply, base_uri = EXCLUDED.base_uri,
			detected_at = EXCLUDED.detected_at, created_at = EXCLUDED.created_at, updated_at = EXCLUDED.updated_at,
			supports_erc165 = EXCLUDED.supports_erc165, supports_metadata = EXCLUDED.supports_metadata,
			supports_enumerable = EXCLUDED.supports_enumerable`,
		m.Address.Bytes(), string(m.Standard), m.Name, m.Symbol, int16(m.Decimals), supply, m.BaseURI, i64(m.DetectedAt),
		m.CreatedAt.UnixNano(), m.UpdatedAt.UnixNano(), m.SupportsERC165, m.SupportsMetadata, m.SupportsEnumerable)
	return err
}

// DeleteTokenMetadata implements port.TokenMetadataWriter.
func (s *Store) DeleteTokenMetadata(ctx context.Context, address common.Address) error {
	if err := s.write(); err != nil {
		return err
	}
	_, err := s.q(ctx).Exec(ctx, "DELETE FROM token_metadata WHERE address = $1", address.Bytes())
	return err
}

// GetTokenMetadata implements port.TokenMetadataReader.
func (s *Store) GetTokenMetadata(ctx context.Context, address common.Address) (*port.TokenMetadata, error) {
	rows, err := s.q(ctx).Query(ctx, "SELECT "+metadataColumns+" FROM token_metadata WHERE address = $1", address.Bytes())
	if err != nil {
		return nil, err
	}
	m, err := pgx.CollectExactlyOneRow(rows, scanMetadata)
	return m, notFound(err)
}

// ListTokensByStandard implements port.TokenMetadataReader: in address
// order; an empty standard lists every token.
func (s *Store) ListTokensByStandard(ctx context.Context, standard port.TokenStandard, page port.Page) ([]*port.TokenMetadata, string, error) {
	lq := listQuery[*port.TokenMetadata]{
		list:       "tokens:" + string(standard),
		sql:        "SELECT " + metadataColumns + " FROM token_metadata WHERE true",
		keys:       []keyCol{{"address", kindBytes, false}},
		defaultAll: true,
		scan:       scanMetadata,
		keyOf:      func(m *port.TokenMetadata) []string { return []string{hexOf(m.Address.Bytes())} },
	}
	if standard != "" {
		lq.sql, lq.args = "SELECT "+metadataColumns+" FROM token_metadata WHERE standard = $1", []any{string(standard)}
	}
	return lq.run(ctx, s.q(ctx), page)
}

// GetTokensCount implements port.TokenMetadataReader.
func (s *Store) GetTokensCount(ctx context.Context, standard port.TokenStandard) (int, error) {
	var n int
	var err error
	if standard == "" {
		err = s.q(ctx).QueryRow(ctx, "SELECT count(*) FROM token_metadata").Scan(&n)
	} else {
		err = s.q(ctx).QueryRow(ctx, "SELECT count(*) FROM token_metadata WHERE standard = $1", string(standard)).Scan(&n)
	}
	return n, err
}

// SearchTokens implements port.TokenMetadataReader: names and symbols
// containing the query, ignoring case; an empty query matches nothing.
func (s *Store) SearchTokens(ctx context.Context, query string, limit int) ([]*port.TokenMetadata, error) {
	if query == "" {
		return nil, nil
	}
	sql := "SELECT " + metadataColumns + ` FROM token_metadata
		WHERE strpos(lower(name), $1) > 0 OR strpos(lower(symbol), $1) > 0 ORDER BY address`
	if limit > 0 {
		sql += " LIMIT " + itoa(limit)
	}
	rows, err := s.q(ctx).Query(ctx, sql, strings.ToLower(query))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanMetadata)
}
