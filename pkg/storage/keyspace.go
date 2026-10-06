package storage

import (
	"sort"
	"strings"
	"sync"
)

// The keyspace registry records which key prefixes exist and who owns them,
// so that operations over the whole database (reindex) and tests (every
// stored key belongs to a known prefix) do not depend on a hand-written
// list. Packages that store data register their prefixes in init;
// chain-specific packages register theirs when they are linked in.

// KeyClass says what a reindex does with a prefix.
type KeyClass int

const (
	// ChainData is derived from the chain and deleted by a reindex.
	ChainData KeyClass = iota
	// Preserved is entered by users (contract verification) and survives a
	// reindex.
	Preserved
)

// Keyspace is one owner's prefixes.
type Keyspace struct {
	Owner    string
	Class    KeyClass
	Prefixes []string
}

var (
	keyspaceMu sync.RWMutex
	keyspaces  = map[string]Keyspace{}
)

// RegisterKeyspace records the prefixes an owner (a package or feature)
// stores keys under. Registering an owner twice panics: it is a wiring bug.
func RegisterKeyspace(owner string, class KeyClass, prefixes ...string) {
	keyspaceMu.Lock()
	defer keyspaceMu.Unlock()
	if _, dup := keyspaces[owner]; dup {
		panic("storage: keyspace " + owner + " registered twice")
	}
	keyspaces[owner] = Keyspace{Owner: owner, Class: class, Prefixes: append([]string(nil), prefixes...)}
}

// Keyspaces returns the registered keyspaces, sorted by owner.
func Keyspaces() []Keyspace {
	keyspaceMu.RLock()
	defer keyspaceMu.RUnlock()
	out := make([]Keyspace, 0, len(keyspaces))
	for _, k := range keyspaces {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Owner < out[j].Owner })
	return out
}

// PrefixesOf returns the registered prefixes of a class, sorted.
func PrefixesOf(class KeyClass) []string {
	var out []string
	for _, k := range Keyspaces() {
		if k.Class == class {
			out = append(out, k.Prefixes...)
		}
	}
	sort.Strings(out)
	return out
}

// KeyOwner returns the owner of the registered prefix key starts with, or
// "" if no prefix covers it.
func KeyOwner(key string) string {
	best, owner := 0, ""
	for _, k := range Keyspaces() {
		for _, p := range k.Prefixes {
			if strings.HasPrefix(key, p) && len(p) > best {
				best, owner = len(p), k.Owner
			}
		}
	}
	return owner
}

func init() {
	RegisterKeyspace("core", ChainData,
		"/data/blocks/", "/data/txs/", "/data/receipts/", "/data/contractaddr/", "/data/logs/",
		"/index/txh/", "/index/blockh/", "/index/time/", "/index/logs/",
		"/meta/lh", "/meta/tc", "/meta/bc", "/meta/schema",
	)
	RegisterKeyspace("reorg", ChainData, prefixUndo, prefixOrphanReorg, prefixOrphanBlock, prefixOrphanHeight, prefixOrphanTx, keyOrphanSeq)
	RegisterKeyspace("features", ChainData, "/meta/features/")
	RegisterKeyspace("address", ChainData, "/index/addr/", "/meta/addrseq/")
	RegisterKeyspace("balance", ChainData, "/index/balance/")
	RegisterKeyspace("contracts", ChainData, "/data/contract/", "/index/contract/", "/data/internal/", "/index/internal/")
	RegisterKeyspace("tokens", ChainData, "/data/erc20/", "/data/erc721/", "/index/erc20/", "/index/erc721/", "/data/token/", "/index/token/")
	RegisterKeyspace("aa", ChainData,
		"/data/setcode/", "/index/setcode/", "/data/aa/", "/index/aa/",
		"/data/userop/", "/index/userop/", "/data/bundler/", "/data/paymaster/", "/data/factory/",
		"/data/smartaccount/", "/data/module/", "/index/module/",
	)
	RegisterKeyspace("multichain", ChainData, "/chain/")
	RegisterKeyspace("verification", Preserved, "/data/abi/", "/data/verification/", "/index/verification/")
}
