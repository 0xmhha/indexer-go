package evmtrace

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"time"
)

type Config struct {
	Name         string        `yaml:"name"`
	Provider     string        `yaml:"provider"`
	StartHeight  uint64        `yaml:"start_height"`
	PollInterval time.Duration `yaml:"poll_interval"`
}

func (c Config) Descriptor(anchor model.FeedBlock, profile string) (model.SupplementStreamDescriptor, error) {
	var d model.SupplementStreamDescriptor
	if anchor.Validate() != nil || c.StartHeight <= anchor.Number || c.PollInterval < 0 || c.PollInterval > 5*time.Second || c.PollInterval > 0 && c.PollInterval < 20*time.Millisecond {
		return d, ErrTrace
	}
	def := Definition{Provider: c.Provider, Format: "geth.callTracer.v1", Profile: profile}
	if e := def.Validate(); e != nil {
		return d, e
	}
	raw, e := json.Marshal(def)
	if e != nil {
		return d, e
	}
	d = model.SupplementStreamDescriptor{Version: 1, Name: c.Name, Codec: Codec, Selection: raw, SelectionHash: sha256.Sum256(append([]byte(Codec+"\x00"), raw...)), FromHeight: c.StartHeight}
	return d, d.Validate()
}
func DefinitionFor(d model.SupplementStreamDescriptor) (Definition, error) {
	v, e := model.CallTraceDefinitionFor(d)
	if e != nil {
		return v, ErrTrace
	}
	return v, nil
}
