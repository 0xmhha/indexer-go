package nativediagnostics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/execwire/metadata"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/internal/executionops"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
)

func privateDir(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func write(t *testing.T, path string, v any) string {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	return path
}
func sealed(t *testing.T, path string, v any) string {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return write(t, path, struct {
		Data   json.RawMessage
		Digest execwire.Hash
	}{b, execwire.Hash(sha256.Sum256(b))})
}
func observations() (executionops.CheckpointInspection, format.SourceObservation, executionops.RetentionInspection) {
	c := port.ExecutionControl{ExecutionConfig: port.ExecutionConfig{Chain: execwire.Chain{ID: "5042", Genesis: execwire.Hash{1}}, Anchor: execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}, AnchorRoot: execwire.Hash{2}, PartitionBlocks: 1000000}, Generation: 1, Visible: execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}, Root: execwire.Hash{2}, BaselineReady: true}
	cp := executionops.CheckpointInspection{Control: c, Bootstrap: postgres.ExecutionBootstrapInfo{Coverage: "full", Mode: "genesis", Digest: append([]byte{1}, make([]byte, 31)...)}}
	src := format.SourceObservation{Chain: c.Chain, Anchor: c.Anchor, Committed: c.Visible, Parent: c.Visible, ParentRoot: c.Root, HeaderAvailable: true, StateAvailable: true}
	return cp, src, executionops.RetentionInspection{Control: c}
}
func arguments(t *testing.T, cp executionops.CheckpointInspection, src format.SourceObservation, rt executionops.RetentionInspection) []string {
	t.Helper()
	dir := privateDir(t)
	return []string{"--checkpoint-artifact", write(t, filepath.Join(dir, "cp"), cp), "--source-artifact", write(t, filepath.Join(dir, "source"), src), "--retention-artifact", write(t, filepath.Join(dir, "retention"), rt)}
}
func finding(r Report, field, code string) bool {
	for _, f := range r.Findings {
		if f.Field == field && f.Code == code {
			return true
		}
	}
	return false
}
func TestUnknownNeverReady(t *testing.T) {
	var out bytes.Buffer
	if code := Run(t.Context(), nil, &out, testProduct("arc-native")); code != 2 {
		t.Fatalf("uninspected native/source/indexer must HOLD/2, got %d: %s", code, out.String())
	}
	var r Report
	if json.Unmarshal(out.Bytes(), &r) != nil || r.Status != Hold || r.Authority != "not_authorized" || r.LiveFreshness != "not_performed" || r.FullState != "not_performed" {
		t.Fatal(out.String())
	}
}
func TestCompatibleProductsAndInputPreservation(t *testing.T) {
	for _, profile := range []string{"arc-native", "stablenet-native"} {
		t.Run(profile, func(t *testing.T) {
			cp, src, rt := observations()
			args := arguments(t, cp, src, rt)
			dir := privateDir(t)
			path := filepath.Join(dir, "spool")
			s, e := spool.Open(t.Context(), spool.Options{Path: path, Chain: src.Chain, Anchor: src.Anchor, MaxBytes: 1 << 20, MaxRecords: 8})
			if e != nil {
				t.Fatal(e)
			}
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
			native := filepath.Join(dir, "native")
			if e = os.MkdirAll(native, 0700); e != nil {
				t.Fatal(e)
			}
			if profile == "stablenet-native" {
				if e = os.MkdirAll(filepath.Join(native, "indexer-stablenet", "chaindata"), 0700); e != nil {
					t.Fatal(e)
				}
			}
			args = append(args, "--spool", path, "--native", native)
			type saved struct {
				info os.FileInfo
				data []byte
			}
			before := map[string]saved{}
			roots := []string{dir, filepath.Dir(args[1])}
			snapshot := func(check bool) {
				for _, root := range roots {
					e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
						if e != nil {
							return e
						}
						i, e := d.Info()
						if e != nil {
							return e
						}
						var b []byte
						if !d.IsDir() {
							b, e = os.ReadFile(p)
						}
						if e != nil {
							return e
						}
						if check {
							old, ok := before[p]
							if !ok || !os.SameFile(old.info, i) || !bytes.Equal(old.data, b) {
								t.Errorf("source changed: %s", p)
							}
						} else {
							before[p] = saved{i, b}
						}
						return nil
					})
					if e != nil {
						t.Fatal(e)
					}
				}
			}
			snapshot(false)
			r := Check(t.Context(), args, testProduct(profile))
			if r.Status != Hold || !finding(r, "checkpoint.source", "compatible_observation") || !finding(r, "native.fence", "optional_marker_absent") {
				t.Fatalf("%+v", r)
			}
			var a, b bytes.Buffer
			if Run(t.Context(), args, &a, testProduct(profile)) != 2 || Run(t.Context(), args, &b, testProduct(profile)) != 2 || !bytes.Equal(a.Bytes(), b.Bytes()) || a.Len() > 64<<10 {
				t.Fatal("nondeterministic or incorrect exit")
			}
			snapshot(true)
		})
	}
}
func TestIndependentKnownConflictsDominateUnknownAndPending(t *testing.T) {
	cases := map[string]func(*executionops.CheckpointInspection, *format.SourceObservation, *executionops.RetentionInspection){
		"chain": func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			s.Chain.ID = "5042002"
		},
		"anchor": func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			s.Anchor.Height = 1
			s.Anchor.Hash[0] = 9
			s.Committed = s.Anchor
		},
		"root": func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			s.ParentRoot[0]++
		},
		"generation": func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			s.Committed.Generation++
		},
		"ack_height": func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			s.Committed.Height++
		},
		"ack_hash": func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			c.Control.Visible.Height = 1
			c.Control.Visible.Hash[0] = 8
			r.Control = c.Control
			s.Committed = c.Control.Visible
			s.Committed.Hash[0]++
		},
		"public": func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			r.Control.Root[0]++
		},
		"phase": func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			r.Pending = true
		},
		"partial_permit": func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			r.Permit.Token[0] = 1
		},
		"bootstrap": func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			c.Bootstrap.Coverage = "unsupported"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cp, s, rt := observations()
			mutate(&cp, &s, &rt)
			s.TransitionPending = true
			args := arguments(t, cp, s, rt)
			r := Check(t.Context(), args, testProduct("arc-native"))
			if r.Status != Fail {
				t.Fatalf("known conflict hidden by HOLD: %+v", r)
			}
			var out bytes.Buffer
			if Run(t.Context(), args, &out, testProduct("arc-native")) != 1 {
				t.Fatal("exit must be 1")
			}
		})
	}
}
func TestPendingIncompleteAndOlderGenerationHold(t *testing.T) {
	for _, change := range []func(*executionops.CheckpointInspection, *format.SourceObservation, *executionops.RetentionInspection){
		func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			c.Control.Rewinding = true
			r.Control = c.Control
		},
		func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			c.Control.BaselineReady = false
			r.Control = c.Control
		},
		func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			s.HeaderAvailable = false
			s.StateAvailable = false
			s.Parent = execwire.Ref{}
			s.ParentRoot = execwire.Hash{}
		},
		func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			s.TransitionPending = true
		},
		func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			s.MaintenancePending = true
		},
		func(c *executionops.CheckpointInspection, s *format.SourceObservation, r *executionops.RetentionInspection) {
			c.Control.Generation = 2
			c.Control.Visible.Generation = 2
			r.Control = c.Control
			s.Parent = c.Control.Visible
		},
	} {
		cp, s, rt := observations()
		change(&cp, &s, &rt)
		r := Check(t.Context(), arguments(t, cp, s, rt), testProduct("stablenet-native"))
		if r.Status != Hold {
			t.Fatalf("%+v", r)
		}
	}
}
func permit() retention.Permit {
	c := execwire.Chain{ID: "5042", Genesis: execwire.Hash{1}}
	return retention.Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash{10}, Token: execwire.Hash{11}, Request: retention.Request{Version: 1, Chain: c, Anchor: execwire.Ref{Generation: 1, Hash: c.Genesis}, ACK: execwire.Ref{Generation: 2, Height: 90000, Hash: execwire.Hash{2}}, Tail: execwire.Ref{Generation: 2, Height: 90002, Hash: execwire.Hash{3}}, Head: execwire.Ref{Height: 90003, Hash: execwire.Hash{4}}, HeadRoot: execwire.Hash{5}, SourceDigest: execwire.Hash{6}, KeepBlocks: 86400, Through: 3600}, Visible: execwire.Ref{Generation: 2, Height: 90001, Hash: execwire.Hash{8}}, Root: execwire.Hash{9}}
}
func TestNativeMaintenanceFloorsPhasesAndConflicts(t *testing.T) {
	for _, profile := range []string{"arc-native", "stablenet-native"} {
		for _, phase := range []string{"copy", "swap", "cleanup", "done"} {
			t.Run(profile+phase, func(t *testing.T) {
				p := permit()
				dir := privateDir(t)
				native := filepath.Join(dir, "native")
				if e := os.Mkdir(native, 0700); e != nil {
					t.Fatal(e)
				}
				marker := filepath.Join(dir, ".native.execution-retention")
				floorDir := native
				if profile == "stablenet-native" {
					marker = filepath.Join(native, ".execution-retention")
					floorDir = filepath.Join(native, "indexer-stablenet", "chaindata")
					if e := os.MkdirAll(floorDir, 0700); e != nil {
						t.Fatal(e)
					}
				}
				replacement := retention.FileIdentity{Device: 1, Inode: 2}
				if phase == "copy" {
					replacement = retention.FileIdentity{}
				}
				sealed(t, marker, retention.NativeFence{Version: 1, Permit: p, Phase: phase, Original: retention.FileIdentity{Device: 1, Inode: 1}, Replacement: replacement})
				floorPath := sealed(t, filepath.Join(floorDir, ".execution-retention-floor"), retention.NativeFloor{Version: 1, Permit: p})
				cp, s, rt := observations()
				cp.Control.Generation = 2
				cp.Control.Visible = p.Visible
				cp.Control.Root = p.Root
				rt.Control = cp.Control
				s.Committed = p.Request.ACK
				s.Parent = p.Visible
				s.ParentRoot = p.Root
				args := append(arguments(t, cp, s, rt), "--native", native)
				r := Check(t.Context(), args, testProduct(profile))
				if r.Status != Hold || !finding(r, "native.fence", "maintenance_fence_present") {
					t.Fatalf("%+v", r)
				}
				bad := p
				bad.Request.Chain.ID = "5042002"
				sealed(t, floorPath, retention.NativeFloor{Version: 1, Permit: bad})
				if r = Check(t.Context(), args, testProduct(profile)); r.Status != Fail {
					t.Fatalf("foreign floor hidden: %+v", r)
				}
				sealed(t, floorPath, retention.NativeFloor{Version: 1, Permit: p})
				sealed(t, marker, retention.NativeFence{Version: 1, Permit: p, Phase: "unknown", Original: retention.FileIdentity{Inode: 1}})
				if r = Check(t.Context(), args, testProduct(profile)); r.Status != Fail {
					t.Fatal("unsupported phase accepted")
				}
			})
		}
	}
}
func TestHistoricalFloorDoesNotRequireCurrentACK(t *testing.T) {
	p := permit()
	cp, s, rt := observations()
	cp.Control.Generation = 3
	cp.Control.Visible = execwire.Ref{Generation: 3, Height: 91000, Hash: execwire.Hash{22}}
	cp.Control.Root = execwire.Hash{23}
	rt.Control = cp.Control
	s.Committed = cp.Control.Visible
	s.Parent = cp.Control.Visible
	s.ParentRoot = cp.Control.Root
	dir := privateDir(t)
	native := filepath.Join(dir, "native")
	if e := os.Mkdir(native, 0700); e != nil {
		t.Fatal(e)
	}
	sealed(t, filepath.Join(native, ".execution-retention-floor"), retention.NativeFloor{Version: 1, Permit: p})
	args := append(arguments(t, cp, s, rt), "--native", native)
	if r := Check(t.Context(), args, testProduct("arc-native")); r.Status != Hold {
		t.Fatalf("historical floor treated as current pending intent: %+v", r)
	}
	cp.Control.Visible.Height = 3500
	rt.Control = cp.Control
	s.Committed = cp.Control.Visible
	s.Parent = cp.Control.Visible
	args = append(arguments(t, cp, s, rt), "--native", native)
	if r := Check(t.Context(), args, testProduct("arc-native")); r.Status != Fail {
		t.Fatal("floor above public checkpoint accepted")
	}
}
func TestMissingUninitializedAndOptionalMarkers(t *testing.T) {
	dir := privateDir(t)
	for _, path := range []string{filepath.Join(dir, "absent"), dir} {
		r := Check(t.Context(), []string{"--spool", path, "--native", path}, testProduct("arc-native"))
		if r.Status != Hold || !finding(r, "source.store", "store_missing_or_uninitialized") {
			t.Fatalf("%+v", r)
		}
	}
	if _, e := os.Stat(filepath.Join(dir, "absent")); !os.IsNotExist(e) {
		t.Fatal("missing path initialized")
	}
	r := Check(t.Context(), []string{"--native", dir}, testProduct("stablenet-native"))
	if !finding(r, "native.maintenance", "store_missing_or_uninitialized") {
		t.Fatalf("missing chaindata called absent optional marker: %+v", r)
	}
}
func TestStrictArtifactsRedactionBoundsAndUnsafeLinks(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "private-key-postgres-user-secret-token")
	for _, b := range []string{`{`, `{}`, `null`, `{"Control":null}`, `{"secret":"postgres://user:password@host/?token=secret"}`, `{"Control":{},"control":{}}`, `{"Control":{"Generation":"HOLD"}}`, `{"Control":{"Generation":null}}`, `{} {}`, strings.Repeat("[", 34) + strings.Repeat("]", 34), strings.Repeat(" ", (1<<20)+1)} {
		if e := os.WriteFile(path, []byte(b), 0600); e != nil {
			t.Fatal(e)
		}
		var out bytes.Buffer
		if code := Run(t.Context(), []string{"--checkpoint-artifact", path}, &out, testProduct("arc-native")); code != 1 {
			t.Fatalf("malformed accepted: %.100s: %s", b, out.String())
		}
		if strings.Contains(out.String(), "password") || strings.Contains(out.String(), "secret") || strings.Contains(out.String(), dir) {
			t.Fatal("raw input leaked")
		}
	}
	write(t, path, observationsControl(t))
	link := filepath.Join(dir, "link")
	if e := os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	parent := filepath.Join(dir, "parent")
	if e := os.Symlink(dir, parent); e != nil {
		t.Fatal(e)
	}
	hard := filepath.Join(dir, "hard")
	if e := os.Link(path, hard); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{link, hard, filepath.Join(parent, filepath.Base(path)), dir, filepath.Join(dir, "missing")} {
		var out bytes.Buffer
		if Run(t.Context(), []string{"--checkpoint-artifact", p}, &out, testProduct("arc-native")) != 1 {
			t.Fatalf("unsafe selected artifact accepted: %s", p)
		}
	}
}
func observationsControl(t *testing.T) executionops.CheckpointInspection {
	t.Helper()
	cp, _, _ := observations()
	return cp
}

