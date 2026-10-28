package nativemonitor_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/execwire/configfile"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/internal/executionops"
	"github.com/0xmhha/indexer-go/internal/nativediagnostics"
	"github.com/0xmhha/indexer-go/internal/nativemonitor"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	arc "github.com/0xmhha/indexer-go/x/arc/diagnostics"
	stablenet "github.com/0xmhha/indexer-go/x/stablenet/diagnostics"
)

func TestActualProductDiagnosticsRemainOfflineAndCompatible(t *testing.T) {
	for _, product := range []struct {
		p     nativediagnostics.Product
		chain string
	}{{arc.Product(), "5042"}, {stablenet.Product(), "8283"}} {
		t.Run(product.p.Name, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			write := func(name string, v any) string {
				t.Helper()
				p := filepath.Join(dir, name)
				b, e := json.Marshal(v)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(p, b, 0600); e != nil {
					t.Fatal(e)
				}
				return p
			}
			chain := execwire.Chain{ID: product.chain, Genesis: execwire.Hash{1}}
			anchor := execwire.Ref{Generation: 1, Hash: chain.Genesis}
			control := port.ExecutionControl{ExecutionConfig: port.ExecutionConfig{Chain: chain, Anchor: anchor, AnchorRoot: execwire.Hash{2}, PartitionBlocks: 1000000}, Generation: 1, Visible: anchor, Root: execwire.Hash{2}, BaselineReady: true}
			source := format.SourceObservation{Chain: chain, Anchor: anchor, Committed: anchor, Parent: anchor, ParentRoot: control.Root, HeaderAvailable: true, StateAvailable: true}
			cp := executionops.CheckpointInspection{Control: control, Bootstrap: postgres.ExecutionBootstrapInfo{Coverage: "full", Mode: "genesis", Digest: append([]byte{1}, make([]byte, 31)...)}}
			spoolDir := filepath.Join(dir, "source")
			store, err := spool.Open(t.Context(), spool.Options{Path: spoolDir, Chain: chain, Anchor: anchor, MaxBytes: 1 << 20, MaxRecords: 8})
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			args := []string{"--spool", spoolDir, "--source-artifact", write("source.json", source), "--checkpoint-artifact", write("checkpoint.json", cp), "--retention-artifact", write("retention.json", executionops.RetentionInspection{Control: control})}
			var output bytes.Buffer
			if code := nativediagnostics.Run(t.Context(), args, &output, product.p); code != 2 {
				t.Fatalf("product diagnostic: %d %s", code, &output)
			}
			var report nativemonitor.DiagnosticReport
			if err = configfile.JSON(output.Bytes(), &report); err != nil {
				t.Fatal("consumer schema drift", err)
			}
			if len(report.Observations) < 4 {
				t.Fatal("actual typed observations missing")
			}
			canonical, err := json.Marshal(report)
			if err != nil || !bytes.Equal(bytes.TrimSpace(output.Bytes()), canonical) {
				t.Fatal("consumer dropped or changed diagnostic fields")
			}
			at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
			n := func(v uint64) *uint64 { return &v }
			id := nativemonitor.Identity{Profile: product.p.Name, Chain: chain, Anchor: anchor}
			policy := nativemonitor.Policy{Version: 1, Expected: &id, MaxAgeSeconds: n(60), CapacityWarnBPS: n(8000), CapacityStopBPS: n(9500), MinFreeBytes: n(100), BacklogWarnBlocks: n(10), BacklogStopBlocks: n(20), LagWarnSeconds: n(5), LagStopSeconds: n(10), RequiredStorage: []string{"raw", "filesystem"}}
			snapshot := nativemonitor.Snapshot{Version: 1, Identity: &id, Diagnostics: &nativemonitor.DiagnosticSample{ObservedAt: &at, Report: &report}, Metrics: &nativemonitor.Metrics{ObservedAt: &at, Pipeline: &nativemonitor.Pipeline{Generation: n(1), Head: n(0), Received: n(0), Processed: n(0), ACK: n(0), OldestPendingSeconds: n(0), ACKLagSeconds: n(0), WorkerLagSeconds: n(0)}, Storage: []nativemonitor.Storage{{Kind: "raw", Used: n(0), Limit: n(1000)}, {Kind: "filesystem", Free: n(1000)}}}}
			policyPath := write("policy.json", policy)
			snapshotPath := write("snapshot.json", snapshot)
			before := map[string][]byte{}
			ids := map[string]os.FileInfo{}
			walk := func(check bool) {
				t.Helper()
				visited := 0
				e := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.IsDir() {
						return nil
					}
					visited++
					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					info, err := os.Stat(path)
					if err != nil {
						return err
					}
					if check {
						if !bytes.Equal(before[path], data) || ids[path] == nil || !os.SameFile(ids[path], info) {
							t.Fatal("monitor modified fixture")
						}
					} else {
						before[path] = data
						ids[path] = info
					}
					return nil
				})
				if e != nil {
					t.Fatal(e)
				}
				if check && visited != len(before) {
					t.Fatal("monitor removed fixture files")
				}
			}
			walk(false)
			output.Reset()
			if code := nativemonitor.Run(t.Context(), []string{"--policy", policyPath, "--snapshot", snapshotPath, "--at", at.Format(time.RFC3339Nano)}, &output); code != 2 {
				t.Fatalf("monitor compatibility: %d %s", code, &output)
			}
			var result nativemonitor.Report
			if err = json.Unmarshal(output.Bytes(), &result); err != nil || result.Status != "HOLD" || result.AlertLevel != "unknown" || result.Actuation != "none" {
				t.Fatal(output.String())
			}
			walk(true)
		})
	}
}
