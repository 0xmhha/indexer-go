package collector

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
)

// The identity is versioned and source-specific. A scope change needs a new spool.
func proofPolicy(selection *stateproof.Selection) (*stateproof.Selection, *execwire.Hash, error) {
	if selection == nil {
		return nil, nil, nil
	}
	normalized, digest, e := selection.Normalize()
	if e != nil {
		return nil, nil, e
	}
	hash := execwire.Hash(sha256.Sum256(append([]byte("arc.native-state-proofs.v1\x00arc\x00arc-native\x00"), digest[:]...)))
	return &normalized, &hash, nil
}
func admissionPolicy(o Options) (*stateproof.Selection, *execwire.Hash, error) {
	selection, policy, err := proofPolicy(o.StateProofs)
	if err != nil || o.Managed == nil {
		return selection, policy, err
	}
	if nativeinput.ValidIdentity(*o.Managed) != nil || o.Chain.ID != "5042002" || o.Chain.Genesis != o.Managed.Genesis {
		return nil, nil, ErrNative
	}
	identity, err := json.Marshal(*o.Managed)
	if err != nil {
		return nil, nil, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("arc.managed-replay.v1\x00"))
	_, _ = h.Write(identity)
	_, _ = h.Write([]byte{0})
	if policy != nil {
		_, _ = h.Write([]byte{1})
		_, _ = h.Write(policy[:])
	} else {
		_, _ = h.Write([]byte{0})
	}
	var binding execwire.Hash
	copy(binding[:], h.Sum(nil))
	return selection, &binding, nil
}
