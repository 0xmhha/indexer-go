package chains

import (
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// FeeDelegation is the part of a transaction that lets an account other than
// the sender pay its gas, for example StableNet fee delegation (type 0x16).
type FeeDelegation struct {
	Payer   common.Address
	V, R, S *big.Int // the payer's signature
}

// FeeDelegationScheme reads fee delegation from the transaction types it
// covers. Chain profiles register their schemes when they are linked in, so
// chain-neutral code (ingest, APIs) can ask who pays gas without importing a
// chain package.
type FeeDelegationScheme struct {
	Name  string
	Types []uint8
	Of    func(tx *model.Transaction) (*FeeDelegation, bool)
}

var (
	fdMu      sync.RWMutex
	fdSchemes []FeeDelegationScheme
	fdTypes   = map[uint8]string{}
)

// RegisterFeeDelegation adds a scheme. It panics if the name or one of the
// types is already registered.
func RegisterFeeDelegation(s FeeDelegationScheme) {
	fdMu.Lock()
	defer fdMu.Unlock()
	for _, other := range fdSchemes {
		if other.Name == s.Name {
			panic(fmt.Sprintf("chains: fee delegation scheme %q registered twice", s.Name))
		}
	}
	for _, t := range s.Types {
		if owner, ok := fdTypes[t]; ok {
			panic(fmt.Sprintf("chains: transaction type %#x already has fee delegation scheme %q", t, owner))
		}
	}
	for _, t := range s.Types {
		fdTypes[t] = s.Name
	}
	fdSchemes = append(fdSchemes, s)
}

// FeeDelegationOf returns the fee delegation of tx, if a registered scheme
// finds one.
func FeeDelegationOf(tx *model.Transaction) (*FeeDelegation, bool) {
	fdMu.RLock()
	defer fdMu.RUnlock()
	for _, s := range fdSchemes {
		if fd, ok := s.Of(tx); ok {
			return fd, true
		}
	}
	return nil, false
}

// IsFeeDelegationType reports whether a registered scheme covers the
// transaction type.
func IsFeeDelegationType(t uint8) bool {
	fdMu.RLock()
	defer fdMu.RUnlock()
	_, ok := fdTypes[t]
	return ok
}

// GasPayer returns the account that pays gas for tx: the fee payer of a fee
// delegation transaction, otherwise the sender.
func GasPayer(tx *model.Transaction) common.Address {
	if fd, ok := FeeDelegationOf(tx); ok {
		return fd.Payer
	}
	return tx.From
}
