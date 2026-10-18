package evmstate

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
)

type AccountConfig struct {
	Address string   `yaml:"address"`
	Code    bool     `yaml:"code"`
	Slots   []string `yaml:"slots"`
}
type Config struct {
	ProjectChanges   bool            `yaml:"project_changes"`
	ParentCheckpoint bool            `yaml:"parent_checkpoint"`
	Required         bool            `yaml:"required"`
	Name             string          `yaml:"name"`
	StartHeight      uint64          `yaml:"start_height"`
	PollInterval     time.Duration   `yaml:"poll_interval"`
	Accounts         []AccountConfig `yaml:"accounts"`
}

func (c Config) Descriptor(anchor model.FeedBlock) (model.SupplementStreamDescriptor, error) {
	var d model.SupplementStreamDescriptor
	if anchor.Validate() != nil || c.StartHeight <= anchor.Number || c.PollInterval < 0 || c.PollInterval > 5*time.Second || c.PollInterval > 0 && c.PollInterval < 20*time.Millisecond || len(c.Accounts) == 0 || len(c.Accounts) > MaxAccounts {
		return d, fmt.Errorf("state_proofs: explicit name/start_height/accounts and poll interval 20ms..5s required")
	}
	parse := func(s string, n int) ([]byte, error) {
		if len(s) != n*2+2 || s[:2] != "0x" {
			return nil, ErrProof
		}
		return hex.DecodeString(s[2:])
	}
	sel := Selection{}
	total := 0
	for _, a := range c.Accounts {
		total += len(a.Slots)
		if total > MaxSlots {
			return d, ErrProof
		}
		b, e := parse(a.Address, 20)
		if e != nil {
			return d, fmt.Errorf("state_proofs: exact 20-byte address required")
		}
		target := Target{Address: common.BytesToAddress(b), Code: a.Code}
		for _, slot := range a.Slots {
			b, e := parse(slot, 32)
			if e != nil {
				return d, fmt.Errorf("state_proofs: exact 32-byte slot required")
			}
			target.Slots = append(target.Slots, common.BytesToHash(b))
		}
		sel.Targets = append(sel.Targets, target)
	}
	normalized, digest, e := sel.Normalize()
	if e != nil {
		return d, e
	}
	raw, e := json.Marshal(normalized)
	if e != nil {
		return d, e
	}
	d = model.SupplementStreamDescriptor{Version: 1, Name: c.Name, Codec: Codec, SelectionHash: digest, Selection: raw, FromHeight: c.StartHeight, ParentCheckpoint: c.ParentCheckpoint || c.Required || c.ProjectChanges, Required: c.Required}
	return d, d.Validate()
}
func selectionFor(d model.SupplementStreamDescriptor) (Selection, error) {
	if d.Validate() != nil || d.Codec != Codec {
		return Selection{}, ErrProof
	}
	var sel Selection
	if e := json.Unmarshal(d.Selection, &sel); e != nil {
		return sel, e
	}
	normalized, digest, e := sel.Normalize()
	if e != nil || digest != d.SelectionHash {
		return Selection{}, ErrProof
	}
	return normalized, nil
}
