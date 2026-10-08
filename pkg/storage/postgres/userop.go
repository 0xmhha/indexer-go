package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/userop"
)

var (
	_ port.UserOpIndexReader = (*Store)(nil)
	_ port.UserOpIndexWriter = (*Store)(nil)
)

// ========== UserOperations ==========

// GetUserOp implements port.UserOpIndexReader.
func (s *Store) GetUserOp(ctx context.Context, opHash common.Hash) (*userop.UserOperation, error) {
	return getJSON[userop.UserOperation](ctx, s.q(ctx), "SELECT data FROM user_operations WHERE hash = $1", opHash.Bytes())
}

// GetUserOpsByTx implements port.UserOpIndexReader: in bundle order.
func (s *Store) GetUserOpsByTx(ctx context.Context, txHash common.Hash) ([]*userop.UserOperation, error) {
	return queryJSON[userop.UserOperation](ctx, s.q(ctx),
		"SELECT data FROM user_operations WHERE tx_hash = $1 ORDER BY bundle_index", txHash.Bytes())
}

// GetUserOpsByBlock implements port.UserOpIndexReader: by transaction hash,
// then bundle order (the Pebble block index order).
func (s *Store) GetUserOpsByBlock(ctx context.Context, blockNumber uint64) ([]*userop.UserOperation, error) {
	return queryJSON[userop.UserOperation](ctx, s.q(ctx),
		"SELECT data FROM user_operations WHERE block_number = $1 ORDER BY tx_hash, bundle_index", i64(blockNumber))
}

// GetUserOpsBySender implements port.UserOpIndexReader.
func (s *Store) GetUserOpsBySender(ctx context.Context, sender common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	return s.userOpPage(ctx, "userop-sender:"+sender.Hex(), "sender", sender, page)
}

// GetUserOpsByBundler implements port.UserOpIndexReader.
func (s *Store) GetUserOpsByBundler(ctx context.Context, bundler common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	return s.userOpPage(ctx, "userop-bundler:"+bundler.Hex(), "bundler", bundler, page)
}

// GetUserOpsByPaymaster implements port.UserOpIndexReader.
func (s *Store) GetUserOpsByPaymaster(ctx context.Context, paymaster common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	return s.userOpPage(ctx, "userop-paymaster:"+paymaster.Hex(), "paymaster", paymaster, page)
}

// GetUserOpsByFactory implements port.UserOpIndexReader.
func (s *Store) GetUserOpsByFactory(ctx context.Context, factory common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	return s.userOpPage(ctx, "userop-factory:"+factory.Hex(), "factory", factory, page)
}

// userOpPage reads one page of the operations whose column is addr, newest
// first.
func (s *Store) userOpPage(ctx context.Context, list, column string, addr common.Address, page port.Page) ([]*userop.UserOperation, string, error) {
	return listQuery[*userop.UserOperation]{
		list: list,
		sql:  "SELECT data FROM user_operations WHERE " + column + " = $1",
		args: []any{addr.Bytes()},
		keys: []keyCol{{"block_number", kindInt, true}, {"tx_hash", kindBytes, true}, {"bundle_index", kindInt, true}},
		scan: scanJSON[userop.UserOperation],
		keyOf: func(op *userop.UserOperation) []string {
			return []string{u64s(op.BlockNumber), hexOf(op.TransactionHash.Bytes()), u64s(uint64(op.BundleIndex))}
		},
	}.run(ctx, s.q(ctx), cappedPage(page))
}

// GetRecentUserOps implements port.UserOpIndexReader.
func (s *Store) GetRecentUserOps(ctx context.Context, limit int) ([]*userop.UserOperation, error) {
	return queryJSON[userop.UserOperation](ctx, s.q(ctx),
		"SELECT data FROM user_operations ORDER BY block_number DESC, tx_hash DESC, bundle_index DESC LIMIT $1", recentLimit(limit))
}

// GetUserOpCount implements port.UserOpIndexReader.
func (s *Store) GetUserOpCount(ctx context.Context) (int, error) {
	return s.count(ctx, "SELECT count(*) FROM user_operations")
}

// optionalAddr returns an address column value: NULL for none or the zero
// address.
func optionalAddr(a *common.Address) []byte {
	if a == nil || *a == (common.Address{}) {
		return nil
	}
	return a.Bytes()
}

// SaveUserOp implements port.UserOpIndexWriter: writing an operation again
// replaces it.
func (s *Store) SaveUserOp(ctx context.Context, op *userop.UserOperation) error {
	return s.SaveUserOps(ctx, []*userop.UserOperation{op})
}

