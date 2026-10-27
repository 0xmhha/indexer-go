package nativepreflight

import (
	"reflect"

	"github.com/0xmhha/indexer-go/execwire"
)

// unknownIdentity is a partial observation, not a set of guessed operator values.
func unknownIdentity() Unknown {
	return Unknown{"chain_id": true, "genesis": true, "height": true, "hash": true, "generation": true, "root": true}
}

// checkKnownIdentities unifies the known components before applying the real
// wire constraints. This catches cross-file relations (not just equal fields),
// e.g. genesis mode with a known nonzero height, or a known height-zero hash
// conflicting with a genesis declared in another otherwise unresolved file.
// Original HOLD findings remain; inferred consistency never verifies an input.
func checkKnownIdentities(r *Report, observations []Native) Native {
	var neutral execwire.Hash
	_ = neutral.UnmarshalText([]byte(NeutralHash))
	merged := Native{Chain: execwire.Chain{ID: "1", Genesis: neutral}, Anchor: execwire.Ref{Generation: 1, Height: 1, Hash: neutral}, Root: neutral, Unknown: unknownIdentity()}
	merge := func(key string, dst any, src any, unknown bool) {
		if unknown {
			return
		}
		value := reflect.ValueOf(dst).Elem()
		if merged.Unknown[key] {
			value.Set(reflect.ValueOf(src))
			merged.Unknown[key] = false
			return
		}
		if !reflect.DeepEqual(value.Interface(), src) {
			r.Add("identity."+key, Fail, "conflicting_known_identity")
		}
	}
	for _, o := range observations {
		merge("chain_id", &merged.Chain.ID, o.Chain.ID, o.Unknown["chain_id"])
		merge("genesis", &merged.Chain.Genesis, o.Chain.Genesis, o.Unknown["genesis"])
		merge("height", &merged.Anchor.Height, o.Anchor.Height, o.Unknown["height"])
		merge("hash", &merged.Anchor.Hash, o.Anchor.Hash, o.Unknown["hash"])
		merge("generation", &merged.Anchor.Generation, o.Anchor.Generation, o.Unknown["generation"])
		merge("root", &merged.Root, o.Root, o.Unknown["root"])
	}
	if !validIdentity(merged.Chain, merged.Anchor, merged.Unknown) {
		r.Add("identity", Fail, "conflicting_identity_constraints")
	}
	return merged
}
