package nativepreflight

import (
	"encoding/json"
	"strings"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/configfile"
)

// Unknown tracks selected operator identity, boundary and capacity fields.
// Runtime limits, arbitrary syntax, nulls and unknown keys stay invalid.
type Unknown map[string]bool
type Placeholder struct {
	Path, Key string
	Neutral   any
}

const NeutralHash = "0x0101010101010101010101010101010101010101010101010101010101010101"

// DecodeDeclaredJSON retains the production schema. A detached validation copy
// substitutes neutral values only for absent identity fields or exact HOLD.
// Unknown flags prevent those substitutions from becoming verified matches.
func DecodeDeclaredJSON(b []byte, target any, scope string, fields []Placeholder, r *Report) (Unknown, error) {
	var obj map[string]any
	if err := configfile.JSON(b, &obj); err != nil {
		return nil, err
	}
	u := Unknown{}
	for _, f := range fields {
		path := strings.Split(f.Path, ".")
		m := obj
		for _, part := range path[:len(path)-1] {
			for existing := range m {
				if strings.EqualFold(existing, part) {
					part = existing
					break
				}
			}
			next, ok := m[part].(map[string]any)
			if !ok {
				next = map[string]any{}
				if _, present := m[part]; present {
					return nil, configfile.ErrInput
				}
				m[part] = next
			}
			m = next
		}
		key := path[len(path)-1]
		for existing := range m {
			if strings.EqualFold(existing, key) {
				key = existing
				break
			}
		}
		v, present := m[key]
		text, isText := v.(string)
		if !present || isText && (text == "HOLD" || text == "") {
			u[f.Key] = true
			m[key] = f.Neutral
			r.Add(scope+"."+f.Path, Hold, "operator_input_missing")
		}
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, configfile.ErrInput
	}
	return u, configfile.JSON(data, target)
}
func (r *Report) MatchKnown(field string, a, b any, unknown bool) {
	if unknown {
		r.Add(field, Hold, "unresolved_comparison")
		return
	}
	r.Match(field, a, b)
}
func (r *Report) ChainMatch(field string, a, b execwire.Chain, ua, ub Unknown) {
	r.MatchKnown(field+".id", a.ID, b.ID, ua["chain_id"] || ub["chain_id"])
	r.MatchKnown(field+".genesis", a.Genesis, b.Genesis, ua["genesis"] || ub["genesis"])
}
func (r *Report) AnchorMatch(field string, a, b execwire.Ref, ua, ub Unknown) {
	r.MatchKnown(field+".height", a.Height, b.Height, ua["height"] || ub["height"])
	r.MatchKnown(field+".hash", a.Hash, b.Hash, ua["hash"] || ub["hash"])
	r.MatchKnown(field+".generation", a.Generation, b.Generation, ua["generation"] || ub["generation"])
}
func IdentityFields(chain, anchor, root string) []Placeholder {
	return []Placeholder{{chain + ".ID", "chain_id", "1"}, {chain + ".Genesis", "genesis", NeutralHash}, {anchor + ".Height", "height", 0}, {anchor + ".Hash", "hash", NeutralHash}, {anchor + ".Generation", "generation", 1}, {root, "root", NeutralHash}}
}

func declarationFields() []Placeholder {
	fields := []Placeholder{{"network_id", "network", 0}, {"start_height", "start", 0}, {"retained_floor", "floor", 0}}
	for _, key := range []string{"native_bytes", "indexer_bytes", "checkpoint_bytes", "scratch_bytes", "backup_bytes", "free_bytes"} {
		fields = append(fields, Placeholder{key, key, 0})
	}
	return fields
}