// SaveUserOps implements port.UserOpIndexWriter.
func (s *Store) SaveUserOps(ctx context.Context, ops []*userop.UserOperation) error {
	if len(ops) == 0 {
		return s.write()
	}
	return s.inTx(ctx, func(q querier) error {
		for _, op := range ops {
			data, err := json.Marshal(op)
			if err != nil {
				return fmt.Errorf("marshal userop: %w", err)
			}
			if _, err := q.Exec(ctx, `INSERT INTO user_operations
				(hash, sender, bundler, paymaster, factory, block_number, tx_hash, bundle_index, data)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				ON CONFLICT (hash) DO UPDATE SET sender = EXCLUDED.sender, bundler = EXCLUDED.bundler,
					paymaster = EXCLUDED.paymaster, factory = EXCLUDED.factory, block_number = EXCLUDED.block_number,
					tx_hash = EXCLUDED.tx_hash, bundle_index = EXCLUDED.bundle_index, data = EXCLUDED.data`,
				op.Hash.Bytes(), op.Sender.Bytes(), op.Bundler.Bytes(), optionalAddr(op.Paymaster), optionalAddr(op.Factory),
				i64(op.BlockNumber), op.TransactionHash.Bytes(), int64(op.BundleIndex), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// ========== Statistics and smart accounts ==========

// The bundler, factory and paymaster statistics and the smart accounts are
// kept as given, one JSON record per address; lists are in address order.

// GetBundlerStats implements port.UserOpIndexReader.
func (s *Store) GetBundlerStats(ctx context.Context, bundler common.Address) (*userop.BundlerStats, error) {
	return getRecordOr(ctx, s, "bundler_stats", bundler, &userop.BundlerStats{Address: bundler})
}

// GetFactoryStats implements port.UserOpIndexReader.
func (s *Store) GetFactoryStats(ctx context.Context, factory common.Address) (*userop.FactoryStats, error) {
	return getRecordOr(ctx, s, "factory_stats", factory, &userop.FactoryStats{Address: factory})
}

// GetPaymasterStats implements port.UserOpIndexReader.
func (s *Store) GetPaymasterStats(ctx context.Context, paymaster common.Address) (*userop.PaymasterStats, error) {
	return getRecordOr(ctx, s, "paymaster_stats", paymaster, &userop.PaymasterStats{Address: paymaster})
}

// GetSmartAccount implements port.UserOpIndexReader.
func (s *Store) GetSmartAccount(ctx context.Context, address common.Address) (*userop.SmartAccount, error) {
	return getJSON[userop.SmartAccount](ctx, s.q(ctx), "SELECT data FROM smart_accounts WHERE address = $1", address.Bytes())
}

// ListBundlers implements port.UserOpIndexReader.
func (s *Store) ListBundlers(ctx context.Context, page port.Page) ([]*userop.BundlerStats, string, error) {
	return listRecords(ctx, s, "bundler_stats", page, func(r *userop.BundlerStats) common.Address { return r.Address })
}

// ListFactories implements port.UserOpIndexReader.
func (s *Store) ListFactories(ctx context.Context, page port.Page) ([]*userop.FactoryStats, string, error) {
	return listRecords(ctx, s, "factory_stats", page, func(r *userop.FactoryStats) common.Address { return r.Address })
}

// ListPaymasters implements port.UserOpIndexReader.
func (s *Store) ListPaymasters(ctx context.Context, page port.Page) ([]*userop.PaymasterStats, string, error) {
	return listRecords(ctx, s, "paymaster_stats", page, func(r *userop.PaymasterStats) common.Address { return r.Address })
}

// ListSmartAccounts implements port.UserOpIndexReader.
func (s *Store) ListSmartAccounts(ctx context.Context, page port.Page) ([]*userop.SmartAccount, string, error) {
	return listRecords(ctx, s, "smart_accounts", page, func(r *userop.SmartAccount) common.Address { return r.Address })
}

// UpdateBundlerStats implements port.UserOpIndexWriter.
func (s *Store) UpdateBundlerStats(ctx context.Context, stats *userop.BundlerStats) error {
	return s.putRecord(ctx, "bundler_stats", stats.Address, stats)
}

// UpdateFactoryStats implements port.UserOpIndexWriter.
func (s *Store) UpdateFactoryStats(ctx context.Context, stats *userop.FactoryStats) error {
	return s.putRecord(ctx, "factory_stats", stats.Address, stats)
}

// UpdatePaymasterStats implements port.UserOpIndexWriter.
func (s *Store) UpdatePaymasterStats(ctx context.Context, stats *userop.PaymasterStats) error {
	return s.putRecord(ctx, "paymaster_stats", stats.Address, stats)
}

// SaveSmartAccount implements port.UserOpIndexWriter.
func (s *Store) SaveSmartAccount(ctx context.Context, account *userop.SmartAccount) error {
	return s.putRecord(ctx, "smart_accounts", account.Address, account)
}

// putRecord stores the JSON record of an address in table (address, data),
// replacing an earlier one.
func (s *Store) putRecord(ctx context.Context, table string, address common.Address, record any) error {
	if err := s.write(); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", table, err)
	}
	_, err = s.q(ctx).Exec(ctx, "INSERT INTO "+table+` (address, data) VALUES ($1, $2)
		ON CONFLICT (address) DO UPDATE SET data = EXCLUDED.data`, address.Bytes(), data)
	return err
}

// getRecordOr reads the JSON record of an address in table (address, data),
// or returns zero when there is none.
func getRecordOr[T any](ctx context.Context, s *Store, table string, address common.Address, zero *T) (*T, error) {
	v, err := getJSON[T](ctx, s.q(ctx), "SELECT data FROM "+table+" WHERE address = $1", address.Bytes())
	if errors.Is(err, port.ErrNotFound) {
		return zero, nil
	}
	return v, err
}

// listRecords reads one page of the JSON records of table (address, data),
// in address order.
func listRecords[T any](ctx context.Context, s *Store, table string, page port.Page, addressOf func(*T) common.Address) ([]*T, string, error) {
	return listQuery[*T]{
		list: table,
		sql:  "SELECT data FROM " + table + " WHERE true",
		keys: []keyCol{{"address", kindBytes, false}},
		scan: scanJSON[T],
		keyOf: func(r *T) []string {
			return []string{hexOf(addressOf(r).Bytes())}
		},
	}.run(ctx, s.q(ctx), cappedPage(page))
}
