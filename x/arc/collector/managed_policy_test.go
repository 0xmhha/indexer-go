//go:build darwin || linux

package collector

import (
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/spool"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestManagedSpoolPolicyPinsImmutableInstanceBootstrapAndProofScope(t *testing.T) {
	o, _ := managedFixture(t, "managed")
	_, binding, err := admissionPolicy(o)
	require.NoError(t, err)
	require.NotNil(t, binding, "managed source must never adopt a legacy unbound spool")
	original := *binding
	for _, mutate := range []func(*Options){func(o *Options) { o.Managed.Instance[0]++ }, func(o *Options) { o.Managed.BootstrapDigest[0]++ }, func(o *Options) {
		o.StateProofs = &stateproof.Selection{Targets: []stateproof.Target{{Address: common.Address{1}, Code: true}}}
	}} {
		changed := o
		i := *o.Managed
		changed.Managed = &i
		mutate(&changed)
		_, other, err := admissionPolicy(changed)
		require.NoError(t, err)
		require.NotNil(t, other)
		require.NotEqual(t, original, *other)
	}
	// Epochs deliberately live outside immutable identity. Authorized rewind
	// reuses this source policy rather than inventing a new spool authority.
	_, again, err := admissionPolicy(o)
	require.NoError(t, err)
	require.Equal(t, original, *again)
	opts := spool.Options{Path: filepath.Join(t.TempDir(), "source"), Chain: o.Chain, Anchor: execwire.Ref{Generation: 1, Hash: o.Chain.Genesis}, MaxBytes: 1 << 20, MaxRecords: 4}
	old, err := spool.Open(t.Context(), opts)
	require.NoError(t, err)
	require.NoError(t, old.Close())
	managed := opts
	managed.Admission = binding
	refused, err := spool.Open(t.Context(), managed)
	if refused != nil {
		require.NoError(t, refused.Close())
	}
	require.Error(t, err)
	old, err = spool.Open(t.Context(), opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, old.Close()) }()
	status, err := old.Status(t.Context())
	require.NoError(t, err)
	require.Equal(t, opts.Anchor, status.Committed)
}
