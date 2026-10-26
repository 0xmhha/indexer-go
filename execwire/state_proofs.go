package execwire

const StateProofFormat = "evm.state-proof.v1"
const MaxStateProofBytes = 8 << 20

// StateProofBundle contains selected block-end evidence, never a full witness.
// Nil means not collected. The consumer binds the root, selection and profile.
type StateProofBundle struct {
	Version  uint32
	Format   string
	Profile  string
	Provider string
	Response []byte
}

func ValidateStateProofBundle(b *StateProofBundle) error {
	if b == nil {
		return nil
	}
	if b.Version != 1 || b.Format != StateProofFormat || !blobLabel(b.Profile) || !blobLabel(b.Provider) || len(b.Response) == 0 {
		return ErrProtocol
	}
	if len(b.Response) > MaxStateProofBytes {
		return ErrLimit
	}
	return nil
}

// StateProofTarget is also the stdlib-only native exporter selection format.
type StateProofTarget struct {
	Address Address `json:"address" yaml:"address"`
	Code    bool    `json:"code" yaml:"code"`
	Slots   []Word  `json:"slots" yaml:"slots"`
}
