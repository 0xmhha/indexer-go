package arcingress

import (
	"context"
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

type preparedStatusFunc func(context.Context) (port.ExecutionControl, error)

func (f preparedStatusFunc) ExecutionStatus(ctx context.Context) (port.ExecutionControl, error) {
	return f(ctx)
}

func nativeArtifactFixture(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	out, err := json.Marshal(sealedArtifact{Data: data, Digest: execwire.Hash(sha256.Sum256(data))})
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0600)
}
func writeNativeArtifact(t *testing.T, path string, v any) {
	t.Helper()
	require.NoError(t, nativeArtifactFixture(path, v))
}

func TestNativePreparedCheckRequiresCleanExplicitNewBindingAndStopBeforeRun(t *testing.T) {
	p := rawProgram(t)
	p.closeRaw()
	p.opts.Path = filepath.Join(p.opts.Volume.Root, "new-raw")
	p.cfg.CreateWindow = true
	parent := t.TempDir()
	p.cfg.Native.DataDir = filepath.Join(parent, "native")
	p.cfg.Native.Executable = filepath.Join(t.TempDir(), "fixture-executable")
	p.cfg.Collector.Executable = filepath.Join(t.TempDir(), "fixture-replay")
	p.cfg.Collector.SpoolDir = filepath.Join(t.TempDir(), "spool")
	require.NoError(t, os.Chmod(filepath.Dir(p.cfg.Collector.SpoolDir), 0700))
	require.NoError(t, os.WriteFile(p.cfg.Collector.Executable, []byte("never executed"), 0700))
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
	p.head = nativeHead{Version: 1, Instance: p.identity.Instance, ImportEpoch: 1, ReadEpoch: 2, RequestID: 1, Tip: nativeRef{Height: 1, Hash: m.Hash, Root: r.Root}}
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
func TestNativePreparedCheckRejectsPublishedPrefixConflictBelowAheadHead(t *testing.T) {
	for _, wrong := range []string{"none", "hash", "root"} {
		t.Run(wrong, func(t *testing.T) {
			p, s := recoveryFixture(t)
			s.control = p.deps.Control
			one := p.head.Tip
			batch := rawFixture(t, p.control.Chain, one, 2)
			require.NoError(t, p.persist(t.Context(), batch))
			decoded, err := verifyBatch(p.control.Chain, one, batch)
			require.NoError(t, err)
			m := p.meta(p.head, batch[0].Block)
			_, digest, err := encodeInput(m, decoded.encoded, decoded.receipts)
			require.NoError(t, err)
			receipt := importReceipt{Version: 1, Identity: p.identity, RequestID: 2, ImportEpoch: 1, ReadEpoch: 3, Height: 2, Hash: m.Hash, Parent: m.Parent, ParentRoot: m.ParentRoot, Root: execwire.Hash(decoded.block.StateRoot), ReceiptsRoot: execwire.Hash(decoded.block.ReceiptRoot), InputDigest: digest}
			writeNativeArtifact(t, completionPath(p.cfg.Native.DataDir, 1, 2), receipt)
			p.head = nativeHead{Version: 1, Instance: p.identity.Instance, ImportEpoch: 1, ReadEpoch: 3, RequestID: 2, Tip: nativeRef{Height: 2, Hash: m.Hash, Root: receipt.Root}}
			writeNativeArtifact(t, filepath.Join(p.cfg.Native.DataDir, headName), p.head)
			p.deps.Control.Visible.Height = 1
			p.deps.Control.Visible.Hash = one.Hash
			p.deps.Control.Root = one.Root
			if wrong == "hash" {
				p.deps.Control.Visible.Hash = execwire.Hash{99}
			}
			if wrong == "root" {
				p.deps.Control.Root = execwire.Hash{99}
			}
			s.control = p.deps.Control
			err = p.Check(t.Context())
			if wrong == "none" {
				require.NoError(t, err)
			} else {
				require.Error(t, err, "native ahead must not hide an incompatible published SQL prefix before API exposure")
			}
		})
	}
}

func TestNativePreparedCheckRejectsLiveFenceBeforeAndAfterInspection(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[after], func(t *testing.T) {
			p, _ := recoveryFixture(t)
			calls := 0
			p.deps.Store = preparedStatusFunc(func(context.Context) (port.ExecutionControl, error) {
				calls++
				c := p.deps.Control
				if !after || calls > 1 {
					c.ReorgPending = true
				}
				return c, nil
			})
			require.ErrorIs(t, p.Check(t.Context()), port.ErrExecutionRewinding)
			if after {
				require.GreaterOrEqual(t, calls, 2, "a successful inspection must recheck the live SQL fence before API exposure")
			}
		})
	}
}

func TestNativePreparedCheckBindsCurrentPublishedPrefixAndRejectsMidInspectionAdvance(t *testing.T) {
	for _, kind := range []string{"current", "wrong-hash", "wrong-root", "advanced"} {
		t.Run(kind, func(t *testing.T) {
			p, s := recoveryFixture(t)
			current := p.deps.Control
			current.Visible.Height = p.head.Tip.Height
			current.Visible.Hash = p.head.Tip.Hash
			current.Root = p.head.Tip.Root
			s.control = current
			switch kind {
			case "wrong-hash":
				s.control.Visible.Hash = execwire.Hash{99}
			case "wrong-root":
				s.control.Root = execwire.Hash{99}
			case "advanced":
				calls := 0
				p.deps.Store = preparedStatusFunc(func(context.Context) (port.ExecutionControl, error) {
					calls++
					if calls == 1 {
						return p.deps.Control, nil
					}
					return current, nil
				})
			}
			err := p.Check(t.Context())
			switch kind {
			case "current":
				require.NoError(t, err)
			case "advanced":
				require.ErrorIs(t, err, port.ErrExecutionStale)
			default:
				require.Error(t, err, "current SQL publication must be checked rather than the cached preparation tip")
			}
		})
	}
}