type badWriter struct{ short bool }

func (w badWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, errors.New("token=private")
}
func TestCancellationCLIAndOutputFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out bytes.Buffer
	if Run(ctx, nil, &out, testProduct("arc-native")) != 1 || !strings.Contains(out.String(), "cancelled_or_deadline") {
		t.Fatal(out.String())
	}
	for _, args := range [][]string{{"--native"}, {"--native", "x", "--native", "y"}, {"--config", "secret"}, {"--native", strings.Repeat("x", 4097)}} {
		out.Reset()
		if Run(t.Context(), args, &out, testProduct("arc-native")) != 1 || !strings.Contains(out.String(), "invalid_arguments") {
			t.Fatal(out.String())
		}
	}
	if Run(t.Context(), nil, io.Discard, Product{Name: "evm"}) != 1 {
		t.Fatal("unsupported profile")
	}
	for _, w := range []badWriter{{}, {true}} {
		if Run(t.Context(), nil, w, testProduct("arc-native")) != 1 {
			t.Fatal("output failure accepted")
		}
	}
}
func TestActualDiagnosticCLI(t *testing.T) {
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	for _, profile := range []string{"arc", "stablenet"} {
		binary := filepath.Join(privateDir(t), "diagnose")
		build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./x/"+profile+"/cmd/execution-diagnose")
		build.Dir = root
		if b, e := build.CombinedOutput(); e != nil {
			t.Fatalf("build: %v %s", e, b)
		}
		cmd := exec.CommandContext(t.Context(), binary)
		b, e := cmd.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(e, &exit) || exit.ExitCode() != 2 {
			t.Fatalf("actual exit: %v %s", e, b)
		}
		var r Report
		if json.Unmarshal(b, &r) != nil || r.Status != Hold || r.Authority != "not_authorized" {
			t.Fatal(string(b))
		}
		cp, src, rt := observations()
		args := arguments(t, cp, src, rt)
		dir := privateDir(t)
		native := filepath.Join(dir, "native")
		if e = os.Mkdir(native, 0700); e != nil {
			t.Fatal(e)
		}
		if profile == "stablenet" {
			if e = os.MkdirAll(filepath.Join(native, "indexer-stablenet", "chaindata"), 0700); e != nil {
				t.Fatal(e)
			}
		}
		source := filepath.Join(dir, "source")
		store, err := spool.Open(t.Context(), spool.Options{Path: source, Chain: src.Chain, Anchor: src.Anchor, MaxBytes: 1 << 20, MaxRecords: 8})
		if err != nil {
			t.Fatal(err)
		}
		if err = store.Close(); err != nil {
			t.Fatal(err)
		}
		args = append(args, "--native", native, "--spool", source)
		cmd = exec.CommandContext(t.Context(), binary, args...)
		first, err := cmd.CombinedOutput()
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || !bytes.Contains(first, []byte("compatible_observation")) || !bytes.Contains(first, []byte("optional_marker_absent")) || !bytes.Contains(first, []byte("metadata_observed_payloads_uninspected")) {
			t.Fatalf("actual compatible exports: %v %s", err, first)
		}
		cmd = exec.CommandContext(t.Context(), binary, args...)
		second, err := cmd.CombinedOutput()
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || !bytes.Equal(first, second) {
			t.Fatal("actual export report nondeterministic")
		}
		cmd = exec.CommandContext(t.Context(), binary, "--dsn", "postgres://user:secret@host")
		b, e = cmd.CombinedOutput()
		if !errors.As(e, &exit) || exit.ExitCode() != 1 || bytes.Contains(b, []byte("secret")) {
			t.Fatalf("actual failure: %v %s", e, b)
		}
	}
}

