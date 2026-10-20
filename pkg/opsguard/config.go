package opsguard

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Defaults for the sampling settings. Byte thresholds have no defaults: they
// belong to one deployment's disks and budgets.
const (
	DefaultFilesystemInterval    = 30 * time.Second
	DefaultFilesystemMissingStop = time.Minute
	DefaultPostgresInterval      = 5 * time.Minute
	DefaultPostgresMissingStop   = 10 * time.Minute
	DefaultPostgresStatsInterval = time.Minute
	DefaultStopForecast          = 2 * time.Hour
	DefaultResumeSamples         = 3
)

// Config is the operations section of the indexer configuration.
type Config struct {
	// HaltFile is the latch a halt writes and startup checks. It must be on
	// storage that outlives the process and does not depend on the monitored
	// volume being present.
	HaltFile string `yaml:"halt_file"`
	// JournalFile optionally keeps capacity samples and transitions as JSON lines.
	JournalFile     string          `yaml:"journal_file"`
	JournalMaxBytes int64           `yaml:"journal_max_bytes"`
	Capacity        *CapacityConfig `yaml:"capacity"`
}

// CapacityConfig selects what the Monitor samples and when it halts.
type CapacityConfig struct {
	// Filesystem holding raw windows and the database.
	FilesystemPath        string        `yaml:"filesystem_path"`
	FilesystemInterval    time.Duration `yaml:"filesystem_interval"`
	FilesystemMissingStop time.Duration `yaml:"filesystem_missing_stop"`
	WarnFreeBytes         int64         `yaml:"warn_free_bytes"`
	StopFreeBytes         int64         `yaml:"stop_free_bytes"`
	// Resume requires more free space than this in every resume sample.
	ResumeFreeBytes int64 `yaml:"resume_free_bytes"`
	// FloorFreeBytes is where the exhaustion forecast ends: free space the
	// raw windows never write into.
	FloorFreeBytes int64 `yaml:"floor_free_bytes"`

	// Optional PostgreSQL data directory, including WAL; a planned budget,
	// not a filesystem quota.
	PostgresDataDir     string        `yaml:"postgres_data_dir"`
	PostgresBudgetBytes int64         `yaml:"postgres_budget_bytes"`
	PostgresInterval    time.Duration `yaml:"postgres_interval"`
	PostgresMissingStop time.Duration `yaml:"postgres_missing_stop"`
	PostgresMaxEntries  int           `yaml:"postgres_max_entries"`
	// PostgresStatsInterval samples the database and WAL sizes of an
	// attached PostgreSQL store, for the journal only.
	PostgresStatsInterval time.Duration `yaml:"postgres_stats_interval"`

	// A forecast reaching the floor or budget within StopForecast halts.
	StopForecast   time.Duration `yaml:"stop_forecast"`
	ResumeSamples  int           `yaml:"resume_samples"`
	ResumeInterval time.Duration `yaml:"resume_interval"`
}

// Enabled reports whether the configuration starts a Monitor.
func (c Config) Enabled() bool { return c.Capacity != nil }

// WithDefaults fills unset sampling settings.
func (c Config) WithDefaults() Config {
	if c.JournalMaxBytes == 0 {
		c.JournalMaxBytes = DefaultJournalMaxBytes
	}
	if c.Capacity == nil {
		return c
	}
	v := *c.Capacity
	setDuration(&v.FilesystemInterval, DefaultFilesystemInterval)
	setDuration(&v.FilesystemMissingStop, DefaultFilesystemMissingStop)
	setDuration(&v.PostgresInterval, DefaultPostgresInterval)
	setDuration(&v.PostgresMissingStop, DefaultPostgresMissingStop)
	setDuration(&v.PostgresStatsInterval, DefaultPostgresStatsInterval)
	setDuration(&v.StopForecast, DefaultStopForecast)
	setDuration(&v.ResumeInterval, v.FilesystemInterval)
	if v.ResumeSamples == 0 {
		v.ResumeSamples = DefaultResumeSamples
	}
	if v.PostgresMaxEntries == 0 {
		v.PostgresMaxEntries = DefaultTreeEntries
	}
	c.Capacity = &v
	return c
}

