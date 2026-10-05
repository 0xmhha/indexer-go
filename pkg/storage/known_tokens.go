package storage

import (
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
