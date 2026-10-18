package postgres

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

func TestSQLConnectionUsesServerParameterBinding(t *testing.T) {
	for _, mode := range []string{"", "cache_statement", "cache_describe", "describe_exec", "exec", "simple_protocol"} {
		t.Run(mode, func(t *testing.T) {
			dsn := "postgres://unused@localhost/test"
			if mode != "" {
				dsn += "?default_query_exec_mode=" + mode
			}
			cfg, schema, err := poolConfig(Options{DSN: dsn, Schema: "select", MaxConns: 3})
			require.NoError(t, err)
			assert.Equal(t, "select", schema)
			assert.Equal(t, `"select"`, cfg.ConnConfig.RuntimeParams["search_path"])
			assert.Equal(t, int32(3), cfg.MaxConns)
			assert.NotEqual(t, pgx.QueryExecModeSimpleProtocol, cfg.ConnConfig.DefaultQueryExecMode)
			if mode == "" {
				assert.Equal(t, pgx.QueryExecModeCacheStatement, cfg.ConnConfig.DefaultQueryExecMode)
			}
		})
	}
	for _, schema := range []string{`x"; DROP SCHEMA public CASCADE;--`, "public,pg_catalog", "a.b", strings.Repeat("a", 64)} {
		_, _, err := poolConfig(Options{Schema: schema})
		require.ErrorContains(t, err, "invalid schema name")
	}
}

// Exercise real storage entry points, including queued writes and cursor
// continuation. SQL-looking values must round-trip as data without broadening
// a predicate, executing a second command, or changing another record.
func TestSQLInjectionValuesRemainData(t *testing.T) {
	for _, mode := range []string{"cache_statement", "simple_protocol"} {
		t.Run(mode, func(t *testing.T) {
			dsn := testDSN(t)
			if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
				u, err := url.Parse(dsn)
				require.NoError(t, err)
				q := u.Query()
				q.Set("default_query_exec_mode", mode)
				u.RawQuery = q.Encode()
				dsn = u.String()
			} else {
				dsn += " default_query_exec_mode=" + mode
			}
			s, err := Open(context.Background(), Options{DSN: dsn, Schema: newTestSchema(t), MaxConns: 4})
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.Close() })
			checkSQLInjectionValues(t, s)
		})
	}
}

func checkSQLInjectionValues(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	payload := `x'); DELETE FROM abis; --`
	addr := common.Address{1}
	require.NoError(t, s.SetABI(ctx, addr, []byte("[]")))
	require.NoError(t, s.Put(ctx, []byte(payload), []byte(payload)))
	got, err := s.Get(ctx, []byte(payload))
	require.NoError(t, err)
	assert.Equal(t, []byte(payload), got)

	txCtx, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	for i := uint(0); i < 3; i++ {
		r := &port.Record{Table: payload, BlockNumber: 1, LogIndex: i, Fields: map[string]string{"value": payload}}
		require.NoError(t, s.SaveRecord(txCtx, r, []port.RecordKey{{ID: payload, Values: []string{payload}}}))
	}
	require.NoError(t, s.SaveRecord(txCtx, &port.Record{Table: "keep", BlockNumber: 2}, nil))
	require.NoError(t, s.SetOutboxCursor(txCtx, payload, 9))
	// Extended exec has no described parameter OIDs. JSON must bind as text,
	// not []byte/bytea; this also exercises queued writes in that mode.
	feature := port.FeatureState{Active: true, Through: 1, Definition: payload}
	require.NoError(t, s.SetFeatureState(txCtx, payload, feature))
	require.NoError(t, tx.Commit())
	states, err := s.FeatureStates(ctx)
	require.NoError(t, err)
	assert.Equal(t, feature, states[payload])

	rows, next, err := s.ListRecords(ctx, payload, port.Page{Limit: 1, Offset: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, uint(1), rows[0].LogIndex)
	require.NotEmpty(t, next)
	rows, _, err = s.ListRecords(ctx, payload, port.Page{Limit: 1, After: next, Offset: 99})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, uint(2), rows[0].LogIndex)
	rows, _, err = s.ListRecordsByKey(ctx, payload, port.RecordKey{ID: payload, Values: []string{payload}}, port.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	_, _, err = s.ListRecords(ctx, payload, port.Page{After: encodeCursor("records:"+payload, "1 OR 1=1;--", "0")})
	require.Error(t, err, "a forged numeric cursor must not become SQL")

	seq, exists, err := s.OutboxCursor(ctx, payload)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, uint64(9), seq)

	metadata := &port.TokenMetadata{Address: common.Address{2}, Name: payload, Symbol: payload}
	require.NoError(t, s.SaveTokenMetadata(ctx, metadata))
	tokens, err := s.SearchTokens(ctx, payload, 1)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	tokens, err = s.SearchTokens(ctx, `' OR 1=1 --`, 1)
	require.NoError(t, err)
	assert.Empty(t, tokens)

	require.NoError(t, s.DeleteRecords(ctx, payload))
	rows, _, err = s.ListRecords(ctx, "keep", port.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	abi, err := s.GetABI(ctx, addr)
	require.NoError(t, err)
	assert.Equal(t, []byte("[]"), abi, "injected commands never ran")
}

func TestClearChainDataQuotesCatalogIdentifiers(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	addr := common.Address{1}
	require.NoError(t, s.SetABI(ctx, addr, []byte("[]")))
	// Catalog identifiers may contain quotes and SQL punctuation, too.
	for _, table := range []string{"x", `x"; TRUNCATE abis; --`} {
		ident := pgx.Identifier{s.schema, table}.Sanitize()
		_, err := s.pool.Exec(ctx, "CREATE TABLE "+ident+" (value text)")
		require.NoError(t, err)
		_, err = s.pool.Exec(ctx, "INSERT INTO "+ident+" (value) VALUES ($1)", "keep as data")
		require.NoError(t, err)
	}
	_, err := s.ClearChainData(ctx, nil, false)
	require.NoError(t, err)
	abi, err := s.GetABI(ctx, addr)
	require.NoError(t, err)
	assert.Equal(t, []byte("[]"), abi)
	var n int
	require.NoError(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{s.schema, `x"; TRUNCATE abis; --`}.Sanitize()).Scan(&n))
	assert.Zero(t, n)
}
