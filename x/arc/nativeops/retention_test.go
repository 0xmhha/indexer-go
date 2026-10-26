package nativeops

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// An unbound fixture command deterministically exits zero after EOF even when
// its owner's context is already canceled, reproducing the Wait/cancel race.
func TestNativeSuccessfulExitStillReportsCanceledContext(t *testing.T) {
	c, p := nativeFixture(t)
	work, e := os.MkdirTemp(filepath.Dir(c.NativeDataDir), ".arc-maintenance-")
	if e != nil {
		t.Fatal(e)
	}
	info, e := os.Lstat(work)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.Command(c.NativeExecutable, "--datadir", c.NativeDataDir, "--operation", "hold", "--scratch-dir", work)
	input, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	output, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	h := &held{input: input, output: output, cmd: cmd, life: &childLifetime{ctx: ctx, cancel: cancel, path: work, info: info}}
	defer func() { _ = h.Close() }()
	if e = json.NewEncoder(input).Encode(p.Request); e != nil {
		t.Fatal(e)
	}
	if _, e = bufio.NewReader(output).ReadBytes('\n'); e != nil {
		t.Fatal(e)
	}
	cancel()
	if e = h.Close(); !errors.Is(e, context.Canceled) {
		t.Fatalf("zero native exit must retain owner cancellation: %v", e)
	}
	if _, e = os.Stat(work); !os.IsNotExist(e) {
		t.Fatal("joined child scratch remains:", e)
	}
}

