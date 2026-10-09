package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.SetCodeIndexReader = (*Store)(nil)
	_ port.SetCodeIndexWriter = (*Store)(nil)
)

// ========== Authorizations ==========

// GetSetCodeAuthorization implements port.SetCodeIndexReader.
func (s *Store) GetSetCodeAuthorization(ctx context.Context, txHash common.Hash, authIndex int) (*port.SetCodeAuthorizationRecord, error) {
	return getJSON[port.SetCodeAuthorizationRecord](ctx, s.q(ctx),
		"SELECT data FROM setcode_authorizations WHERE tx_hash = $1 AND auth_index = $2", txHash.Bytes(), authIndex)
}

// GetSetCodeAuthorizationsByTx implements port.SetCodeIndexReader: in
// authorization list order.
func (s *Store) GetSetCodeAuthorizationsByTx(ctx context.Context, txHash common.Hash) ([]*port.SetCodeAuthorizationRecord, error) {
	return queryJSON[port.SetCodeAuthorizationRecord](ctx, s.q(ctx),
		"SELECT data FROM setcode_authorizations WHERE tx_hash = $1 ORDER BY auth_index", txHash.Bytes())
}

// GetSetCodeAuthorizationsByBlock implements port.SetCodeIndexReader: by
// transaction index, then authorization index.
func (s *Store) GetSetCodeAuthorizationsByBlock(ctx context.Context, blockNumber uint64) ([]*port.SetCodeAuthorizationRecord, error) {
	return queryJSON[port.SetCodeAuthorizationRecord](ctx, s.q(ctx),
		"SELECT data FROM setcode_authorizations WHERE block_number = $1 ORDER BY tx_index, auth_index", i64(blockNumber))
}

// GetSetCodeAuthorizationsByTarget implements port.SetCodeIndexReader.
func (s *Store) GetSetCodeAuthorizationsByTarget(ctx context.Context, target common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	return s.setCodePage(ctx, "setcode-target:"+target.Hex(), "target", target, page)
}

// GetSetCodeAuthorizationsByAuthority implements port.SetCodeIndexReader.
func (s *Store) GetSetCodeAuthorizationsByAuthority(ctx context.Context, authority common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	return s.setCodePage(ctx, "setcode-authority:"+authority.Hex(), "authority", authority, page)
}

