package model

import (
	"fmt"
	"sync"
)

// ExtKey identifies one kind of chain-specific extension. Profiles declare
// keys as package-level variables and expose typed accessors, so feature code
// never handles untyped values:
//
//	var feeDelegationKey = model.NewExtKey("stablenet.fee_delegation")
//
//	func FeeDelegationOf(tx *model.Transaction) (*FeeDelegation, bool) {
//		v, ok := tx.Ext.Get(feeDelegationKey).(*FeeDelegation)
//		return v, ok
//	}
type ExtKey struct {
	name  string
	codec *ExtCodec
}

// NewExtKey returns a new key. Keys are compared by identity, so two packages
// cannot collide by name; the name identifies the key in diagnostics and, once
// a codec is registered, in stored data.
func NewExtKey(name string) *ExtKey { return &ExtKey{name: name} }

// String returns the key's diagnostic name.
func (k *ExtKey) String() string { return k.name }

// Extensions holds chain-specific values attached to a model value.
type Extensions map[*ExtKey]any

// Get returns the value stored under k, or nil.
func (e Extensions) Get(k *ExtKey) any {
	if e == nil {
		return nil
	}
	return e[k]
}

// Set stores v under k, allocating the map on first use.
func (e *Extensions) Set(k *ExtKey, v any) {
	if *e == nil {
		*e = Extensions{}
	}
	(*e)[k] = v
}

// ExtCodec converts one extension value to and from bytes for storage.
type ExtCodec struct {
	Encode func(v any) ([]byte, error)
	Decode func(data []byte) (any, error)
}

var (
	extKeysMu sync.RWMutex
	extKeys   = map[string]*ExtKey{}
)

// RegisterExtCodec makes values under k storable. The key's name is written
// with the value, so it must be unique and must not change once data is
// stored. It panics if another key already uses the name.
func RegisterExtCodec(k *ExtKey, c ExtCodec) {
	extKeysMu.Lock()
	defer extKeysMu.Unlock()
	if other, ok := extKeys[k.name]; ok && other != k {
		panic(fmt.Sprintf("model: extension name %q registered twice", k.name))
	}
	k.codec = &c
	extKeys[k.name] = k
}

func lookupExtKey(name string) (*ExtKey, bool) {
	extKeysMu.RLock()
	defer extKeysMu.RUnlock()
	k, ok := extKeys[name]
	return k, ok
}
