package collector

import (
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/configfile"
	"strings"
)

// LoadConfig reads one explicit file without opening a native executable/store.
func LoadConfig(path string) (Config, error) {
	b, err := configfile.Read(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err = configfile.JSON(b, &c); err != nil {
		return c, err
	}
	return c, ValidateExplicitConfig(c)
}

// ValidateExplicitConfig applies the CLI's required values to file inputs.
func ValidateExplicitConfig(c Config) error {
	if err := ValidateConfig(c); err != nil {
		return err
	}
	if c.Native.Executable == "" || c.Native.Timeout <= 0 || c.Poll <= 0 || c.Retry <= 0 || c.Window < 1 || c.MaxRecords < 1 || c.MaxBytes == 0 || c.Anchor.Generation == 0 {
		return execwire.ErrLimit
	}
	return nil
}

// UnmarshalJSON preserves the CLI's explicit anchor-height requirement,
// including a deliberately supplied zero. No missing height becomes genesis.
func (c *Config) UnmarshalJSON(raw []byte) error {
	type plain Config
	var decoded plain
	if err := configfile.JSON(raw, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return configfile.ErrInput
	}
	var anchor map[string]json.RawMessage
	for key, value := range fields {
		if strings.EqualFold(key, "Anchor") {
			if json.Unmarshal(value, &anchor) != nil {
				return configfile.ErrInput
			}
		}
	}
	explicit := false
	for key := range anchor {
		explicit = explicit || strings.EqualFold(key, "Height")
	}
	if !explicit {
		return configfile.ErrInput
	}
	*c = Config(decoded)
	return nil
}