// setCodePage reads one page of the authorizations whose column (target or
// authority) is addr, newest first.
func (s *Store) setCodePage(ctx context.Context, list, column string, addr common.Address, page port.Page) ([]*port.SetCodeAuthorizationRecord, string, error) {
	return listQuery[*port.SetCodeAuthorizationRecord]{
		list: list,
		sql:  "SELECT data FROM setcode_authorizations WHERE " + column + " = $1",
		args: []any{addr.Bytes()},
		keys: []keyCol{{"block_number", kindInt, true}, {"tx_index", kindInt, true}, {"auth_index", kindInt, true}},
		scan: scanJSON[port.SetCodeAuthorizationRecord],
		keyOf: func(r *port.SetCodeAuthorizationRecord) []string {
			return []string{u64s(r.BlockNumber), u64s(r.TxIndex), strconv.Itoa(r.AuthIndex)}
		},
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// GetSetCodeAuthorizationsCountByTarget implements port.SetCodeIndexReader.
func (s *Store) GetSetCodeAuthorizationsCountByTarget(ctx context.Context, target common.Address) (int, error) {
	return s.count(ctx, "SELECT count(*) FROM setcode_authorizations WHERE target = $1", target.Bytes())
}

// GetSetCodeAuthorizationsCountByAuthority implements port.SetCodeIndexReader.
func (s *Store) GetSetCodeAuthorizationsCountByAuthority(ctx context.Context, authority common.Address) (int, error) {
	return s.count(ctx, "SELECT count(*) FROM setcode_authorizations WHERE authority = $1", authority.Bytes())
}

// GetSetCodeTransactionCount implements port.SetCodeIndexReader: each
// transaction once, however many authorizations it carries.
func (s *Store) GetSetCodeTransactionCount(ctx context.Context) (int, error) {
	return s.count(ctx, "SELECT count(DISTINCT tx_hash) FROM setcode_authorizations")
}

// GetRecentSetCodeAuthorizations implements port.SetCodeIndexReader.
func (s *Store) GetRecentSetCodeAuthorizations(ctx context.Context, limit int) ([]*port.SetCodeAuthorizationRecord, error) {
	return queryJSON[port.SetCodeAuthorizationRecord](ctx, s.q(ctx),
		"SELECT data FROM setcode_authorizations ORDER BY block_number DESC, tx_index DESC, auth_index DESC LIMIT $1", recentLimit(limit))
}

// count runs a count query.
func (s *Store) count(ctx context.Context, sql string, args ...any) (int, error) {
	var n int
	err := s.q(ctx).QueryRow(ctx, sql, args...).Scan(&n)
	return n, err
}

// SaveSetCodeAuthorization implements port.SetCodeIndexWriter: writing an
// authorization again replaces it.
func (s *Store) SaveSetCodeAuthorization(ctx context.Context, record *port.SetCodeAuthorizationRecord) error {
	return s.SaveSetCodeAuthorizations(ctx, []*port.SetCodeAuthorizationRecord{record})
}

// SaveSetCodeAuthorizations implements port.SetCodeIndexWriter.
func (s *Store) SaveSetCodeAuthorizations(ctx context.Context, records []*port.SetCodeAuthorizationRecord) error {
	if len(records) == 0 {
		return s.write()
	}
	return s.inTx(ctx, func(q querier) error {
		for _, r := range records {
			data, err := json.Marshal(r)
			if err != nil {
				return fmt.Errorf("marshal setcode authorization: %w", err)
			}
			if _, err := q.Exec(ctx, `INSERT INTO setcode_authorizations
				(tx_hash, auth_index, block_number, tx_index, target, authority, data) VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (tx_hash, auth_index) DO UPDATE SET block_number = EXCLUDED.block_number,
					tx_index = EXCLUDED.tx_index, target = EXCLUDED.target, authority = EXCLUDED.authority, data = EXCLUDED.data`,
				r.TxHash.Bytes(), r.AuthIndex, i64(r.BlockNumber), i64(r.TxIndex), r.TargetAddress.Bytes(), r.AuthorityAddress.Bytes(), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// ========== Delegation state and statistics ==========

// GetAddressDelegationState implements port.SetCodeIndexReader.
func (s *Store) GetAddressDelegationState(ctx context.Context, address common.Address) (*port.AddressDelegationState, error) {
	st, err := getJSON[port.AddressDelegationState](ctx, s.q(ctx), "SELECT data FROM setcode_delegations WHERE address = $1", address.Bytes())
	if errors.Is(err, port.ErrNotFound) {
		return &port.AddressDelegationState{Address: address}, nil
	}
	return st, err
}

// UpdateAddressDelegationState implements port.SetCodeIndexWriter. Like the
// Pebble store it fills a zero UpdatedAt with the current time.
func (s *Store) UpdateAddressDelegationState(ctx context.Context, state *port.AddressDelegationState) error {
	if err := s.write(); err != nil {
		return err
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now()
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal delegation state: %w", err)
	}
	_, err = s.q(ctx).Exec(ctx, `INSERT INTO setcode_delegations (address, data) VALUES ($1, $2)
		ON CONFLICT (address) DO UPDATE SET data = EXCLUDED.data`, state.Address.Bytes(), data)
	return err
}

// GetAddressSetCodeStats implements port.SetCodeIndexReader.
// CurrentDelegation follows the address's delegation state.
func (s *Store) GetAddressSetCodeStats(ctx context.Context, address common.Address) (*port.AddressSetCodeStats, error) {
	stats := &port.AddressSetCodeStats{Address: address}
	var block, at int64
	err := s.q(ctx).QueryRow(ctx, `SELECT as_target_count, as_authority_count, last_activity_block, last_activity_time
		FROM setcode_stats WHERE address = $1`, address.Bytes()).Scan(&stats.AsTargetCount, &stats.AsAuthorityCount, &block, &at)
	switch {
	case err == nil:
		stats.LastActivityBlock = uint64(block)
		stats.LastActivityTime = time.Unix(0, at).UTC()
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}
	state, err := s.GetAddressDelegationState(ctx, address)
	if err != nil {
		return nil, err
	}
	if state.HasDelegation && state.DelegationTarget != nil {
		target := *state.DelegationTarget
		stats.CurrentDelegation = &target
	}
	return stats, nil
}

// IncrementSetCodeStats implements port.SetCodeIndexWriter. The activity
// time is the block's time when the block is stored (reindexing gives the
// same stats), the current time otherwise.
func (s *Store) IncrementSetCodeStats(ctx context.Context, address common.Address, asTarget, asAuthority bool, blockNumber uint64) error {
	return s.inTx(ctx, func(q querier) error {
		at := time.Now().UnixNano()
		var blockTime int64
		err := q.QueryRow(ctx, "SELECT time FROM blocks WHERE number = $1", i64(blockNumber)).Scan(&blockTime)
		switch {
		case err == nil:
			at = time.Unix(blockTime, 0).UnixNano()
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		target, authority := 0, 0
		if asTarget {
			target = 1
		}
		if asAuthority {
			authority = 1
		}
		_, err = q.Exec(ctx, `INSERT INTO setcode_stats
			(address, as_target_count, as_authority_count, last_activity_block, last_activity_time) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (address) DO UPDATE SET
				as_target_count = setcode_stats.as_target_count + EXCLUDED.as_target_count,
				as_authority_count = setcode_stats.as_authority_count + EXCLUDED.as_authority_count,
				last_activity_block = EXCLUDED.last_activity_block, last_activity_time = EXCLUDED.last_activity_time`,
			address.Bytes(), target, authority, i64(blockNumber), at)
		return err
	})
}
