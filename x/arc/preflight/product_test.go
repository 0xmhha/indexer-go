package preflight

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/0xmhha/indexer-go/internal/config"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/internal/nativepreflight"
)

func fixture(t *testing.T) nativepreflight.Inputs {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, name := range []string{"app.yaml", "native.json", "retention.json", "checkpoint.json", "declaration.json"} {
		b, err := os.ReadFile(filepath.Join("../../../configs/execution/preflight/synthetic-arc", name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	return nativepreflight.Inputs{App: paths["app.yaml"], Native: paths["native.json"], Retention: paths["retention.json"], Checkpoint: paths["checkpoint.json"], Declaration: paths["declaration.json"]}
}
func replace(t *testing.T, path, old, value string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(old)) {
		t.Fatalf("fixture has no %s", old)
	}
	if err = os.WriteFile(path, bytes.ReplaceAll(b, []byte(old), []byte(value)), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestNativePreflightBehavior(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		change     func(*testing.T, *nativepreflight.Inputs)
	}{
		{"complete", "PASS", func(*testing.T, *nativepreflight.Inputs) {}},
		{"shared_native_executable", "PASS", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Retention, "/synthetic/bin/maintenance", "/synthetic/bin/executor")
		}},
		{"shared_executable_inside_data", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Native, "/synthetic/bin/executor", "/synthetic/native/executor")
			replace(t, i.Retention, "/synthetic/bin/maintenance", "/synthetic/native/executor")
		}},
		{"native_path_hold", "HOLD", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Native, "/synthetic/native", "HOLD")
			replace(t, i.Retention, "/synthetic/native", "HOLD")
		}},
		{"invalid_apfs", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"apfs_identity": "11111111-1111-4111-8111-111111111111"`, `"apfs_identity": "???"`)
		}},
		{"identical_anchor_placeholders", "HOLD", func(t *testing.T, i *nativepreflight.Inputs) {
			for _, path := range []string{i.App, i.Native, i.Retention, i.Checkpoint, i.Declaration} {
				replace(t, path, "0x"+strings.Repeat("11", 32), "HOLD")
			}
		}},
		{"unresolved_anchor_known_root_conflict", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			for _, path := range []string{i.App, i.Native, i.Retention, i.Checkpoint, i.Declaration} {
				replace(t, path, "0x"+strings.Repeat("11", 32), "HOLD")
			}
			replace(t, i.Retention, "0x"+strings.Repeat("22", 32), "0x"+strings.Repeat("33", 32))
		}},
		{"wrong_hash_type", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Native, `"Hash": "0x`+strings.Repeat("11", 32)+`"`, `"Hash": {"bad":1}`)
		}},
		{"null_scalar_yaml", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.App, "rate_limit: 5", "rate_limit: null") }},
		{"yaml_alias", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.App, "rate_limit: 5", "rate_limit: &limit 5\n  timeout: *limit")
		}},
		{"trailing_json", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			b, err := os.ReadFile(i.Native)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(i.Native, append(b, []byte(` {}`)...), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"socket_in_native", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Native, "/synthetic/ipc/execution.sock", "/synthetic/native/execution.sock")
			replace(t, i.App, "/synthetic/ipc/execution.sock", "/synthetic/native/execution.sock")
		}},
		{"writer_policy", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"logical_writers": 7`, `"logical_writers": 8`)
		}},
		{"part_policy", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"part_transactions": 2`, `"part_transactions": 3`)
		}},
		{"api_reservation", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"api_reservation": 1`, `"api_reservation": 0`)
		}},
		{"fresh_policy", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, "new_uncached_canonical_post_response_generation_fence_commit", "ttl-cache")
		}},
		{"unresolved_native_root_known_checkpoint_conflict", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Retention, "0x"+strings.Repeat("22", 32), "HOLD")
			replace(t, i.Checkpoint, "0x"+strings.Repeat("22", 32), "0x"+strings.Repeat("33", 32))
		}},
		{"retained_resume", "PASS", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"start_mode": "initial"`, `"start_mode": "resume"`)
			replace(t, i.Declaration, `"start_height": 101`, `"start_height": 133`)
			replace(t, i.Declaration, `"retained_floor": 100`, `"retained_floor": 120`)
			replace(t, i.App, "start_height: 101", "start_height: 133")
		}},
		{"engine_readonly", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.App, "  driver: postgres", "  driver: postgres\n  readonly: true")
		}},
		{"unresolved_capacity", "HOLD", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"native_bytes": 1073741824`, `"native_bytes": "HOLD"`)
		}},
		{"unknown_capacity_known_insufficient_free", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"native_bytes": 1073741824`, `"native_bytes": "HOLD"`)
			replace(t, i.Declaration, `"free_bytes": 21474836480`, `"free_bytes": 1`)
		}},
		{"unknown_height_known_initial_start_conflict", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"Height": 100`, `"Height": "HOLD"`)
			replace(t, i.Declaration, `"start_height": 101`, `"start_height": 102`)
			replace(t, i.App, "start_height: 101", "start_height: 102")
		}},
		{"unknown_genesis_known_network_conflict", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			for _, path := range []string{i.Native, i.Retention, i.Checkpoint, i.Declaration} {
				replace(t, path, "0xe20e653af4441e8c6088e172b129d56420139824400477287b46e7101ae2bb1f", "HOLD")
			}
			replace(t, i.Declaration, `"network_id": 5042002`, `"network_id": 5042`)
		}},
		{"metadata_unknown_network", "HOLD", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"network_id": 5042002`, `"network_id": "HOLD"`)
		}},
		{"native_hold_known_retention_nested_path", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Native, "/synthetic/native", "HOLD")
			replace(t, i.Native, "/synthetic/spool", "/synthetic/native/spool")
			replace(t, i.Retention, "/synthetic/spool", "/synthetic/native/spool")
		}},
		{"backup_operator_paths_hold", "HOLD", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Retention, `"NativeExecutable":`, `"BackupOutput":"HOLD","IndexerReceipt":"HOLD","IndexerArtifact":"HOLD","NativeExecutable":`)
		}},
		{"genesis_mode_with_known_checkpoint_anchor", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Checkpoint, `"Mode": "checkpoint"`, `"Mode": "genesis"`)
			replace(t, i.Checkpoint, `"Height": 100`, `"Height": "HOLD"`)
			replace(t, i.Checkpoint, "0x"+strings.Repeat("11", 32), "HOLD")
		}},
		{"nul_native_path", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Native, "/synthetic/native", `/synthetic/\u0000native`)
			replace(t, i.Retention, "/synthetic/native", `/synthetic/\u0000native`)
		}},
		{"operator_placeholders", "HOLD", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"synthetic-test-only"`, `"HOLD"`)
			replace(t, i.Declaration, `"/synthetic/checkpoint"`, `""`)
			replace(t, i.Declaration, `"enode://`+"79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8"+`@127.0.0.1:30303"`, `""`)
		}},
		{"chain", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.Native, `"5042002"`, `"5042"`) }},
		{"known_genesis", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, "0xe20e653af4441e8c6088e172b129d56420139824400477287b46e7101ae2bb1f", "0x"+strings.Repeat("33", 32))
		}},
		{"network", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"network_id": 5042002`, `"network_id": 5042`)
		}},
		{"anchor_hash", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Native, "0x"+strings.Repeat("11", 32), "0x"+strings.Repeat("33", 32))
		}},
		{"height", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.Native, `"Height": 100`, `"Height": 99`) }},
		{"generation", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Native, `"Generation": 1`, `"Generation": 2`)
		}},
		{"root", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Retention, "0x"+strings.Repeat("22", 32), "0x"+strings.Repeat("33", 32))
		}},
		{"start", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.App, "start_height: 101", "start_height: 102")
		}},
		{"floor", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"retained_floor": 100`, `"retained_floor": 102`)
		}},
		{"db_pool", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.App, "max_conns: 4", "max_conns: 7") }},
		{"rpc_budget", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.App, "rate_limit: 5", "rate_limit: 6") }},
		{"finality", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.App, "finality: head", "finality: finalized")
		}},
		{"nested_spool", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Native, "/synthetic/spool", "/synthetic/native/spool")
		}},
		{"scratch_in_native", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Native, "/synthetic/scratch", "/synthetic/native/scratch")
		}},
		{"checkpoint_in_native", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, "/synthetic/checkpoint", "/synthetic/native/checkpoint")
		}},
		{"negative_bounds", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.Native, `"Window": 8`, `"Window": -1`) }},
		{"quota_mismatch", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Retention, `"SpoolRecords": 262144`, `"SpoolRecords": 100`)
		}},
		{"headroom", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"free_bytes": 21474836480`, `"free_bytes": 1`)
		}},
		{"overflow", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"native_bytes": 1073741824`, `"native_bytes": 18446744073709551615`)
		}},
		{"hold_and_conflict", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Declaration, `"synthetic-test-only"`, `"HOLD"`)
			replace(t, i.App, "rate_limit: 5", "rate_limit: 0")
		}},
		{"unknown_json", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.Native, `"Window": 8`, `"unknown": 8`) }},
		{"duplicate_json", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.Native, `"Window": 8`, `"Window": 8, "window": 9`)
		}},
		{"unknown_yaml", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.App, "rate_limit: 5", "unknown: 5") }},
		{"duplicate_yaml", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.App, "rate_limit: 5", "rate_limit: 5\n  rate_limit: 6")
		}},
		{"malformed", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.Native, `"Window": 8`, `"Window": "???"`) }},
		{"truncated", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.Native, `"Window": 8`, `"Window": `) }},
		{"null", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.Native, `"Window": 8`, `"Window": null`) }},
		{"oversize", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			if err := os.WriteFile(i.Native, bytes.Repeat([]byte(" "), (1<<20)+1), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"partial_baseline", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) { replace(t, i.Checkpoint, `"full"`, `"partial"`) }},
		{"unsupported_e6", "FAIL", func(t *testing.T, i *nativepreflight.Inputs) {
			replace(t, i.App, "    socket:", "    consensus: {profile: arc, protocol: fake, parameters: '{}'}\n    socket:")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := fixture(t)
			tc.change(t, &in)
			r := nativepreflight.Check(context.Background(), in, Product())
			if r.Status != tc.want {
				t.Fatalf("want %s: %+v", tc.want, r)
			}
			if r.OperationalReadiness != "not_authorized" || r.LiveFreshness != "not_checked" {
				t.Fatal(r)
			}
		})
	}
}
func TestOfflineDeterminismRedactionAndNoSourceMutation(t *testing.T) {
	in := fixture(t)
	replace(t, in.App, "https://synthetic.invalid", "https://user:private-password@synthetic.invalid/?token=private-query")
	before := map[string][]byte{}
	paths := []string{in.App, in.Native, in.Retention, in.Checkpoint, in.Declaration}
	for _, p := range paths {
		before[p], _ = os.ReadFile(p)
	}
	t.Setenv("INDEXER_RPC_RATE_LIMIT", "99")
	t.Setenv("INDEXER_DB_POSTGRES_MAX_CONNS", "99")
	t.Setenv("INDEXER_DB_POSTGRES_DSN", "private-environment-secret")
	r := nativepreflight.Check(context.Background(), in, Product())
	b, _ := json.Marshal(r)
	s, _ := json.Marshal(nativepreflight.Check(context.Background(), in, Product()))
	if r.Status != "PASS" || !bytes.Equal(b, s) {
		t.Fatal(string(b))
	}
	if bytes.Contains(b, []byte("private-")) {
		t.Fatal("secret leaked")
	}
	for _, p := range paths {
		after, _ := os.ReadFile(p)
		if !bytes.Equal(before[p], after) {
			t.Fatal("source mutated")
		}
	}
	entries, err := os.ReadDir(filepath.Dir(in.App))
	if err != nil || len(entries) != 5 {
		t.Fatal("created side effects", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = nativepreflight.Check(ctx, in, Product())
	if r.Status != "FAIL" {
		t.Fatal(r)
	}
	for _, tc := range []struct {
		status string
		code   int
	}{{"PASS", 0}, {"HOLD", 2}, {"FAIL", 1}} {
		if (nativepreflight.Report{Status: tc.status}).ExitCode() != tc.code {
			t.Fatal(tc)
		}
	}
	var out bytes.Buffer
	if code := nativepreflight.Run(context.Background(), []string{"--secret=private-password"}, &out, Product()); code != 1 || strings.Contains(out.String(), "private-password") {
		t.Fatal(code, out.String())
	}
}
func TestUnsafeFileLinks(t *testing.T) {
	in := fixture(t)
	link := in.Native + ".link"
	if err := os.Symlink(in.Native, link); err != nil {
		t.Fatal(err)
	}
	in.Native = link
	if r := nativepreflight.Check(context.Background(), in, Product()); r.Status != "FAIL" {
		t.Fatal(r)
	}
}

func TestProofSelectionUsesProductionNormalizationAndStrictSchema(t *testing.T) {
	for _, tc := range []struct {
		name, want        string
		unknown, conflict bool
	}{{name: "normalized_order", want: "PASS"}, {name: "known_selection_conflict", want: "FAIL", conflict: true}, {name: "unknown_selection_key", want: "FAIL", unknown: true}} {
		t.Run(tc.name, func(t *testing.T) {
			in := fixture(t)
			c := config.NewConfig()
			b, err := os.ReadFile(in.App)
			if err != nil {
				t.Fatal(err)
			}
			if err = config.DecodeStrict(b, c); err != nil {
				t.Fatal(err)
			}
			selection := &stateproof.Selection{Targets: []stateproof.Target{{Address: common.Address{3}, Code: true}, {Address: common.Address{1}, Code: true}}}
			c.Source.Execution.RequiredStateProofs = selection
			b, err = yaml.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(in.App, b, 0600); err != nil {
				t.Fatal(err)
			}
			var collectorConfig collector.Config
			b, err = os.ReadFile(in.Native)
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(b, &collectorConfig); err != nil {
				t.Fatal(err)
			}
			collectorConfig.Native.StateProofs = &stateproof.Selection{Targets: []stateproof.Target{selection.Targets[1], selection.Targets[0]}}
			if tc.conflict {
				collectorConfig.Native.StateProofs.Targets[0].Code = false
			}
			b, err = json.Marshal(collectorConfig)
			if err != nil {
				t.Fatal(err)
			}
			if tc.unknown {
				b = bytes.Replace(b, []byte(`"targets":`), []byte(`"unknown":"private-value","targets":`), 1)
			}
			if err = os.WriteFile(in.Native, b, 0600); err != nil {
				t.Fatal(err)
			}
			r := nativepreflight.Check(context.Background(), in, Product())
			if r.Status != tc.want {
				t.Fatalf("want %s: %+v", tc.want, r)
			}
			report, _ := json.Marshal(r)
			if bytes.Contains(report, []byte("private-value")) {
				t.Fatal("secret leaked")
			}
		})
	}
}

type truncatedWriter struct{}

func (truncatedWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }
func TestTruncatedReadyReportFails(t *testing.T) {
	in := fixture(t)
	args := []string{"--app", in.App, "--native", in.Native, "--retention", in.Retention, "--checkpoint", in.Checkpoint, "--declaration", in.Declaration}
	if nativepreflight.Run(context.Background(), args, truncatedWriter{}, Product()) != 1 {
		t.Fatal("short output of an otherwise PASS report must fail")
	}
}