func testProduct(profile string) Product {
	return Product{Name: profile, InspectNative: func(ctx context.Context, path string) (retention.NativeMetadata, error) {
		return retention.InspectNativeMetadata(ctx, path, testLayout(path, profile))
	}, ChainAllowed: func(c execwire.Chain) bool { return profile != "arc-native" || c.ID == "5042" || c.ID == "5042002" }}
}

func TestChangedNativeObservationAndRedactedIOHold(t *testing.T) {
	calls := 0
	product := testProduct("arc-native")
	product.InspectNative = func(context.Context, string) (retention.NativeMetadata, error) {
		calls++
		v := retention.NativeMetadata{StorePresent: true}
		if calls > 1 {
			v.Floor = &retention.NativeFloor{Version: 1, Permit: permit()}
		}
		return v, nil
	}
	r := Check(t.Context(), []string{"--native", "/explicit/unused-by-mock"}, product)
	if r.Status != Hold || !finding(r, "native.resample", "changed_during_read") {
		t.Fatalf("%+v", r)
	}
	product.InspectNative = func(context.Context, string) (retention.NativeMetadata, error) {
		return retention.NativeMetadata{}, errors.New("postgres://user:password@host/?token=private")
	}
	var out bytes.Buffer
	if Run(t.Context(), []string{"--native", "/secret/path"}, &out, product) != 2 || strings.Contains(out.String(), "password") || strings.Contains(out.String(), "/secret") {
		t.Fatal(out.String())
	}
}
func TestNativeSwapMissingDirectoryStillChecksKnownFence(t *testing.T) {
	dir := privateDir(t)
	native := filepath.Join(dir, "native")
	p := permit()
	marker := filepath.Join(dir, ".native.execution-retention")
	sealed(t, marker, retention.NativeFence{Version: 1, Permit: p, Phase: "swap", Original: retention.FileIdentity{Inode: 1}, Replacement: retention.FileIdentity{Inode: 2}})
	r := Check(t.Context(), []string{"--native", native}, testProduct("arc-native"))
	if r.Status != Hold || !finding(r, "native.fence", "maintenance_fence_present") {
		t.Fatalf("missing native hid pending fence: %+v", r)
	}
	p.Request.Chain.ID = "1"
	sealed(t, marker, retention.NativeFence{Version: 1, Permit: p, Phase: "swap", Original: retention.FileIdentity{Inode: 1}, Replacement: retention.FileIdentity{Inode: 2}})
	if r = Check(t.Context(), []string{"--native", native}, testProduct("arc-native")); r.Status != Fail {
		t.Fatalf("unknown native hid known foreign fence: %+v", r)
	}
}
func TestNativeHeaderParentDoesNotClaimSourceGeneration(t *testing.T) {
	cp, s, rt := observations()
	cp.Control.Generation = 2
	cp.Control.Visible.Generation = 2
	rt.Control = cp.Control
	s.Committed = cp.Control.Visible
	if r := Check(t.Context(), arguments(t, cp, s, rt), testProduct("arc-native")); r.Status != Hold || !finding(r, "checkpoint.source", "compatible_observation") {
		t.Fatalf("header substituted generation authority: %+v", r)
	}
}
func TestFindingBoundIsExplicitFailure(t *testing.T) {
	r := Report{Status: Pass}
	for range 130 {
		r.add("bounded", Hold, "unknown")
	}
	if r.Status != Fail || len(r.Findings) != 128 || r.Findings[127].Code != "finding_limit_exceeded" {
		t.Fatal(r)
	}
}

