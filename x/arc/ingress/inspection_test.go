package arcingress

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"github.com/stretchr/testify/require"
)

func inspectionFixture(t *testing.T) (*maintenanceCoordinator, maintenanceRecord) {
	t.Helper()
	c, point, store := maintenanceFixture(t, true)
	scope, binary, err := c.normalize(t.Context())
	require.NoError(t, err)
	owner, err := store.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	var i nativeinput.Identity
	require.NoError(t, json.Unmarshal(owner.Binding.NativeIdentity, &i))
	proof, err := nativeinput.ReadHeadProof(c.cfg.Native.DataDir, i)
	require.NoError(t, err)
	s := maintenanceRecord{Version: 1, Phase: "prepared", Scope: scope, Options: c.options, BinaryDigest: binary, PolicyDigest: owner.PolicyDigest, OwnerDigest: owner.BindingDigest, Point: point, Proof: proof}
	require.NoError(t, spool.WithRetentionEvidence(t.Context(), c.source(owner), execwire.Ref{Height: point.Ref.Height, Hash: point.Ref.Hash}, point.Root, c.options.KeepBlocks, func(r retention.Request, _ execwire.Hash) error { s.Request = r; return nil }))
	j, err := openMaintenanceJournal(c.cfg.Native.DataDir)
	require.NoError(t, err)
	require.NoError(t, j.save(t.Context(), s))
	require.NoError(t, j.close())
	return c, s
}

func TestManagedInspectionReadsWhileOwnersHoldLocksWithoutRecovery(t *testing.T) {
	c, s := inspectionFixture(t)
	lease, err := nativestore.AcquireRun(c.cfg.Native.DataDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, lease.Close()) }()
	raw, err := window.OpenOwnedChecked(c.raw, ownerDomain, func(window.DeletionRecord) error { t.Fatal("inspection recovered deletion"); return window.ErrUnsafeGC })
	require.NoError(t, err)
	defer func() { require.NoError(t, raw.Close()) }()
	before := inspectionFiles(t, filepath.Dir(c.cfg.Native.DataDir))
	n, err := InspectNativeControl(t.Context(), c.cfg.Native.DataDir)
	require.NoError(t, err, "inspection must use fixed read-only metadata, not run locks")
	require.Equal(t, s.Proof.Identity, n.Identity)
	require.Equal(t, s.Proof.Head, n.Head)
	r, err := InspectRawControl(t.Context(), c.raw.Path)
	require.NoError(t, err)
	require.Equal(t, n.Identity.Instance, r.Control.Instance)
	m, err := InspectMaintenance(t.Context(), c.cfg.Native.DataDir)
	require.NoError(t, err)
	require.Equal(t, "prepared", m.Phase)
	require.Equal(t, s.Point, m.Point)
	require.Equal(t, before, inspectionFiles(t, filepath.Dir(c.cfg.Native.DataDir)))
}

func inspectionFiles(t *testing.T, root string) map[string]string {
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