func setDuration(d *time.Duration, def time.Duration) {
	if *d == 0 {
		*d = def
	}
}

// Validate checks a configuration after WithDefaults.
func (c Config) Validate() error {
	if c.HaltFile == "" {
		if c.Capacity != nil {
			return errors.New("operations.halt_file is required with operations.capacity")
		}
		if c.JournalFile != "" {
			return errors.New("operations.journal_file needs operations.capacity")
		}
		return nil
	}
	if !filepath.IsAbs(c.HaltFile) {
		return errors.New("operations.halt_file must be absolute")
	}
	if c.JournalFile != "" && !filepath.IsAbs(c.JournalFile) {
		return errors.New("operations.journal_file must be absolute")
	}
	if c.JournalMaxBytes < 0 {
		return errors.New("operations.journal_max_bytes must not be negative")
	}
	if c.Capacity == nil {
		return nil
	}
	return c.Capacity.validate()
}

func (v *CapacityConfig) validate() error {
	if !filepath.IsAbs(v.FilesystemPath) {
		return errors.New("operations.capacity.filesystem_path must be absolute")
	}
	if v.FloorFreeBytes < 0 || v.FloorFreeBytes >= v.StopFreeBytes || v.StopFreeBytes >= v.WarnFreeBytes || v.WarnFreeBytes > v.ResumeFreeBytes {
		return errors.New("operations.capacity needs floor_free_bytes < stop_free_bytes < warn_free_bytes <= resume_free_bytes")
	}
	for _, d := range []struct {
		name  string
		value time.Duration
	}{{"filesystem_interval", v.FilesystemInterval}, {"filesystem_missing_stop", v.FilesystemMissingStop}, {"postgres_interval", v.PostgresInterval}, {"postgres_missing_stop", v.PostgresMissingStop}, {"postgres_stats_interval", v.PostgresStatsInterval}, {"stop_forecast", v.StopForecast}, {"resume_interval", v.ResumeInterval}} {
		if d.value < time.Second {
			return fmt.Errorf("operations.capacity.%s must be at least 1s", d.name)
		}
	}
	if v.FilesystemMissingStop < v.FilesystemInterval {
		return errors.New("operations.capacity.filesystem_missing_stop must not be shorter than filesystem_interval")
	}
	if v.ResumeSamples < 1 || v.ResumeSamples > 100 {
		return errors.New("operations.capacity.resume_samples must be 1..100")
	}
	if v.PostgresDataDir == "" {
		if v.PostgresBudgetBytes != 0 {
			return errors.New("operations.capacity.postgres_budget_bytes needs postgres_data_dir")
		}
		return nil
	}
	if !filepath.IsAbs(v.PostgresDataDir) {
		return errors.New("operations.capacity.postgres_data_dir must be absolute")
	}
	if v.PostgresBudgetBytes <= 0 {
		return errors.New("operations.capacity.postgres_budget_bytes must be positive")
	}
	if v.PostgresMissingStop < v.PostgresInterval {
		return errors.New("operations.capacity.postgres_missing_stop must not be shorter than postgres_interval")
	}
	if v.PostgresMaxEntries < 1 {
		return errors.New("operations.capacity.postgres_max_entries must be positive")
	}
	return nil
}

// UnmarshalYAML rejects unknown keys, so a misspelled threshold is an error
// instead of a missing limit.
func (c *Config) UnmarshalYAML(n *yaml.Node) error {
	type plain Config
	data, err := yaml.Marshal(n)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var v plain
	if err := dec.Decode(&v); err != nil {
		return err
	}
	*c = Config(v)
	return nil
}