type cancelWriter struct{ cancel context.CancelFunc }

func (w cancelWriter) Write(p []byte) (int, error) { w.cancel(); return len(p), nil }
func TestCancellationDuringOutputCannotSucceed(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if Run(ctx, nil, cancelWriter{cancel}, testProduct("arc-native")) != 1 {
		t.Fatal("canceled output returned successful observation")
	}
}

func TestReportContainsValidatedMetadataWithoutOpaqueStrings(t *testing.T) {
	cp, s, rt := observations()
	cp.Scope = "postgres://user:private@host/?token=secret"
	rt.Scope = cp.Scope
	r := Check(t.Context(), arguments(t, cp, s, rt), testProduct("arc-native"))
	if len(r.Observations) != 3 {
		t.Fatalf("typed observed metadata missing: %+v", r)
	}
	var public *Observation
	for i := range r.Observations {
		if r.Observations[i].Kind == "checkpoint.public" {
			public = &r.Observations[i]
		}
	}
	if public == nil || public.Visible == nil || *public.Visible != cp.Control.Visible || public.Root == nil || *public.Root != cp.Control.Root || public.Generation == nil || *public.Generation != cp.Control.Generation || !strings.Contains(public.Basis, "artifact") {
		t.Fatalf("public metadata unbound or fabricated: %+v", public)
	}
	b, e := json.Marshal(r)
	if e != nil || bytes.Contains(b, []byte("private")) || bytes.Contains(b, []byte("secret")) {
		t.Fatal("opaque value leaked")
	}
	s.HeaderAvailable = false
	s.StateAvailable = false
	s.Parent = execwire.Ref{}
	s.ParentRoot = execwire.Hash{}
	r = Check(t.Context(), arguments(t, cp, s, rt), testProduct("arc-native"))
	for _, o := range r.Observations {
		if o.Kind == "source.artifact" && (o.Parent != nil || o.Root != nil) {
			t.Fatal("unavailable parent fabricated")
		}
	}
}