func nativeFixture(t *testing.T) (Config, retention.Permit) {
	t.Helper()
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	root, e := filepath.EvalSymlinks(root)
	if e != nil {
		t.Fatal(e)
	}
	native := filepath.Join(root, "native")
	if e = os.Mkdir(native, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"db", "rocksdb", "static_files"} {
		if e = os.Mkdir(filepath.Join(native, name), 0700); e != nil {
			t.Fatal(e)
		}
	}
	binary := filepath.Join(root, "adapter")
	quoted := "'" + strings.ReplaceAll(os.Args[0], "'", "'\"'\"'") + "'"
	script := "#!/bin/sh\nGORACE=atexit_sleep_ms=0 exec " + quoted + " -test.run=^TestNativeHelper$ -- --native-test-helper \"$@\"\n"
	if e = os.WriteFile(binary, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	chain := execwire.Chain{ID: "5042002", Genesis: execwire.Hash{1}}
	anchor := execwire.Ref{Generation: 1, Hash: chain.Genesis}
	ack := execwire.Ref{Generation: 1, Height: 132, Hash: execwire.Hash{133}}
	r := retention.Request{Version: 1, Chain: chain, Anchor: anchor, ACK: ack, Tail: ack, Head: execwire.Ref{Height: 132, Hash: ack.Hash}, HeadRoot: execwire.Hash{3}, SourceDigest: execwire.Hash{4}, KeepBlocks: 129, Through: 3}
	p := retention.Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash{5}, Token: execwire.Hash{6}, Request: r, Visible: ack, Root: r.HeadRoot}
	return Config{NativeExecutable: binary, NativeDataDir: native, SpoolDir: filepath.Join(root, "source"), Chain: chain, Anchor: anchor, AnchorRoot: r.HeadRoot, ReserveBytes: 1}, p
}
func TestNativeOwnedHoldCancellationAndStrictReplies(t *testing.T) {
	c, p := nativeFixture(t)
	n := Native{Executable: c.NativeExecutable, Chain: c.Chain, Timeout: 5 * time.Second}
	h, e := n.Lock(t.Context(), c.NativeDataDir, p.Request)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Close(); e != nil {
		t.Fatal(e)
	}
	if e = h.Close(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	h, e = n.Lock(ctx, c.NativeDataDir, p.Request)
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	if e = h.Close(); e == nil {
		t.Fatal("cancelled native child reported success")
	}
	for _, mode := range []string{"bad", "large", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			if e := os.WriteFile(filepath.Join(c.NativeDataDir, "MODE"), []byte(mode), 0600); e != nil {
				t.Fatal(e)
			}
			call, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			h, e := n.Lock(call, c.NativeDataDir, p.Request)
			if h != nil {
				_ = h.Close()
			}
			if e == nil {
				t.Fatal("invalid/blocked child accepted")
			}
		})
	}
}
func TestNativeGuardRejectsFenceAndPathOverrides(t *testing.T) {
	c, _ := nativeFixture(t)
	for _, args := range [][]string{{"--datadir", "/elsewhere"}, {"--chain=arc-mainnet"}, {"--config", "other"}, {"--"}} {
		if e := RunGuard(t.Context(), c.NativeExecutable, c.NativeDataDir, "arc-testnet", args, nil, io.Discard, io.Discard); e == nil {
			t.Fatal("override accepted")
		}
	}
	marker := filepath.Join(filepath.Dir(c.NativeDataDir), "."+filepath.Base(c.NativeDataDir)+".execution-retention")
	if e := os.WriteFile(marker, []byte("pending"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := RunGuard(t.Context(), c.NativeExecutable, c.NativeDataDir, "arc-testnet", nil, nil, io.Discard, io.Discard); !errors.Is(e, nativestore.ErrPending) {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(c.NativeDataDir, "guard-ran")); !os.IsNotExist(e) {
		t.Fatal("guard launched through fence")
	}
	if e := os.Remove(marker); e != nil {
		t.Fatal(e)
	}
	if e := RunGuard(t.Context(), c.NativeExecutable, c.NativeDataDir, "arc-testnet", nil, nil, io.Discard, io.Discard); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(c.NativeDataDir, "guard-ran")); e != nil {
		t.Fatal(e)
	}
}
func TestArcRetentionSourceCopyCompletionAndExactRelease(t *testing.T) {
	c, _ := nativeFixture(t)
	source, node, e := options(c)
	if e != nil {
		t.Fatal(e)
	}
	s, e := spool.Open(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	makeResult := func(ref execwire.Ref, parent execwire.Hash) execwire.Result {
		return execwire.Result{Ref: ref, Parent: parent, StateRoot: execwire.Hash{3}, ReceiptsRoot: execwire.Hash{4}, BlockRLP: []byte{1}, ReceiptsRLP: []byte{2}, Steps: []execwire.Step{{Ordinal: 0, Cause: execwire.Finalize, TxIndex: -1}}}
	}
	parent := c.Anchor.Hash
	for n := uint64(1); n <= 4; n++ {
		ref := execwire.Ref{Generation: 1, Height: n, Hash: execwire.Hash{byte(n + 1)}}
		if _, e = s.Append(t.Context(), makeResult(ref, parent)); e != nil {
			t.Fatal(e)
		}
		parent = ref.Hash
	}
	base := c.Anchor
	base.Generation = 2
	if e = s.Rebind(t.Context(), base); e != nil {
		t.Fatal(e)
	}
	parent = base.Hash
	for n := uint64(1); n <= 132; n++ {
		ref := execwire.Ref{Generation: 2, Height: n, Hash: execwire.Hash{byte(n + 1)}}
		entry, e := s.Append(t.Context(), makeResult(ref, parent))
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Acknowledge(t.Context(), execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: 1, Chain: c.Chain, Ref: ref, Digest: entry.Digest}}); e != nil {
			t.Fatal(e)
		}
		parent = ref.Hash
	}
	head := execwire.Ref{Height: 132, Hash: parent}
	r, e := s.RetentionRequest(t.Context(), head, execwire.Hash{3}, 129)
	if e != nil {
		t.Fatal(e)
	}
	p := retention.Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash{5}, Token: execwire.Hash{6}, Request: r, Visible: r.ACK, Root: r.HeadRoot}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(node.DataDir, "EXPIRED"), []byte("old payload"), 0600); e != nil {
		t.Fatal(e)
	}
	c.BackupOutput = filepath.Join(filepath.Dir(c.NativeDataDir), "matched-backup.json")
	c.IndexerArtifact = filepath.Join(filepath.Dir(c.NativeDataDir), "database.backup")
	c.IndexerReceipt = filepath.Join(filepath.Dir(c.NativeDataDir), "database-receipt.json")
	artifact := []byte("external PostgreSQL backup fixture; production requires authenticated DB/WAL receipt")
	if e = os.WriteFile(c.IndexerArtifact, artifact, 0600); e != nil {
		t.Fatal(e)
	}
	pdBackup, _ := retention.Digest(p)
	external := retention.IndexerBackup{Version: 1, PermitDigest: pdBackup, ArtifactDigest: execwire.Hash(sha256.Sum256(artifact)), PublicationDigest: execwire.Hash{9}, DatabaseIdentity: "fixture/schema", SnapshotIdentity: "fixture-snapshot", WALBoundary: "0/123"}
	if e = retention.WriteArtifact(c.IndexerReceipt, external); e != nil {
		t.Fatal(e)
	}
	receipt, e := applyRetention(t.Context(), c, p)
	if e != nil {
		t.Fatal(e)
	}
	if e = receipt.Match(p); e != nil || receipt.Files != 3 {
		t.Fatal("native/source completion:", receipt, e)
	}
	if e = nativestore.CheckStart(node.DataDir); !errors.Is(e, nativestore.ErrPending) {
		t.Fatal("node fence released early:", e)
	}
	repeat, e := applyRetention(t.Context(), c, p)
	if e != nil || repeat != receipt {
		t.Fatal("completion recovery:", repeat, e)
	}
	c.RestoreManifest = c.BackupOutput
	admitted, e := admitRestore(t.Context(), c, p)
	if e != nil || admitted != receipt {
		t.Fatal("matched source/native/indexer restore admission", admitted, e)
	}
	wrongPermit := p
	wrongPermit.Visible.Height = p.Request.Through - 1
	if _, e = admitRestore(t.Context(), c, wrongPermit); e == nil {
		t.Fatal("old restored consumer accepted")
	}
	if e = nativestore.CheckStart(node.DataDir); !errors.Is(e, nativestore.ErrPending) {
		t.Fatal("restore admission released live fences", e)
	}
	c.RestoreManifest = ""
	pd, _ := retention.Digest(p)
	rd, _ := retention.Digest(receipt)
	release := retention.Release{Version: 1, PermitDigest: pd, ReceiptDigest: rd}
	wrong := release
	wrong.ReceiptDigest[0]++
	if e = ReleaseRetention(t.Context(), c, p, wrong); e == nil {
		t.Fatal("wrong release accepted")
	}
	if e = ReleaseRetention(t.Context(), c, p, release); e != nil {
		t.Fatal(e)
	}
	if e = nativestore.CheckStart(node.DataDir); e != nil {
		t.Fatal(e)
	}
	reopened, e := spool.Open(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	if e = reopened.Close(); e != nil {
		t.Fatal(e)
	}
}
func TestArcRetentionCLIRequiresPrivateConfigAndLiveSupervisor(t *testing.T) {
	c, p := nativeFixture(t)
	path := filepath.Join(filepath.Dir(c.NativeDataDir), "config.json")
	if e := retention.WriteArtifact(path, c); e != nil {
		t.Fatal(e)
	}
	permit := filepath.Join(filepath.Dir(path), "permit.json")
	if e := retention.WriteArtifact(permit, p); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := RunRetention(t.Context(), CLI{Action: "apply", Config: path, Permit: permit}, &out); e == nil || out.Len() != 0 {
		t.Fatal("unsupervised apply accepted:", e)
	}
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadConfig(path); e == nil {
		t.Fatal("public config accepted")
	}
	if e := os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
}

