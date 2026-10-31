package diagnostics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/configfile"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/internal/nativemonitor"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/ethereum/go-ethereum/common"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/internal/nativediagnostics"
	"github.com/stretchr/testify/require"
)

func TestManagedDiagnosticFlagsRemainReadOnlyHold(t *testing.T) {
	r := nativediagnostics.Check(context.Background(), []string{"--raw", "HOLD", "--ingress-artifact", "HOLD"}, Product())
	require.Equal(t, nativediagnostics.Hold, r.Status, "explicit unobserved managed inputs must not be a CLI error or readiness")
	require.Equal(t, "not_authorized", r.Authority)
	require.Equal(t, "not_performed", r.LiveFreshness)
	found := false
	for _, f := range r.Findings {
		found = found || f.Field == "managed.raw"
	}
	require.True(t, found, "the managed scope must stay explicitly unobserved")
}

func put(t *testing.T, path string, v any, sealed bool) {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	if sealed {
		b, err = json.Marshal(struct {
			Data   json.RawMessage
			Digest execwire.Hash
		}{b, execwire.Hash(sha256.Sum256(b))})
		require.NoError(t, err)
	}
	require.NoError(t, os.WriteFile(path, b, 0600))
}
func managedInputs(t *testing.T) ([]string, string, nativeinput.Identity, ingress.Inspection) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(root, 0700))
	native, raw, source := filepath.Join(root, "native"), filepath.Join(root, "raw"), filepath.Join(root, "source", "spool")
	for _, p := range []string{native, raw, filepath.Dir(source)} {
		require.NoError(t, os.MkdirAll(p, 0700))
	}
	var genesis execwire.Hash
	require.NoError(t, genesis.UnmarshalText([]byte("0xe20e653af4441e8c6088e172b129d56420139824400477287b46e7101ae2bb1f")))
	chain := execwire.Chain{ID: "5042002", Genesis: genesis}
	anchor := execwire.Ref{Generation: 1, Height: 100, Hash: execwire.Hash{100}}
	boot := []byte("owned synthetic metadata; SDK state not inspected")
	require.NoError(t, os.WriteFile(filepath.Join(native, nativeinput.BootstrapName), boot, 0600))
	identity := nativeinput.Identity{Version: 1, Instance: execwire.Hash{9}, ChainID: 5042002, Genesis: genesis, ArcRevision: nativeinput.ArcRevision, RethRevision: nativeinput.RethRevision, Layout: "reth-native-v2", BootstrapDigest: execwire.Hash(sha256.Sum256(boot)), Anchor: nativeinput.Ref{Height: 100, Hash: anchor.Hash, Root: execwire.Hash{101}}}
	put(t, filepath.Join(native, nativeinput.IdentityName), identity, true)
	put(t, filepath.Join(native, nativeinput.HeadName), nativeinput.Head{Version: 1, Instance: identity.Instance, ImportEpoch: 1, ReadEpoch: 1, Tip: identity.Anchor}, true)
	type rawState struct {
		Version     uint8
		Instance    execwire.Hash
		Chain       model.FeedChain
		Anchor      nativeinput.Ref
		RawEpoch    uint64
		Received    nativeinput.Ref
		NativeEpoch uint64 `json:",omitempty"`
		Generation  uint64 `json:",omitempty"`
	}
	feedChain := model.FeedChain{ID: chain.ID, GenesisHash: common.Hash(chain.Genesis)}
	state, err := json.Marshal(rawState{2, identity.Instance, feedChain, identity.Anchor, 1, identity.Anchor, 1, 1})
	require.NoError(t, err)
	storeID := execwire.Hash{10}
	put(t, filepath.Join(raw, "STORE.json"), struct {
		Version int
		ID      common.Hash
		Chain   model.FeedChain
		Next    uint64
		Kind    string `json:",omitempty"`
	}{1, common.Hash(storeID), feedChain, 1, "owned-raw-v1"}, false)
	put(t, filepath.Join(raw, "OWNER.json"), window.Owner{Version: 1, StoreID: common.Hash(storeID), Domain: "arc.native/ain1/v1", State: state}, true)
	admission, err := collector.ManagedAdmissionBinding(collector.Options{Chain: chain, Managed: &identity})
	require.NoError(t, err)
	sp, err := spool.Open(t.Context(), spool.Options{Path: source, Chain: chain, Anchor: anchor, Admission: &admission, MaxBytes: 4 << 20, MaxRecords: 16})
	require.NoError(t, err)
	require.NoError(t, sp.Close())
	policy := port.ExecutionIngressPolicy{Version: 1, Module: "arc.native", Chain: chain, Anchor: anchor, AnchorRoot: identity.Anchor.Root, Requirements: execwire.Hash{77}}
	pd, err := policy.Digest()
	require.NoError(t, err)
	ib, err := json.Marshal(identity)
	require.NoError(t, err)
	binding := port.ExecutionIngressBinding{Version: 1, PolicyDigest: pd, NativeInstance: identity.Instance, BootstrapDigest: identity.BootstrapDigest, RawStoreID: storeID, SourcePolicyDigest: admission, NativeIdentity: ib}
	bd, err := binding.Digest()
	require.NoError(t, err)
	view := ingress.Inspection{Version: 1, Scope: ingress.InspectionScope, Control: port.ExecutionControl{ExecutionConfig: port.ExecutionConfig{Chain: chain, Anchor: anchor, AnchorRoot: identity.Anchor.Root, PartitionBlocks: 128}, Visible: anchor, Generation: 1, Root: identity.Anchor.Root, BaselineReady: true}, Owner: &port.ExecutionIngressOwner{Policy: policy, PolicyDigest: pd, Binding: &binding, BindingDigest: bd}}
	require.NoError(t, view.Validate())
	artifact := filepath.Join(root, "sql.json")
	put(t, artifact, view, false)
	return []string{"--native", native, "--raw", raw, "--spool", source, "--ingress-artifact", artifact}, root, identity, view
}
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			out[path] = string(b)
		}
		return nil
	}))
	return out
}
func TestManagedDiagnosticMatchesOwnedMetadataAndExportWithoutMutation(t *testing.T) {
	args, root, _, _ := managedInputs(t)
	before := tree(t, root)
	r := nativediagnostics.Check(t.Context(), args, Product())
	require.Equal(t, nativediagnostics.Hold, r.Status, "%+v", r.Findings)
	kinds := map[string]bool{}
	for _, o := range r.Observations {
		kinds[o.Kind] = true
	}
	for _, kind := range []string{"managed.native", "managed.raw", "managed.sql"} {
		require.True(t, kinds[kind], kind)
	}
	var output bytes.Buffer
	require.Equal(t, 2, nativediagnostics.Run(t.Context(), args, &output, Product()))
	require.NotContains(t, output.String(), root)
	var consumer nativemonitor.DiagnosticReport
	require.NoError(t, configfile.JSON(output.Bytes(), &consumer))
	require.Equal(t, before, tree(t, root), "no lock, recovery, child, socket or file mutation")
}
func TestManagedDiagnosticCorruptionAndOwnerConflictFailClosed(t *testing.T) {
	for _, kind := range []string{"owner", "store", "artifact", "journal", "source_policy"} {
		t.Run(kind, func(t *testing.T) {
			args, _, identity, view := managedInputs(t)
			switch kind {
			case "owner":
				var seal struct {
					Data   json.RawMessage
					Digest execwire.Hash
				}
				b, err := os.ReadFile(filepath.Join(args[3], "OWNER.json"))
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(b, &seal))
				var owner window.Owner
				require.NoError(t, json.Unmarshal(seal.Data, &owner))
				var state struct {
					Version     uint8
					Instance    execwire.Hash
					Chain       model.FeedChain
					Anchor      nativeinput.Ref
					RawEpoch    uint64
					Received    nativeinput.Ref
					NativeEpoch uint64 `json:",omitempty"`
					Generation  uint64 `json:",omitempty"`
				}
				require.NoError(t, json.Unmarshal(owner.State, &state))
				state.Instance = execwire.Hash{99}
				owner.State, err = json.Marshal(state)
				require.NoError(t, err)
				put(t, filepath.Join(args[3], "OWNER.json"), owner, true)
			case "store":
				require.NoError(t, os.WriteFile(filepath.Join(args[3], "STORE.json"), []byte("corrupt-private-input"), 0600))
			case "artifact":
				view.Scope = "wrong"
				put(t, args[7], view, false)
			case "journal":
				require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(args[1]), "."+filepath.Base(args[1])+".arc-execution-maintenance"), []byte("corrupt-private-input"), 0600))
			case "source_policy":
				admission := execwire.Hash{98}
				source := filepath.Join(filepath.Dir(args[5]), "foreign")
				sp, err := spool.Open(t.Context(), spool.Options{Path: source, Chain: view.Control.Chain, Anchor: view.Control.Anchor, Admission: &admission, MaxBytes: 4 << 20, MaxRecords: 16})
				require.NoError(t, err)
				require.NoError(t, sp.Close())
				args[5] = source
			}
			_ = identity
			r := nativediagnostics.Check(t.Context(), args, Product())
			require.Equal(t, nativediagnostics.Fail, r.Status, "%+v", r.Findings)
		})
	}
}
func TestManagedDiagnosticReportsIndependentReceivedNativePublicProgress(t *testing.T) {
	args, _, i, view := managedInputs(t)
	head := nativeinput.Head{Version: 1, Instance: i.Instance, ImportEpoch: 1, ReadEpoch: 2, RequestID: 1, Tip: nativeinput.Ref{Height: 101, Hash: execwire.Hash{101}, Root: execwire.Hash{102}}}
	put(t, filepath.Join(args[1], nativeinput.HeadName), head, true)
	require.NoError(t, os.Mkdir(filepath.Join(args[1], ".arc-native-import-receipts"), 0700))
	put(t, filepath.Join(args[1], ".arc-native-import-receipts", "1-101.json"), nativeinput.Receipt{Version: 1, Identity: i, RequestID: 1, ImportEpoch: 1, ReadEpoch: 2, Height: 101, Hash: head.Tip.Hash, Parent: i.Anchor.Hash, ParentRoot: i.Anchor.Root, Root: head.Tip.Root, ReceiptsRoot: execwire.Hash{103}, InputDigest: execwire.Hash{104}}, true)
	var seal struct {
		Data   json.RawMessage
		Digest execwire.Hash
	}
	b, err := os.ReadFile(filepath.Join(args[3], "OWNER.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &seal))
	var owner window.Owner
	require.NoError(t, json.Unmarshal(seal.Data, &owner))
	// Preserve the production field order while replacing just the received value.
	var state struct {
		Version     uint8
		Instance    execwire.Hash
		Chain       model.FeedChain
		Anchor      nativeinput.Ref
		RawEpoch    uint64
		Received    nativeinput.Ref
		NativeEpoch uint64 `json:",omitempty"`
		Generation  uint64 `json:",omitempty"`
	}
	require.NoError(t, json.Unmarshal(owner.State, &state))
	state.Received = nativeinput.Ref{Height: 102, Hash: execwire.Hash{105}, Root: execwire.Hash{106}}
	owner.State, err = json.Marshal(state)
	require.NoError(t, err)
	put(t, filepath.Join(args[3], "OWNER.json"), owner, true)
	r := nativediagnostics.Check(t.Context(), args, Product())
	require.Equal(t, nativediagnostics.Hold, r.Status, "%+v", r.Findings)
	for _, o := range r.Observations {
		switch o.Kind {
		case "managed.raw":
			require.Equal(t, uint64(102), o.Received.Height)
		case "managed.native":
			require.Equal(t, uint64(101), o.NativeTip.Height)
			require.Nil(t, o.Generation)
		case "managed.sql":
			require.NotNil(t, o.Visible)
			require.Equal(t, view.Control.Visible, *o.Visible)
		}
	}
}

func TestManagedSourceCorruptionIsFailNotUninspected(t *testing.T) {
	args, _, _, _ := managedInputs(t)
	require.NoError(t, os.WriteFile(filepath.Join(args[5], ".exec-spool-state"), []byte("corrupt"), 0600))
	result := inspectManaged(t.Context(), nativediagnostics.ManagedInputs{Spool: args[5]})
	found := false
	for _, f := range result.Findings {
		if f.Field == "managed.source" {
			found = f.Status == nativediagnostics.Fail
		}
	}
	require.True(t, found, "%+v", result.Findings)
}

func TestManagedInspectionMissingUnsafeCancelledAndIncompleteInputs(t *testing.T) {
	for _, kind := range []string{"missing_raw", "symlink_raw", "omitted_false", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			args, root, _, _ := managedInputs(t)
			ctx := t.Context()
			expected := nativediagnostics.Fail
			switch kind {
			case "missing_raw":
				args[3] = filepath.Join(root, "missing")
				expected = nativediagnostics.Hold
			case "symlink_raw":
				alias := filepath.Join(root, "alias")
				require.NoError(t, os.Symlink(args[3], alias))
				args[3] = alias
			case "omitted_false":
				b, err := os.ReadFile(args[7])
				require.NoError(t, err)
				b = bytes.Replace(b, []byte(`,"RawPending":false`), nil, 1)
				require.NoError(t, os.WriteFile(args[7], b, 0600))
			case "cancel":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			before := tree(t, root)
			r := nativediagnostics.Check(ctx, args, Product())
			require.Equal(t, expected, r.Status, "%+v", r.Findings)
			require.Equal(t, before, tree(t, root))
		})
	}
}

func TestManagedReportBoundsRejectInvalidEpochsAndInputReferences(t *testing.T) {
	for _, kind := range []string{"epoch", "height", "hash", "root"} {
		t.Run(kind, func(t *testing.T) {
			args, _, _, view := managedInputs(t)
			p := Product()
			p.Managed = func(context.Context, nativediagnostics.ManagedInputs) nativediagnostics.ManagedResult {
				epoch := uint64(1)
				r := nativediagnostics.InputRef{Height: 100, Hash: execwire.Hash{1}, Root: execwire.Hash{2}}
				switch kind {
				case "epoch":
					epoch = math.MaxUint64
				case "height":
					r.Height = math.MaxUint64
				case "hash":
					r.Hash = execwire.Hash{}
				case "root":
					r.Root = execwire.Hash{}
				}
				return nativediagnostics.ManagedResult{Observations: []nativediagnostics.Observation{{Kind: "managed.raw", Basis: managedBasis, Chain: view.Control.Chain, Anchor: view.Control.Anchor, Received: &r, RawEpoch: &epoch}}}
			}
			require.Equal(t, nativediagnostics.Fail, nativediagnostics.Check(t.Context(), args, p).Status)
		})
	}
}
