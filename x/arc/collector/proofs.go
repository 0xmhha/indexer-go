package collector

import (
	"crypto/sha256"
	"github.com/0xmhha/indexer-go/execwire"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
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
