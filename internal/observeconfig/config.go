// Package observeconfig validates optional observation policy without IO.
package observeconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/internal/nativealerts"
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
)

var ErrConfig = errors.New("observation: invalid or unsupported configuration")

type Resource struct {
	ID          string `yaml:"id"`
	Series      string `yaml:"series"`
	BudgetBytes uint64 `yaml:"budget_bytes"`
}
type Config struct {
	Enabled       bool          `yaml:"enabled"`
	CheckpointDir string        `yaml:"checkpoint_dir,omitempty"`
	Initialize    bool          `yaml:"initialize,omitempty"`
	Sink          string        `yaml:"sink,omitempty"`
	OperatorID    string        `yaml:"operator_id,omitempty"`
	Database      Resource      `yaml:"database,omitempty"`
	WAL           Resource      `yaml:"wal,omitempty"`
	Interval      time.Duration `yaml:"interval,omitempty"`
	MaxAge        time.Duration `yaml:"max_age,omitempty"`
	// Named YAML fields keep strict decoding and stable operator spelling.
	Warn            uint64        `yaml:"warn_bps,omitempty"`
	Stop            uint64        `yaml:"stop_bps,omitempty"`
	Clear           uint64        `yaml:"clear_bps,omitempty"`
	WarnCount       int           `yaml:"warn_count,omitempty"`
	ClearCount      int           `yaml:"clear_count,omitempty"`
	WarnDwell       time.Duration `yaml:"warn_dwell,omitempty"`
	ClearDwell      time.Duration `yaml:"clear_dwell,omitempty"`
	Cooldown        time.Duration `yaml:"cooldown,omitempty"`
	InboxSize       int           `yaml:"inbox_size,omitempty"`
	DeliveryTimeout time.Duration `yaml:"delivery_timeout,omitempty"`
}

func (c Config) WithDefaults() Config {
	if !c.Enabled {
		return c
	}
	if c.Sink == "" {
		c.Sink = "disabled"
	}
	if c.Interval == 0 {
		c.Interval = time.Minute
	}
	base := c.Interval
	if base < time.Second || base > time.Hour {
		base = time.Minute
	}
	if c.MaxAge == 0 {
		c.MaxAge = 3 * base
	}
	if c.WarnCount == 0 {
		c.WarnCount = 3
	}
	if c.ClearCount == 0 {
		c.ClearCount = 3
	}
	if c.WarnDwell == 0 {
		c.WarnDwell = 2 * base
	}
	if c.ClearDwell == 0 {
		c.ClearDwell = 2 * base
	}
	if c.Cooldown == 0 {
		c.Cooldown = 15 * time.Minute
	}
	if c.InboxSize == 0 {
		c.InboxSize = 256
	}
	if c.DeliveryTimeout == 0 {
		c.DeliveryTimeout = 2 * time.Second
	}
	return c
}
func decodeID(s string) (nativeobserve.Digest, bool) {
	var id nativeobserve.Digest
	if len(s) != 64 || strings.ToLower(s) != s {
		return id, false
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, false
	}
	copy(id[:], b)
	return id, id != (nativeobserve.Digest{})
}
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if !filepath.IsAbs(c.CheckpointDir) || filepath.Clean(c.CheckpointDir) != c.CheckpointDir || strings.IndexByte(c.CheckpointDir, 0) >= 0 ||
		len(c.CheckpointDir) > 4096 || c.Sink != "disabled" || c.Interval < time.Second || c.Interval > time.Hour ||
		c.MaxAge < c.Interval || c.MaxAge > 24*time.Hour || c.InboxSize < 1 || c.InboxSize > nativeobserve.MaxKeys ||
		c.DeliveryTimeout <= 0 || c.DeliveryTimeout > 5*time.Second {
		return ErrConfig
	}
	if c.OperatorID != "" {
		if _, ok := decodeID(c.OperatorID); !ok {
			return ErrConfig
		}
	}
	for _, r := range []Resource{c.Database, c.WAL} {
		if _, ok := decodeID(r.ID); !ok {
			return ErrConfig
		}
		if _, ok := decodeID(r.Series); !ok || r.BudgetBytes == 0 {
			return ErrConfig
		}
	}
	if c.Database.ID == c.WAL.ID {
		return ErrConfig
	}
	for _, r := range c.rules() {
		if r.Validate() != nil {
			return ErrConfig
		}
	}
	return nil
}
func (c Config) policy() nativeobserve.Digest {
	data, _ := json.Marshal(struct {
		Warn, Stop, Clear                       uint64
		WarnCount, ClearCount                   int
		WarnDwell, ClearDwell, Cooldown, MaxAge time.Duration
	}{c.Warn, c.Stop, c.Clear, c.WarnCount, c.ClearCount, c.WarnDwell, c.ClearDwell, c.Cooldown, c.MaxAge})
	return nativeobserve.Digest(sha256.Sum256(data))
}
func (c Config) rules() []nativealerts.Rule {
	var rules []nativealerts.Rule
	for n, v := range []Resource{c.Database, c.WAL} {
		id, _ := decodeID(v.ID)
		metric := nativeobserve.DatabaseBytes
		if n == 1 {
			metric = nativeobserve.WALBytes
		}
		rules = append(rules, nativealerts.Rule{Key: nativealerts.Key{Resource: nativeobserve.ResourceID(id), Metric: metric, Kind: nativealerts.Capacity}, Policy: c.policy(), Mode: nativealerts.BudgetBPS, Warn: c.Warn, Stop: c.Stop, Clear: c.Clear, WarnCount: c.WarnCount, ClearCount: c.ClearCount, WarnDwell: c.WarnDwell, ClearDwell: c.ClearDwell, Cooldown: c.Cooldown, MaxAge: c.MaxAge})
	}
	return rules
}
func (c Config) bindings(source nativeobserve.Source) []nativeobserve.Binding {
	var result []nativeobserve.Binding
	for n, v := range []Resource{c.Database, c.WAL} {
		id, _ := decodeID(v.ID)
		series, _ := decodeID(v.Series)
		kind, def, metric := nativeobserve.Database, nativeobserve.DatabaseLogical, nativeobserve.DatabaseBytes
		if n == 1 {
			kind, def, metric = nativeobserve.Cluster, nativeobserve.ClusterWALLogical, nativeobserve.WALBytes
		}
		scope := nativeobserve.Scope{Kind: kind, ID: nativeobserve.ResourceID(id)}
		result = append(result, nativeobserve.Binding{Scope: scope, Metric: metric, Unit: nativeobserve.Bytes, Definition: def, Source: source, Series: series, Policy: c.policy(), Budget: nativeobserve.Budget{Scope: scope, Definition: def, ID: c.policy(), Limit: nativeobserve.Number{Present: true, Value: v.BudgetBytes}}})
	}
	return result
}

func (c Config) Rules() []nativealerts.Rule { return c.rules() }
func (c Config) Bindings(source nativeobserve.Source) []nativeobserve.Binding {
	return c.bindings(source)
}
func (c Config) OperatorDigest() nativeobserve.Digest {
	value, _ := decodeID(c.OperatorID)
	return value
}
