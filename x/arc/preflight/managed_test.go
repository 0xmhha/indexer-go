package preflight

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/nativepreflight"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/0xmhha/indexer-go/x/arc/nativeops"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func managedFixture(t *testing.T) (nativepreflight.Inputs, *config.Config) {
	t.Helper()
	in := fixture(t)
	c := config.NewConfig()
	b, err := os.ReadFile(in.App)
	require.NoError(t, err)
	require.NoError(t, config.DecodeStrict(b, c))
	var n collector.Config
	b, err = os.ReadFile(in.Native)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &n))
	n.SpoolDir, n.Native.ScratchDir = "/synthetic/source/spool", "/synthetic/source"
	b, err = json.Marshal(n)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(in.Native, b, 0600))
	var r nativeops.Config
	b, err = os.ReadFile(in.Retention)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &r))
	r.SpoolDir = n.SpoolDir
	b, err = json.Marshal(r)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(in.Retention, b, 0600))
	var d nativepreflight.Declaration
	b, err = os.ReadFile(in.Declaration)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &d))
	d.FreeBytes, d.BackupBytes = 64<<30, 16<<30
	b, err = json.Marshal(d)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(in.Declaration, b, 0600))
	c.Source.Execution.Ingress = &ingress.Config{Module: "arc.native", Window: "arc-raw", Native: ingress.NativeConfig{Executable: "/synthetic/bin/importer", DataDir: n.Native.DataDir}, Collector: ingress.CollectorConfig{Executable: n.Native.Executable, SpoolDir: n.SpoolDir, MaxBytes: n.MaxBytes, MaxRecords: n.MaxRecords}, Peer: chainfeed.PeerSettings{NetworkID: 5042002, ForkHash: "01020304", KeyFile: "/synthetic/keys/arc.key", Peers: []string{d.Enode}}}
	c.WindowStorage = window.Layout{Volumes: map[string]window.VolumeSpec{"raw": {Root: "/synthetic/raw-volume", ID: "synthetic-raw", MaxBytes: 64 << 20}}, Windows: map[string]window.WindowConfig{"arc-raw": {Volume: "raw", Path: "/synthetic/raw-volume/arc-raw", MaxBytes: 4 << 20, SegmentBytes: 64 << 10, MaxPayloadBytes: 4096, ReadBudgetBytes: 2 << 20, MaxReaders: 2}}}
	writeManagedApp(t, in.App, c)
	return in, c
}

func writeManagedApp(t *testing.T, path string, c *config.Config) {
	t.Helper()
	b, err := yaml.Marshal(c)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0600))
}

func TestManagedAppPreflightAcceptsPreparedLayoutAsHold(t *testing.T) {
	in, _ := managedFixture(t)
	r := nativepreflight.Check(context.Background(), in, Product())
	require.Equal(t, nativepreflight.Hold, r.Status, "managed source-parent scratch must be validated as a declaration, never started: %+v", r.Findings)
	require.Equal(t, "not_authorized", r.OperationalReadiness)
	require.Equal(t, "not_checked", r.LiveFreshness)
}

func TestManagedAppPreflightUnknownDoesNotHideKnownConflict(t *testing.T) {
	in, c := managedFixture(t)
	c.Source.Execution.Ingress.Peer.ForkHash = "HOLD"
	writeManagedApp(t, in.App, c)
	r := nativepreflight.Check(context.Background(), in, Product())
	require.Equal(t, nativepreflight.Hold, r.Status, "%+v", r.Findings)
	c.Database.Postgres.MaxConns = 7
	writeManagedApp(t, in.App, c)
	require.Equal(t, nativepreflight.Fail, nativepreflight.Check(context.Background(), in, Product()).Status)
}

func TestManagedAppPreflightRejectsPolicyPathAndRawCapacityConflicts(t *testing.T) {
	for _, name := range []string{"network", "source_quota", "native", "scratch_scope", "raw_overlap", "raw_budget", "initialize", "numeric_limit", "raw_backup"} {
		t.Run(name, func(t *testing.T) {
			in, c := managedFixture(t)
			i := c.Source.Execution.Ingress
			switch name {
			case "network":
				i.Peer.NetworkID = 1
			case "source_quota":
				i.Collector.MaxBytes++
			case "native":
				i.Native.DataDir = "/foreign/native"
			case "scratch_scope":
				i.Collector.SpoolDir = "/synthetic/spool"
			case "raw_overlap":
				w := c.WindowStorage.Windows[i.Window]
				w.Path = "/synthetic/native/raw"
				c.WindowStorage.Windows[i.Window] = w
			case "raw_budget":
				w := c.WindowStorage.Windows[i.Window]
				w.MaxBytes = 128 << 20
				c.WindowStorage.Windows[i.Window] = w
			case "initialize":
				i.InitializeVolume = true
			case "numeric_limit":
				i.MaxActiveBlocks = 1
			case "raw_backup":
				var d nativepreflight.Declaration
				b, err := os.ReadFile(in.Declaration)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(b, &d))
				d.BackupBytes = d.NativeBytes + i.Collector.MaxBytes + d.IndexerBytes
				b, err = json.Marshal(d)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(in.Declaration, b, 0600))
			}
			writeManagedApp(t, in.App, c)
			require.Equal(t, nativepreflight.Fail, nativepreflight.Check(t.Context(), in, Product()).Status)
		})
	}
}

func TestManagedAppPreflightSelectedUnknownInputsRemainHoldAndFilesUnchanged(t *testing.T) {
	in, c := managedFixture(t)
	i := c.Source.Execution.Ingress
	i.Peer.ForkHash = "HOLD"
	i.Peer.KeyFile = "HOLD"
	i.Peer.Peers = []string{"HOLD"}
	writeManagedApp(t, in.App, c)
	b, err := os.ReadFile(in.App)
	require.NoError(t, err)
	// The numeric identity placeholder is accepted only in the detached copy.
	b = bytes.Replace(b, []byte("network_id: 5042002"), []byte("network_id: HOLD"), 1)
	require.NoError(t, os.WriteFile(in.App, b, 0600))
	before, err := os.ReadFile(in.App)
	require.NoError(t, err)
	r := nativepreflight.Check(t.Context(), in, Product())
	require.Equal(t, nativepreflight.Hold, r.Status, "%+v", r.Findings)
	after, err := os.ReadFile(in.App)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