func TestNativeGuardCancellationJoinsOwnedChild(t *testing.T) {
	c, _ := nativeFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()
	done := make(chan error, 1)
	go func() {
		e := RunGuard(ctx, c.NativeExecutable, c.NativeDataDir, "arc-testnet", []string{"--wait"}, nil, writer, io.Discard)
		_ = writer.CloseWithError(e)
		done <- e
	}()
	line, e := bufio.NewReader(reader).ReadString('\n')
	if e != nil || line != "ready\n" {
		t.Fatal("guard readiness:", line, e)
	}
	cancel()
	if e = <-done; !errors.Is(e, context.Canceled) {
		t.Fatal("guard cancel:", e)
	}
	if _, e = os.Stat(filepath.Join(c.NativeDataDir, "guard-stopped")); e != nil {
		t.Fatal("owned child did not shut down gracefully:", e)
	}
}

func TestNativeScratchReplacementIsPreserved(t *testing.T) {
	c, p := nativeFixture(t)
	if e := os.WriteFile(filepath.Join(c.NativeDataDir, "MODE"), []byte("replace-work"), 0600); e != nil {
		t.Fatal(e)
	}
	n := Native{Executable: c.NativeExecutable, Chain: c.Chain, Timeout: 5 * time.Second}
	h, e := n.Lock(t.Context(), c.NativeDataDir, p.Request)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Close(); e == nil {
		t.Fatal("replaced scratch was accepted")
	}
	files, e := filepath.Glob(filepath.Join(filepath.Dir(c.NativeDataDir), ".arc-maintenance-*", "KEEP"))
	if e != nil || len(files) != 1 {
		t.Fatal("replacement not preserved:", files, e)
	}
	b, e := os.ReadFile(files[0])
	if e != nil || string(b) != "replacement" {
		t.Fatal(e)
	}
}
func TestArcConfigRecognizesSealedSwapWithMissingCurrentPath(t *testing.T) {
	c, p := nativeFixture(t)
	root := filepath.Dir(c.NativeDataDir)
	if e := os.Rename(c.NativeDataDir, filepath.Join(root, "backup")); e != nil {
		t.Fatal(e)
	}
	if _, _, e := options(c); e == nil {
		t.Fatal("missing unfenced native directory accepted")
	}
	type id struct{ Device, Inode uint64 }
	data, e := json.Marshal(struct {
		Version               uint16
		Permit                retention.Permit
		Phase                 string
		Original, Replacement id
	}{1, p, "swap", id{1, 2}, id{1, 3}})
	if e != nil {
		t.Fatal(e)
	}
	seal, e := json.Marshal(struct {
		Data   json.RawMessage
		Digest execwire.Hash
	}{data, execwire.Hash(sha256.Sum256(data))})
	if e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(root, "."+filepath.Base(c.NativeDataDir)+".execution-retention")
	if e = os.WriteFile(marker, seal, 0600); e != nil {
		t.Fatal(e)
	}
	_, node, e := options(c)
	if e != nil || node.DataDir != c.NativeDataDir {
		t.Fatal("sealed rename-boundary recovery rejected:", e)
	}
	if _, e = PlanRetention(t.Context(), c, 129); !errors.Is(e, nativestore.ErrPending) {
		t.Fatal("diagnosis started through pending fence:", e)
	}
	changed := c
	changed.Anchor.Generation++
	if _, _, e = options(changed); e == nil {
		t.Fatal("foreign anchor accepted for missing-path recovery")
	}
}