func testLayout(path, profile string) retention.NativeLayout {
	if profile == "arc-native" {
		return retention.NativeLayout{Fence: filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".execution-retention"), Floor: filepath.Join(path, ".execution-retention-floor"), Detached: filepath.Join(path, ".execution-retention-pending"), AllowMissingStore: true}
	}
	return retention.NativeLayout{Fence: filepath.Join(path, ".execution-retention"), Floor: filepath.Join(path, "indexer-stablenet", "chaindata", ".execution-retention-floor")}
}
func TestDeliberateMissingOperatorPathsStayHold(t *testing.T) {
	for _, profile := range []string{"arc-native", "stablenet-native"} {
		for _, flag := range []string{"--native", "--spool", "--source-artifact", "--checkpoint-artifact", "--retention-artifact"} {
			for _, value := range []string{"HOLD", ""} {
				var out bytes.Buffer
				if Run(t.Context(), []string{flag, value}, &out, testProduct(profile)) != 2 || strings.Contains(out.String(), "invalid_arguments") {
					t.Fatalf("%s %s placeholder was malformed: %s", profile, flag, out.String())
				}
			}
		}
	}
	cp, s, rt := observations()
	s.ParentRoot[0]++
	args := append(arguments(t, cp, s, rt), "--native", "HOLD")
	if r := Check(t.Context(), args, testProduct("arc-native")); r.Status != Fail {
		t.Fatal("operator HOLD hid independent root conflict")
	}
}

