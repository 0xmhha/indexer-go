package arcingress

import (
	"crypto/sha256"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func writeNativeArtifact(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	out, err := json.Marshal(sealedArtifact{Data: data, Digest: execwire.Hash(sha256.Sum256(data))})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, out, 0600))
}

func TestNativePreparedCheckRequiresCleanExplicitNewBindingAndStopBeforeRun(t *testing.T) {
	p := rawProgram(t)
	p.closeRaw()
	p.opts.Path = filepath.Join(p.opts.Volume.Root, "new-raw")
	p.cfg.CreateWindow = true
	parent := t.TempDir()
	p.cfg.Native.DataDir = filepath.Join(parent, "native")
	p.cfg.Native.Executable = filepath.Join(t.TempDir(), "fixture-executable")
	p.deps.Socket = filepath.Join(t.TempDir(), "execution.sock")
	require.NoError(t, os.Chmod(parent, 0700))
	require.NoError(t, os.Chmod(filepath.Dir(p.deps.Socket), 0700))
	require.NoError(t, os.WriteFile(p.cfg.Native.Executable, []byte("this fixture is never executed"), 0700))
	require.NoError(t, os.Mkdir(p.cfg.Native.DataDir, 0700))
	for _, name := range []string{"db", "rocksdb", "static_files"} {
		require.NoError(t, os.Mkdir(filepath.Join(p.cfg.Native.DataDir, name), 0700))
	}
	c := p.deps.Control
	boot := bootstrapReceipt{Version: 1, ArcRevision: ArcRevision, RethRevision: RethRevision, Anchor: checkpoint.Manifest{Version: 1, Mode: "snapshot", Coverage: "full", Chain: c.Chain, Ref: c.Anchor, Root: c.AnchorRoot, Header: []byte{0xc0}}, Floor: 0, Next: 1, StateDigest: execwire.Hash{4}}
	encoded, err := json.Marshal(boot)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(p.cfg.Native.DataDir, ".arc-native-bootstrap.json"), encoded, 0600))
	require.NoError(t, p.Check(t.Context()))
	_, err = os.Lstat(p.opts.Path)
	require.ErrorIs(t, err, os.ErrNotExist, "Check creates no raw window or native instance")
	pending := filepath.Join(p.cfg.Native.DataDir, ".arc-native-import.pending")
	require.NoError(t, os.WriteFile(pending, []byte("partial"), 0600))
	require.Error(t, p.Check(t.Context()))
	require.NoError(t, os.Remove(pending))
	p.cfg.CreateWindow = false
	require.Error(t, p.Check(t.Context()))
	p.cfg.CreateWindow = true
	require.NoError(t, os.WriteFile(filepath.Join(p.cfg.Native.DataDir, headName), []byte("partial"), 0600))
	require.Error(t, p.Check(t.Context()))
	p.Stop()
	require.ErrorIs(t, p.Run(t.Context()), port.ErrClosed)
}
func TestNativeReconcileLostReplyRequiresRawDigestAndExactReadyHead(t *testing.T) {
	p := rawProgram(t)
	b := rawFixture(t, p.control.Chain, p.identity.Anchor, 1)
	require.NoError(t, p.persist(t.Context(), b))
	decoded, err := verifyBatch(p.control.Chain, p.identity.Anchor, b)
	require.NoError(t, err)
	old := p.head
	m := p.meta(old, b[0].Block)
	_, digest, err := encodeInput(m, decoded.encoded, decoded.receipts)
	require.NoError(t, err)
	r := importReceipt{Version: 1, Identity: p.identity, RequestID: 1, ImportEpoch: 1, ReadEpoch: 2, Height: 1, Hash: m.Hash, Parent: m.Parent, ParentRoot: m.ParentRoot, Root: execwire.Hash(decoded.block.StateRoot), ReceiptsRoot: execwire.Hash(decoded.block.ReceiptRoot), InputDigest: digest}
	p.cfg.Native.DataDir = t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(p.cfg.Native.DataDir, ".arc-native-import-receipts"), 0700))
	path := completionPath(p.cfg.Native.DataDir, 1, 1)
	writeNativeArtifact(t, path, r)
	p.head = nativeHead{Version: 1, Instance: p.identity.Instance, ImportEpoch: 1, ReadEpoch: 2, RequestID: 1, Tip: nativeRef{1, m.Hash, r.Root}}
	require.NoError(t, p.reconcile(t.Context()), "completed native input may resume only with retained exact raw evidence")
	p.head.Tip.Root = execwire.Hash{9}
	require.Error(t, p.reconcile(t.Context()), "ready root and immutable completed receipt must agree")
	p.head.Tip.Root = r.Root
	r.InputDigest = execwire.Hash{9}
	writeNativeArtifact(t, path, r)
	require.Error(t, p.reconcile(t.Context()))
}
func TestNativeReconcileVerifiesReceivedControlRootBeforePeerStart(t *testing.T) {
	p := rawProgram(t)
	b := rawFixture(t, p.control.Chain, p.identity.Anchor, 1)
	require.NoError(t, p.persist(t.Context(), b))
	previous, _ := json.Marshal(p.control)
	p.control.Received.Root = execwire.Hash{9}
	next, _ := json.Marshal(p.control)
	require.NoError(t, p.raw.CompareOwner(t.Context(), previous, next))
	require.Error(t, p.reconcile(t.Context()), "a checksummed owner is not proof of raw commitments")
}
