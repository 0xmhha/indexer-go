package model

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
type ExtKey struct{ name string }

// NewExtKey returns a new key. The name is for diagnostics only; keys are
// compared by identity, so two packages cannot collide by name.
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