func TestMissingExportFieldsNeverFabricatePendingAbsenceOrGenesis(t *testing.T) {
	cp, s, rt := observations()
	dir := privateDir(t)
	for _, item := range []struct {
		flag   string
		value  any
		remove string
	}{
		{"--source-artifact", s, "TransitionPending"}, {"--source-artifact", s, "MaintenancePending"}, {"--source-artifact", s, "HeaderAvailable"},
		{"--retention-artifact", rt, "Pending"}, {"--checkpoint-artifact", cp, "Bootstrap"},
	} {
		b, e := json.Marshal(item.value)
		if e != nil {
			t.Fatal(e)
		}
		var object map[string]json.RawMessage
		if e = json.Unmarshal(b, &object); e != nil {
			t.Fatal(e)
		}
		delete(object, item.remove)
		path := write(t, filepath.Join(dir, "partial"), object)
		r := Check(t.Context(), []string{item.flag, path}, testProduct("arc-native"))
		if r.Status != Fail || len(r.Observations) != 0 || !finding(r, "source.artifact", "invalid_artifact_schema") && !finding(r, "checkpoint.artifact", "invalid_artifact_schema") && !finding(r, "retention.artifact", "invalid_artifact_schema") {
			t.Fatalf("partial export fabricated observation: %+v", r)
		}
	}
	b, e := json.Marshal(s)
	if e != nil {
		t.Fatal(e)
	}
	var object map[string]json.RawMessage
	if e = json.Unmarshal(b, &object); e != nil {
		t.Fatal(e)
	}
	var anchor map[string]json.RawMessage
	if e = json.Unmarshal(object["Anchor"], &anchor); e != nil {
		t.Fatal(e)
	}
	delete(anchor, "Height")
	object["Anchor"], e = json.Marshal(anchor)
	if e != nil {
		t.Fatal(e)
	}
	r := Check(t.Context(), []string{"--source-artifact", write(t, filepath.Join(dir, "missing-height"), object)}, testProduct("arc-native"))
	if r.Status != Fail || len(r.Observations) != 0 {
		t.Fatal("missing height became observed genesis")
	}
}
func TestUnreadSourceMarkersDoNotClaimPendingAbsence(t *testing.T) {
	_, s, _ := observations()
	dir := privateDir(t)
	path := filepath.Join(dir, "source")
	store, e := spool.Open(t.Context(), spool.Options{Path: path, Chain: s.Chain, Anchor: s.Anchor, MaxBytes: 1 << 20, MaxRecords: 8})
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Close(); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(path, ".exec-spool-transition"), []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	r := Check(t.Context(), []string{"--spool", path}, testProduct("arc-native"))
	if r.Status != Fail || len(r.Observations) != 1 || r.Observations[0].TransitionPending != nil || r.Observations[0].MaintenancePending != nil {
		t.Fatalf("unknown marker became absent: %+v", r)
	}
}

