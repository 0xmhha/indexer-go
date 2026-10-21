package flags

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/fetch"
)

// Flag is one flag for a configuration field.
type Flag struct {
	Name string
	// Key is the field's YAML path and Env the environment variable that
	// sets the same field; help shows both.
	Key   string
	Env   string
	Usage string
	// Field returns a pointer to the field in cfg: *string, *[]string,
	// *int, *uint64, *float64, *bool or *time.Duration.
	Field func(cfg *config.Config) any
}

// ConfigGroup is a group of configuration flags. Flag values are parsed
// into a configuration of defaults and copied to the real one only when
// given.
type ConfigGroup struct {
	name   string
	flags  []Flag
	values *config.Config
}

// NewConfigGroup returns a group of flags; help shows their defaults.
func NewConfigGroup(name string, fl ...Flag) *ConfigGroup {
	return &ConfigGroup{name: name, flags: fl, values: config.NewConfig()}
}

// Name implements Group.
func (g *ConfigGroup) Name() string { return g.name }

// Flags lists the group's flags.
func (g *ConfigGroup) Flags() []Flag { return g.flags }

// AddFlags implements Group.
func (g *ConfigGroup) AddFlags(fs *pflag.FlagSet) {
	for _, f := range g.flags {
		usage := fmt.Sprintf("%s (%s, $%s)", f.Usage, f.Key, f.Env)
		switch p := f.Field(g.values).(type) {
		case *string:
			fs.StringVar(p, f.Name, *p, usage)
		case *[]string:
			fs.StringSliceVar(p, f.Name, *p, usage)
		case *float64:
			fs.Float64Var(p, f.Name, *p, usage)
		case *time.Duration:
			fs.DurationVar(p, f.Name, *p, usage)
		case *int:
			fs.IntVar(p, f.Name, *p, usage)
		case *uint64:
			fs.Uint64Var(p, f.Name, *p, usage)
		case *bool:
			fs.BoolVar(p, f.Name, *p, usage)
		default:
			panic(fmt.Sprintf("flags: field of --%s has unsupported type %T", f.Name, p))
		}
	}
}

// ApplyTo implements Applier.
func (g *ConfigGroup) ApplyTo(cfg *config.Config, changed func(name string) bool) {
	for _, f := range g.flags {
		if !changed(f.Name) {
			continue
		}
		switch dst := f.Field(cfg).(type) {
		case *string:
			*dst = *f.Field(g.values).(*string)
		case *[]string:
			*dst = slices.Clone(*f.Field(g.values).(*[]string))
		case *float64:
			*dst = *f.Field(g.values).(*float64)
		case *time.Duration:
			*dst = *f.Field(g.values).(*time.Duration)
		case *int:
			*dst = *f.Field(g.values).(*int)
		case *uint64:
			*dst = *f.Field(g.values).(*uint64)
		case *bool:
			*dst = *f.Field(g.values).(*bool)
		}
	}
}

// choices writes allowed values for a usage string.
func choices(values ...string) string { return strings.Join(values, ", ") }

// RPC is the chain node group.
func RPC() *ConfigGroup {
	return NewConfigGroup("Chain node",
		Flag{Name: "rpc", Key: "rpc.endpoint", Env: "INDEXER_RPC_ENDPOINT", Usage: "JSON-RPC endpoint URL of the node",
			Field: func(c *config.Config) any { return &c.RPC.Endpoint }},
		Flag{Name: "rpc-fallback", Key: "rpc.fallback_endpoints", Env: "INDEXER_RPC_FALLBACK_ENDPOINTS", Usage: "Further HTTP(S) endpoints of the same chain, comma-separated, tried in order",
			Field: func(c *config.Config) any { return &c.RPC.FallbackEndpoints }},
		Flag{Name: "rpc-ws", Key: "rpc.ws_endpoint", Env: "INDEXER_RPC_WS_ENDPOINT", Usage: "WebSocket endpoint whose newHeads wake the live loop",
			Field: func(c *config.Config) any { return &c.RPC.WSEndpoint }},
		Flag{Name: "rpc-rate-limit", Key: "rpc.rate_limit", Env: "INDEXER_RPC_RATE_LIMIT", Usage: "HTTP requests per second to the nodes, every attempt included; 0 is unlimited",
			Field: func(c *config.Config) any { return &c.RPC.RateLimit }},
		Flag{Name: "rpc-timeout", Key: "rpc.timeout", Env: "INDEXER_RPC_TIMEOUT", Usage: "Timeout of one RPC attempt",
			Field: func(c *config.Config) any { return &c.RPC.Timeout }},
	)
}

