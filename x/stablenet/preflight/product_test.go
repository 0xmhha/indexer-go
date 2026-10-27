package preflight

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/execwire/nativecfg"
	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/nativepreflight"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/x/stablenet/consensus"
	"gopkg.in/yaml.v3"
)

func stableFixture(t *testing.T) (nativepreflight.Inputs, *config.Config, nativecfg.Config, nativepreflight.Declaration) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../../../testdata/execution/consensus/native.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Policy consensus.TrustPolicy }
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	v, err := consensus.NewIndependent(f.Policy)
	if err != nil {
		t.Fatal(err)
	}
	point, err := v.AnchorPoint()
	if err != nil {
		t.Fatal(err)
	}
	enode := "enode://79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8@127.0.0.1:30303"
	text := func(h execwire.Hash) string { b, _ := h.MarshalText(); return string(b) }
	c := nativecfg.Config{StartMode: "genesis", DataDir: "/synthetic/native", SpoolDir: "/synthetic/spool", Socket: "/synthetic/ipc/execution.sock", GenesisFile: "/synthetic/genesis.json", ChainID: f.Policy.Chain.ID, NetworkID: 8283, GenesisHash: text(f.Policy.Chain.Genesis), AnchorHeight: point.Height, AnchorHash: text(point.Hash), AnchorRoot: text(point.Root), Peers: []string{enode}, SpoolBytes: 4 << 30, SpoolRecords: 262144, RetentionReserveBytes: 1 << 30}
	d := nativepreflight.Declaration{StartMode: "initial", Profile: "stablenet-native", Chain: f.Policy.Chain, NetworkID: 8283, Anchor: execwire.Ref{Generation: 1, Height: point.Height, Hash: point.Hash}, Root: point.Root, StartHeight: point.Height + 1, RetainedFloor: point.Height, CheckpointPath: "/synthetic/checkpoint", CheckpointAuthority: "synthetic-test-only", APFSIdentity: "11111111-1111-4111-8111-111111111111", Enode: enode, PGData: "/synthetic/pgdata", NativeBytes: 1 << 30, IndexerBytes: 1 << 30, CheckpointBytes: 1 << 20, ScratchBytes: 1 << 20, FreeBytes: 20 << 30, BackupBytes: 8 << 30, PartTransactions: 2, APIReservation: 1, LogicalWriters: 7, FreshPolicy: "new_uncached_canonical_post_response_generation_fence_commit"}
	app := config.NewConfig()
	app.RPC.Endpoint = "https://synthetic.invalid"
	app.RPC.RateLimit = 5
	app.Database.Driver = "postgres"
	app.Database.Postgres.DSN = "postgres://synthetic.invalid/synthetic"
	app.Database.Postgres.MaxConns = 4
	app.Indexer.StatePrefetch = true
	app.Indexer.StartHeight = d.StartHeight
	policy, _ := json.Marshal(f.Policy)
	app.Source.Execution = &config.ExecutionSourceConfig{Socket: c.Socket, AnchorHeight: point.Height, AnchorHash: c.AnchorHash, AnchorRoot: c.AnchorRoot, Consensus: &chains.ConsensusPolicy{Profile: "stablenet", Protocol: consensus.IndependentProtocol, Parameters: string(policy)}}
	in := nativepreflight.Inputs{App: filepath.Join(dir, "app.yaml"), Native: filepath.Join(dir, "native.json"), Checkpoint: filepath.Join(dir, "checkpoint.json"), Declaration: filepath.Join(dir, "declaration.json")}
	writeJSON(t, in.Checkpoint, format.Manifest{Version: 1, Mode: "genesis", Coverage: "full", Chain: d.Chain, Ref: d.Anchor, Root: d.Root, Header: f.Policy.AnchorHeader})
	return in, app, c, d
}
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestStableNetNativeAndE6PolicyBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		change     func(*config.Config, *nativecfg.Config, *nativepreflight.Declaration)
	}{
		{"supported_declared_E6", "PASS", func(*config.Config, *nativecfg.Config, *nativepreflight.Declaration) {}},
		{"unresolved_E6_parameters", "HOLD", func(a *config.Config, _ *nativecfg.Config, _ *nativepreflight.Declaration) {
			a.Source.Execution.Consensus.Parameters = "HOLD"
		}},
		{"unresolved_parameters_bad_protocol", "FAIL", func(a *config.Config, _ *nativecfg.Config, _ *nativepreflight.Declaration) {
			a.Source.Execution.Consensus.Parameters = "HOLD"
			a.Source.Execution.Consensus.Protocol = "unsupported"
		}},
		{"unresolved_native_genesis", "HOLD", func(_ *config.Config, c *nativecfg.Config, _ *nativepreflight.Declaration) { c.GenesisHash = "HOLD" }},
		{"production_default_spool_quotas", "PASS", func(_ *config.Config, c *nativecfg.Config, _ *nativepreflight.Declaration) {
			c.SpoolBytes = 0
			c.SpoolRecords = 0
		}},
		{"without_optional_E6", "PASS", func(a *config.Config, _ *nativecfg.Config, _ *nativepreflight.Declaration) {
			a.Source.Execution.Consensus = nil
		}},
		{"network_mismatch", "FAIL", func(_ *config.Config, c *nativecfg.Config, _ *nativepreflight.Declaration) { c.NetworkID = 999 }},
		{"unsupported_profile", "FAIL", func(a *config.Config, _ *nativecfg.Config, _ *nativepreflight.Declaration) {
			a.Source.Execution.Consensus.Profile = "arc"
		}},
		{"unsupported_protocol", "FAIL", func(a *config.Config, _ *nativecfg.Config, _ *nativepreflight.Declaration) {
			a.Source.Execution.Consensus.Protocol = "rpc-finalized"
		}},
		{"bad_fork", "FAIL", func(a *config.Config, _ *nativecfg.Config, _ *nativepreflight.Declaration) {
			var p consensus.TrustPolicy
			_ = json.Unmarshal([]byte(a.Source.Execution.Consensus.Parameters), &p)
			p.Fork = "unknown"
			b, _ := json.Marshal(p)
			a.Source.Execution.Consensus.Parameters = string(b)
		}},
		{"wrong_anchor", "FAIL", func(a *config.Config, _ *nativecfg.Config, _ *nativepreflight.Declaration) {
			var p consensus.TrustPolicy
			_ = json.Unmarshal([]byte(a.Source.Execution.Consensus.Parameters), &p)
			p.AnchorHeader[10] ^= 1
			b, _ := json.Marshal(p)
			a.Source.Execution.Consensus.Parameters = string(b)
		}},
		{"duplicate_policy", "FAIL", func(a *config.Config, _ *nativecfg.Config, _ *nativepreflight.Declaration) {
			a.Source.Execution.Consensus.Parameters = `{"Fork":"anzeon-v1","Fork":"unknown"}`
		}},
		{"invalid_peer", "FAIL", func(_ *config.Config, c *nativecfg.Config, _ *nativepreflight.Declaration) {
			c.Peers = []string{"not-an-enode"}
		}},
		{"capacity", "FAIL", func(_ *config.Config, c *nativecfg.Config, _ *nativepreflight.Declaration) {
			c.Capacity = &nativecfg.CapacityConfig{NodeBytes: 1, StopFreeBytes: 10, ResumeFreeBytes: 9}
		}},
		{"small_spool", "FAIL", func(_ *config.Config, c *nativecfg.Config, _ *nativepreflight.Declaration) { c.SpoolBytes = 1 }},
		{"large_records", "FAIL", func(_ *config.Config, c *nativecfg.Config, _ *nativepreflight.Declaration) {
			c.SpoolRecords = 1<<20 + 1
		}},
		{"genesis_start", "FAIL", func(_ *config.Config, c *nativecfg.Config, _ *nativepreflight.Declaration) { c.AnchorHeight = 1 }},
		{"missing_peer_apfs_checkpoint", "HOLD", func(_ *config.Config, c *nativecfg.Config, d *nativepreflight.Declaration) {
			c.Peers = []string{}
			d.Enode = ""
			d.APFSIdentity = "HOLD"
			d.CheckpointAuthority = ""
			d.CheckpointPath = "HOLD"
		}},
		{"mixed_profile", "FAIL", func(_ *config.Config, _ *nativecfg.Config, d *nativepreflight.Declaration) { d.Profile = "arc-native" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, a, c, d := stableFixture(t)
			tc.change(a, &c, &d)
			writeJSON(t, in.Native, c)
			writeJSON(t, in.Declaration, d)
			b, err := yaml.Marshal(a)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(in.App, b, 0600); err != nil {
				t.Fatal(err)
			}
			r := nativepreflight.Check(context.Background(), in, Product())
			if r.Status != tc.want {
				t.Fatalf("want %s: %+v", tc.want, r)
			}
		})
	}
}

// A genesis start fixes the original height even when its numeric input is HOLD.
// The other files can agree with each other and still contradict that mode.
func TestGenesisModeCannotHideKnownCheckpointConflict(t *testing.T) {
	in, app, c, d := stableFixture(t)
	app.Source.Execution.Consensus = nil
	app.Source.Execution.AnchorHeight = 100
	app.Indexer.StartHeight = 101
	d.Anchor.Height, d.StartHeight, d.RetainedFloor = 100, 101, 100
	writeJSON(t, in.Declaration, d)
	writeJSON(t, in.Checkpoint, format.Manifest{Version: 1, Mode: "checkpoint", Coverage: "full", Chain: d.Chain, Ref: d.Anchor, Root: d.Root})
	b, err := yaml.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(in.App, b, 0600); err != nil {
		t.Fatal(err)
	}
	b, err = json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err = json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	raw["anchor_height"] = "HOLD"
	writeJSON(t, in.Native, raw)
	if r := nativepreflight.Check(context.Background(), in, Product()); r.Status != "FAIL" {
		t.Fatalf("known nonzero checkpoint conflicts with genesis mode: %+v", r)
	}
}
