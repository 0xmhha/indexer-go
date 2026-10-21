package flags

import (
	"fmt"

	"github.com/spf13/pflag"

	"github.com/0xmhha/indexer-go/internal/config"
)

// Flag is one flag for a configuration field.
type Flag struct {
	Name string
	// Key is the field's YAML path and Env the environment variable that
	// sets the same field; help shows both.
	Key   string
	Env   string
	Usage string
	// Field returns a pointer to the field in cfg: *string, *int, *uint64
	// or *bool.
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
		case *int:
			*dst = *f.Field(g.values).(*int)
		case *uint64:
			*dst = *f.Field(g.values).(*uint64)
		case *bool:
			*dst = *f.Field(g.values).(*bool)
		}
	}
}

// RPC is the chain node group.
func RPC() *ConfigGroup {
	return NewConfigGroup("Chain node",
		Flag{Name: "rpc", Key: "rpc.endpoint", Env: "INDEXER_RPC_ENDPOINT", Usage: "JSON-RPC endpoint URL of the node",
			Field: func(c *config.Config) any { return &c.RPC.Endpoint }},
	)
}

// Database is the storage group.
func Database() *ConfigGroup {
	return NewConfigGroup("Database",
		Flag{Name: "db", Key: "database.path", Env: "INDEXER_DB_PATH", Usage: "Pebble database directory",
			Field: func(c *config.Config) any { return &c.Database.Path }},
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