// Database is the storage group. The PostgreSQL DSN holds a password, so
// it has no flag: set it in the file or INDEXER_DB_POSTGRES_DSN.
func Database() *ConfigGroup {
	return NewConfigGroup("Database",
		Flag{Name: "db-driver", Key: "database.driver", Env: "INDEXER_DB_DRIVER", Usage: "Storage: " + choices(config.DriverPebble, config.DriverPostgres) + " (the PostgreSQL DSN has no flag)",
			Field: func(c *config.Config) any { return &c.Database.Driver }},
		Flag{Name: "db", Key: "database.path", Env: "INDEXER_DB_PATH", Usage: "Pebble database directory",
			Field: func(c *config.Config) any { return &c.Database.Path }},
	)
}

// Node is the process role group.
func Node() *ConfigGroup {
	return NewConfigGroup("Process role",
		Flag{Name: "node-role", Key: "node.role", Env: "INDEXER_NODE_ROLE", Usage: "What this process does: " + choices(config.RoleAll, config.RoleIngest, config.RoleAPI),
			Field: func(c *config.Config) any { return &c.Node.Role }},
	)
}

// Indexer is the ingestion group.
func Indexer() *ConfigGroup {
	return NewConfigGroup("Indexing",
		Flag{Name: "start-height", Key: "indexer.start_height", Env: "INDEXER_START_HEIGHT", Usage: "Block height to start indexing from",
			Field: func(c *config.Config) any { return &c.Indexer.StartHeight }},
		Flag{Name: "workers", Key: "indexer.workers", Env: "INDEXER_WORKERS", Usage: "Blocks fetched concurrently",
			Field: func(c *config.Config) any { return &c.Indexer.Workers }},
		Flag{Name: "chunk-size", Key: "indexer.chunk_size", Env: "INDEXER_CHUNK_SIZE", Usage: "Blocks per batch, positive; 1 is real-time mode",
			Field: func(c *config.Config) any { return &c.Indexer.ChunkSize }},
		Flag{Name: "mode", Key: "indexer.mode", Env: "INDEXER_MODE", Usage: "What is ingested: " + choices(config.ModeFull, config.ModeDeclared+" (only the logs features.records declares)"),
			Field: func(c *config.Config) any { return &c.Indexer.Mode }},
		Flag{Name: "finality", Key: "indexer.finality", Env: "INDEXER_FINALITY", Usage: "How far the live loop indexes: " + choices(fetch.FinalityHead, fetch.FinalityConfirmations, fetch.FinalityFinalized),
			Field: func(c *config.Config) any { return &c.Indexer.Finality }},
		Flag{Name: "confirmations", Key: "indexer.confirmations", Env: "INDEXER_CONFIRMATIONS", Usage: "Blocks behind the head with --finality confirmations",
			Field: func(c *config.Config) any { return &c.Indexer.Confirmations }},
		Flag{Name: "era-dir", Key: "source.era_dir", Env: "INDEXER_SOURCE_ERA_DIR", Usage: "Directory of era1 archives read before the node",
			Field: func(c *config.Config) any { return &c.Source.EraDir }},
	)
}

// API is the API server group.
func API() *ConfigGroup {
	return NewConfigGroup("API server",
		Flag{Name: "api", Key: "api.enabled", Env: "INDEXER_API_ENABLED", Usage: "Serve the API (--api=false turns it off)",
			Field: func(c *config.Config) any { return &c.API.Enabled }},
		Flag{Name: "api-host", Key: "api.host", Env: "INDEXER_API_HOST", Usage: "Address the API listens on",
			Field: func(c *config.Config) any { return &c.API.Host }},
		Flag{Name: "api-port", Key: "api.port", Env: "INDEXER_API_PORT", Usage: "Port the API listens on",
			Field: func(c *config.Config) any { return &c.API.Port }},
		Flag{Name: "graphql", Key: "api.enable_graphql", Env: "INDEXER_API_GRAPHQL", Usage: "Serve GraphQL",
			Field: func(c *config.Config) any { return &c.API.EnableGraphQL }},
		Flag{Name: "jsonrpc", Key: "api.enable_jsonrpc", Env: "INDEXER_API_JSONRPC", Usage: "Serve JSON-RPC",
			Field: func(c *config.Config) any { return &c.API.EnableJSONRPC }},
		Flag{Name: "websocket", Key: "api.enable_websocket", Env: "INDEXER_API_WEBSOCKET", Usage: "Serve WebSocket",
			Field: func(c *config.Config) any { return &c.API.EnableWebSocket }},
	)
}

// Log is the logging group.
func Log() *ConfigGroup {
	return NewConfigGroup("Logging",
		Flag{Name: "log-level", Key: "log.level", Env: "INDEXER_LOG_LEVEL", Usage: "Log level: debug, info, warn or error",
			Field: func(c *config.Config) any { return &c.Log.Level }},
		Flag{Name: "log-format", Key: "log.format", Env: "INDEXER_LOG_FORMAT", Usage: "Log format: console or json",
			Field: func(c *config.Config) any { return &c.Log.Format }},
	)
}
