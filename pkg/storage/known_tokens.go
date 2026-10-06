package storage

import (
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// KnownToken is token metadata a chain defines instead of the token
// contract reporting it, such as a native coin exposed as a token contract.
type KnownToken struct {
	Name     string
	Symbol   string
	Decimals int
}

var (
	knownTokensMu sync.RWMutex
	knownTokens   = map[common.Address]KnownToken{}
)

// RegisterKnownToken records the metadata of a token contract. Chain
// packages register theirs when they are linked in; registering an address
// twice panics: it is a wiring bug.
func RegisterKnownToken(addr common.Address, t KnownToken) {
	knownTokensMu.Lock()
	defer knownTokensMu.Unlock()
	if _, dup := knownTokens[addr]; dup {
		panic("storage: known token " + addr.Hex() + " registered twice")
	}
	knownTokens[addr] = t
}

// knownToken returns the registered metadata of a token contract.
func knownToken(addr common.Address) (KnownToken, bool) {
	knownTokensMu.RLock()
	defer knownTokensMu.RUnlock()
	t, ok := knownTokens[addr]
	return t, ok
}

// KnownTokenAddresses returns the addresses of the registered token
// contracts, sorted. They exist from genesis without a creation
// transaction, so ingest indexes their metadata at block 0.
func KnownTokenAddresses() []common.Address {
	knownTokensMu.RLock()
	defer knownTokensMu.RUnlock()
	out := make([]common.Address, 0, len(knownTokens))
	for a := range knownTokens {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cmp(out[j]) < 0 })
	return out
}
