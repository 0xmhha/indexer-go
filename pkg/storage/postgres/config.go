package postgres

import (
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var schemaName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// poolConfig keeps SQL values on the server's parameter-binding path. pgx
// normally caches prepared statements; transaction poolers can use exec.
// A requested simple protocol is upgraded to exec to avoid client-side
// interpolation. Trusted, argument-free embedded migrations still use pgx's
// multi-statement path; they never contain application data.
func poolConfig(opts Options) (*pgxpool.Config, string, error) {
	schema := opts.Schema
	if schema == "" {
		schema = "public"
	}
	if !schemaName.MatchString(schema) {
		return nil, "", fmt.Errorf("postgres: invalid schema name %q", schema)
	}
	cfg, err := pgxpool.ParseConfig(opts.DSN)
	if err != nil {
		return nil, "", fmt.Errorf("postgres: parse DSN: %w", err)
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	if cfg.ConnConfig.DefaultQueryExecMode == pgx.QueryExecModeSimpleProtocol {
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	}
	// search_path is an identifier setting, not a value placeholder.
	cfg.ConnConfig.RuntimeParams["search_path"] = pgx.Identifier{schema}.Sanitize()
	cfg.ConnConfig.Tracer = queryObserver{}
	return cfg, schema, nil
}
