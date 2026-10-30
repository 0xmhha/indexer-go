//go:build darwin || linux

package nativeinput

import (
	"crypto/sha256"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedEpochRetainedPrefixUsesOriginalInputAndIndependentCoordinatorGeneration(t *testing.T) {
	dir := t.TempDir()
	p := proofFixture()
	boot := []byte("immutable bootstrap")
	p.Identity.BootstrapDigest = execwire.Hash(sha256.Sum256(boot))
	p.Receipt.Identity = p.Identity
	previous := p.Head
	previous.RequestID = 2
	previous.ReadEpoch = 3
	previous.Tip = Ref{Height: 2, Hash: execwire.Hash{11}, Root: execwire.Hash{12}}
	p.Head = Head{Version: 1, Instance: p.Identity.Instance, ImportEpoch: 2, ReadEpoch: 4, Tip: p.Ref}
	tr := Transition{Version: 1, Identity: p.Identity, Previous: previous, Marker: port.ExecutionRewind{RequestGeneration: 7, From: execwire.Ref{Generation: 7, Height: 2, Hash: previous.Tip.Hash}, Target: execwire.Ref{Generation: 8, Height: 1, Hash: p.Ref.Hash}, FromRoot: previous.Tip.Root, Root: p.Ref.Root}, Head: p.Head}
	epochs := filepath.Join(dir, ".arc-native-import-epochs")
	require.NoError(t, os.Mkdir(epochs, 0700))
	sealedFixture(t, filepath.Join(epochs, "2.json"), tr)
	base, err := ReceiptContext(dir, p.Identity, p.Head, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), base.ImportEpoch)
	require.Equal(t, p.Identity.Anchor, base.Tip)
	require.NoError(t, ValidateProof(p, p.Identity), "new current epoch can retain a prefix receipt from its original epoch")
	require.NoError(t, os.WriteFile(filepath.Join(dir, BootstrapName), boot, 0600))
	sealedFixture(t, filepath.Join(dir, IdentityName), p.Identity)
	sealedFixture(t, filepath.Join(dir, HeadName), p.Head)
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".arc-native-import-receipts"), 0700))
	sealedFixture(t, filepath.Join(dir, ".arc-native-import-receipts/1-1.json"), p.Receipt)
	require.NoError(t, VerifyCurrent(dir, p, p.Identity))
	for _, mutate := range []func(*Transition){func(t *Transition) { t.Head.ReadEpoch++ }, func(t *Transition) { t.Previous.ImportEpoch = 2 }, func(t *Transition) { t.Marker.Target.Generation = 7 }, func(t *Transition) { t.Head.Tip.Hash[0]++ }, func(t *Transition) { t.Identity.Instance[0]++ }} {
		bad := tr
		mutate(&bad)
		sealedFixture(t, filepath.Join(epochs, "2.json"), bad)
		require.Error(t, VerifyCurrent(dir, p, p.Identity), "current native root cannot replace missing or inconsistent lineage")
	}
	require.NoError(t, os.Remove(filepath.Join(epochs, "2.json")))
	require.Error(t, VerifyCurrent(dir, p, p.Identity))
}