// The subprocess implements only the native protocol fixture. No Python, node,
// native database or application service is started by Go package tests.
func TestNativeHelper(t *testing.T) {
	index := 0
	for i, a := range os.Args {
		if a == "--native-test-helper" {
			index = i + 1
			break
		}
	}
	if index == 0 {
		return
	}
	args := os.Args[index:]
	fail := func(e error) {
		if e != nil {
			_, _ = io.WriteString(os.Stderr, e.Error())
			os.Exit(2)
		}
	}
	value := func(flag string) string {
		for i, a := range args {
			if a == flag && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	dir := value("--datadir")
	if len(args) > 0 && args[0] == "node" {
		b, e := json.Marshal(args)
		fail(e)
		fail(os.WriteFile(filepath.Join(dir, "guard-ran"), b, 0600))
		if slices.Contains(args, "--wait") {
			signals := make(chan os.Signal, 1)
			signal.Notify(signals, syscall.SIGTERM)
			_, e = io.WriteString(os.Stdout, "ready\n")
			fail(e)
			<-signals
			signal.Stop(signals)
			fail(os.WriteFile(filepath.Join(dir, "guard-stopped"), []byte("stopped"), 0600))
		}
		os.Exit(0)
	}
	op := value("--operation")
	var raw json.RawMessage
	fail(json.NewDecoder(os.Stdin).Decode(&raw))
	mode, _ := os.ReadFile(filepath.Join(dir, "MODE"))
	if string(mode) == "replace-work" {
		work := value("--scratch-dir")
		fail(os.Rename(work, work+"-moved"))
		fail(os.Mkdir(work, 0700))
		fail(os.WriteFile(filepath.Join(work, "KEEP"), []byte("replacement"), 0600))
	}
	if string(mode) == "timeout" {
		_, e := io.Copy(io.Discard, os.Stdin)
		fail(e)
		os.Exit(0)
	}
	if string(mode) == "bad" || string(mode) == "large" {
		text := "invalid\n"
		if string(mode) == "large" {
			text = strings.Repeat("x", 70000) + "\n"
		}
		_, _ = io.WriteString(os.Stdout, text)
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	if op == "hold" || op == "inspect" {
		var r retention.Request
		fail(json.Unmarshal(raw, &r))
		h := r.Head
		if h.Hash == (execwire.Hash{}) {
			h = execwire.Ref{Height: 132, Hash: execwire.Hash{133}}
		}
		root := r.HeadRoot
		if root == (execwire.Hash{}) {
			root = execwire.Hash{3}
		}
		fail(json.NewEncoder(os.Stdout).Encode(Observation{Head: h, Root: root, AnchorRoot: execwire.Hash{3}}))
		if op == "hold" {
			_, e := io.Copy(io.Discard, os.Stdin)
			fail(e)
		}
	} else {
		switch op {
		case "prune":
			fail(os.Remove(filepath.Join(dir, "EXPIRED")))
		case "verify", "match-backup":
			if _, e := os.Stat(filepath.Join(dir, "EXPIRED")); !os.IsNotExist(e) {
				os.Exit(2)
			}
		default:
			os.Exit(2)
		}
		fail(json.NewEncoder(os.Stdout).Encode(struct{ Status string }{"native_verified"}))
	}
	os.Exit(0)
}

func TestIndexerReceiptRemainsBoundAfterSupervision(t *testing.T) {
	c, p := nativeFixture(t)
	c.IndexerArtifact = filepath.Join(filepath.Dir(c.NativeDataDir), "indexer.backup")
	c.IndexerReceipt = filepath.Join(filepath.Dir(c.NativeDataDir), "indexer.json")
	data := []byte("owned external artifact")
	if err := os.WriteFile(c.IndexerArtifact, data, 0600); err != nil {
		t.Fatal(err)
	}
	pd, _ := retention.Digest(p)
	a := retention.IndexerBackup{Version: 1, PermitDigest: pd, ArtifactDigest: execwire.Hash(sha256.Sum256(data)), PublicationDigest: execwire.Hash{9}, DatabaseIdentity: "accepted/schema", SnapshotIdentity: "accepted", WALBoundary: "0/10"}
	c.backupDigest, _ = retention.Digest(a)
	if err := retention.WriteArtifact(c.IndexerReceipt, a); err != nil {
		t.Fatal(err)
	}
	if _, err := indexerBackup(t.Context(), c, p); err != nil {
		t.Fatal(err)
	}
	b := a
	b.DatabaseIdentity = "foreign/schema"
	b.WALBoundary = "F/FFFFFFFF"
	if err := b.Validate(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(c.IndexerReceipt); err != nil {
		t.Fatal(err)
	}
	if err := retention.WriteArtifact(c.IndexerReceipt, b); err != nil {
		t.Fatal(err)
	}
	if _, err := indexerBackup(t.Context(), c, p); !errors.Is(err, retention.ErrUnsafe) {
		t.Fatalf("substituted receipt accepted after supervisor match: %v", err)
	}
}
