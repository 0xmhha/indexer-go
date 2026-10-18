package evmblob

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// Provider is an operator-owned label, never a URL, secret or RPC method.
type Definition struct {
	Provider string `json:"provider"`
	Format   string `json:"format"`
	Profile  string `json:"profile"`
}

func label(v string) bool {
	if len(v) == 0 || len(v) > 64 {
		return false
	}
	for _, c := range v {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}
func (d Definition) Validate() error {
	if !label(d.Provider) || !label(d.Profile) || d.Format != Format {
		return ErrBlob
	}
	return nil
}
func Descriptor(name string, from uint64, def Definition) (model.SupplementStreamDescriptor, error) {
	var d model.SupplementStreamDescriptor
	if def.Validate() != nil {
		return d, ErrBlob
	}
	raw, e := json.Marshal(def)
	if e != nil {
		return d, e
	}
	d = model.SupplementStreamDescriptor{Version: 1, Name: name, Codec: Codec, Selection: raw, SelectionHash: sha256.Sum256(append([]byte(Codec+"\x00"), raw...)), FromHeight: from}
	return d, d.Validate()
}
func DefinitionFor(d model.SupplementStreamDescriptor) (Definition, error) {
	var def Definition
	if d.Validate() != nil || d.Codec != Codec || d.Required || d.ParentCheckpoint {
		return def, ErrBlob
	}
	if e := json.Unmarshal(d.Selection, &def); e != nil || def.Validate() != nil {
		return Definition{}, ErrBlob
	}
	canonical, e := json.Marshal(def)
	if e != nil || !bytes.Equal(canonical, d.Selection) {
		return Definition{}, ErrBlob
	}
	return def, nil
}
