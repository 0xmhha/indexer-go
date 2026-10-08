package postgres

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/history"
)

// The methods the application uses to assemble a store besides the ports
// (pkg/storage.Storage and the optional setters pkg/app looks for).

// SetLogger sets the logger of the store's warnings.
func (s *Store) SetLogger(logger *zap.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

// Compact does nothing: PostgreSQL reclaims space with autovacuum.
func (s *Store) Compact(ctx context.Context, start, end []byte) error { return nil }

// SetTokenMetadataFetcher sets where GetTokenBalances reads the metadata of
// tokens the store has none for.
func (s *Store) SetTokenMetadataFetcher(fetcher port.TokenMetadataFetcher) { s.tokenFetcher = fetcher }

// describeToken fills in a token balance's metadata (history.DescribeToken).
func (s *Store) describeToken(ctx context.Context, tb *port.TokenBalance) {
	history.DescribeToken(ctx, tb, s, s.tokenFetcher, s.logger)
}

// ========== Genesis allocation ==========

// SetGenesisBalanceResolver enables the genesis lookup: GetAddressBalance
// answers an early block's zero balance of an account with no recorded
// balance with the node's balance at block 0, and records it. Each account
// is looked up once.
func (s *Store) SetGenesisBalanceResolver(source port.BalanceSource) {
	s.genesisMu.Lock()
	defer s.genesisMu.Unlock()
	s.genesisSource = source
	if s.genesisTried == nil {
		s.genesisTried = make(map[common.Address]bool)
	}
}

// genesisBalance performs the lookup for GetAddressBalance, which found
// balance (zero) for addr at blockNumber. Failures are logged and balance is
// returned.
func (s *Store) genesisBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) *big.Int {
	if blockNumber >= port.GenesisLookupMaxBlock {
		return balance
	}
	s.genesisMu.Lock()
	source, tried := s.genesisSource, s.genesisTried[addr]
	s.genesisMu.Unlock()
	bt := s.boundTx(ctx)
	if source == nil || tried || (bt != nil && bt.genesisSeen[addr]) {
		return balance
	}
	// Inside a block transaction the attempt joins the memo when it
	// commits, so a rolled back block looks the account up again.
	if bt != nil {
		if bt.genesisSeen == nil {
			bt.genesisSeen = make(map[common.Address]bool)
		}
		bt.genesisSeen[addr] = true
	} else {
		s.publishGenesisTried(map[common.Address]bool{addr: true})
	}

	if has, err := s.HasBalanceRecord(ctx, addr); err != nil || has {
		return balance
	}
	allocation, err := source.BalanceAt(ctx, addr, big.NewInt(0))
	if err != nil {
		s.logger.Debug("failed to fetch genesis balance from RPC", zap.String("address", addr.Hex()), zap.Error(err))
		return balance
	}
	if allocation.Sign() == 0 {
		return balance
	}
	if s.readOnly {
		// An API process answers with the allocation; the indexing process
		// records it when it reads the account.
		return allocation
	}
	s.logger.Info("auto-initializing genesis allocation balance",
		zap.String("address", addr.Hex()), zap.String("balance", allocation.String()))
	if err := s.SetBalance(ctx, addr, 0, allocation); err != nil {
		s.logger.Error("failed to initialize genesis balance in storage", zap.String("address", addr.Hex()), zap.Error(err))
	}
	return allocation
}

func (s *Store) publishGenesisTried(seen map[common.Address]bool) {
	if len(seen) == 0 {
		return
	}
	s.genesisMu.Lock()
	defer s.genesisMu.Unlock()
	if s.genesisTried == nil {
		s.genesisTried = make(map[common.Address]bool)
	}
	for addr := range seen {
		s.genesisTried[addr] = true
	}
}

// resetGenesisTried forgets the lookups, so those a rollback undid happen
// again.
func (s *Store) resetGenesisTried() {
	s.genesisMu.Lock()
	s.genesisTried = make(map[common.Address]bool)
	s.genesisMu.Unlock()
}

// ========== Reindex ==========

// preservedTables keep their rows when chain data is cleared: the schema
// version, contract verification (user data) and the outbox numbering and
// cursors, so events of the new indexing continue the sequence consumers
// have seen (as the Pebble store's Preserved keyspaces).
var preservedTables = map[string]bool{
	"schema_migrations": true, "abis": true, "contract_verifications": true,
	"outbox_sequence": true, "outbox_cursors": true,
}

// ClearChainData deletes the indexed chain data for a reindex: every table
// except preservedTables, and the kv rows under kvPrefixes (the chain data
// keyspaces registered with pkg/storage). With all, the preserved tables
// (but the schema version) and every kv row go too, as clearing a Pebble
// data folder does. It returns the tables cleared.
func (s *Store) ClearChainData(ctx context.Context, kvPrefixes []string, all bool) ([]string, error) {
	if err := s.write(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind = 'r' ORDER BY c.relname`)
	if err != nil {
		return nil, err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		keep := preservedTables[name] && (!all || name == "schema_migrations")
		if !keep && name != "kv" {
			tables = append(tables, name)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	err = s.inTx(ctx, func(q querier) error {
		if len(tables) > 0 {
			quoted := make([]string, len(tables))
			for i, t := range tables {
				quoted[i] = `"` + t + `"`
			}
			if _, err := q.Exec(ctx, "TRUNCATE "+strings.Join(quoted, ", ")); err != nil {
				return fmt.Errorf("truncate chain data: %w", err)
			}
		}
		if all {
			kvPrefixes = []string{""} // every key
		}
		for _, p := range kvPrefixes {
			if end := prefixEnd([]byte(p)); end != nil {
				_, err = q.Exec(ctx, "DELETE FROM kv WHERE key >= $1 AND key < $2", []byte(p), end)
			} else {
				_, err = q.Exec(ctx, "DELETE FROM kv WHERE key >= $1", []byte(p))
			}
			if err != nil {
				return fmt.Errorf("delete kv prefix %q: %w", p, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.resetGenesisTried()
	return append(tables, "kv"), nil
}