func TestKnownCorruptionDominatesChangedAndMissing(t *testing.T) {
	product := testProduct("arc-native")
	product.InspectNative = func(context.Context, string) (retention.NativeMetadata, error) {
		return retention.NativeMetadata{}, errors.Join(metadata.ErrUnsafe, metadata.ErrChanged, os.ErrNotExist)
	}
	var out bytes.Buffer
	if code := Run(t.Context(), []string{"--native", "/explicit/mock-only"}, &out, product); code != 1 {
		t.Fatalf("known unsafe metadata must FAIL despite changed/missing HOLD, got %d: %s", code, out.String())
	}
}

func TestSupportedPartialPreparedBootstrapIsHoldButKnownInvalidDataFail(t *testing.T) {
	cp, s, rt := observations()
	cp.Bootstrap.Coverage = "partial"
	cp.Bootstrap.Mode = "prepared"
	cp.Bootstrap.Digest = nil
	r := Check(t.Context(), arguments(t, cp, s, rt), testProduct("arc-native"))
	if r.Status != Hold || !finding(r, "checkpoint.bootstrap", "partial_baseline_metadata") {
		t.Fatalf("supported partial export treated as corruption: %+v", r)
	}
	s.ParentRoot[0]++
	if r = Check(t.Context(), arguments(t, cp, s, rt), testProduct("arc-native")); r.Status != Fail {
		t.Fatal("partial HOLD hid known root conflict")
	}
	for _, change := range []func(*executionops.CheckpointInspection){
		func(c *executionops.CheckpointInspection) { c.Bootstrap.Digest = make([]byte, 32) },
		func(c *executionops.CheckpointInspection) { c.Control.PartitionBlocks = 1_000_000_001 },
		func(c *executionops.CheckpointInspection) { c.Bootstrap.Mode = "prepared" },
		func(c *executionops.CheckpointInspection) {
			c.Control.Anchor.Height = 1
			c.Control.Anchor.Hash = execwire.Hash{8}
			c.Control.Visible = c.Control.Anchor
		},
	} {
		cp, s, rt = observations()
		change(&cp)
		rt.Control = cp.Control
		r = Check(t.Context(), arguments(t, cp, s, rt), testProduct("arc-native"))
		if r.Status != Fail {
			t.Fatalf("known invalid bootstrap/control accepted: %+v", r)
		}
	}
}
