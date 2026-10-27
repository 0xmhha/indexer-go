//go:build arc_native && (darwin || linux)

package nativeops

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
)

// Run by the Arc CI job after Rust exports owned native-library MDBX/RocksDB/
// static-file fixtures. No node starts and this test never skips. PostgreSQL
// receipt authority is separately covered by the fsync-on coordinator tests.
func TestArcNativeLibraryMatchedBackupAndRestore(t *testing.T) {
	fixture := os.Getenv("INDEXER_ARC_NATIVE_FIXTURE_DIR")
	binary := os.Getenv("INDEXER_ARC_MAINTENANCE_BINARY")
	if !filepath.IsAbs(fixture) || !filepath.IsAbs(binary) {
		t.Fatal("owned native fixture and maintenance binary required")
	}
	var m struct {
		Chain      execwire.Chain
		Anchor     execwire.Ref
		AnchorRoot execwire.Hash
		Head       execwire.Ref
		HeadRoot   execwire.Hash
		Refs       []execwire.Ref
	}
	data, err := os.ReadFile(filepath.Join(fixture, "fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(root, "native")
	err = filepath.WalkDir(filepath.Join(fixture, "native"), func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(filepath.Join(fixture, "native"), path)
		if e != nil {
			return e
		}
		target := filepath.Join(native, rel)
		if d.IsDir() {
			return os.Mkdir(target, 0700)
		}
		in, e := os.Open(path)
		if e != nil {
			return e
		}
		defer func() { _ = in.Close() }()
		out, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		// A dense owned copy makes physical reclaim observable independently
		// of filesystem clone/sparse-copy optimizations.
		_, e = io.Copy(out, struct{ io.Reader }{in})
		syncErr := out.Sync()
		closeErr := out.Close()
		if syncErr != nil {
			return syncErr
		}
		if e != nil {
			return e
		}
		return closeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	allocated := func() int64 {
		info, e := os.Stat(filepath.Join(native, "db", "mdbx.dat"))
		if e != nil {
			t.Fatal(e)
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatal("native Unix allocation metadata required")
		}
		return st.Blocks
	}
	beforeBlocks := allocated()
	c := Config{NativeExecutable: binary, NativeDataDir: native, SpoolDir: filepath.Join(root, "source"), Chain: m.Chain, Anchor: m.Anchor, AnchorRoot: m.AnchorRoot, ReserveBytes: 1}
	source, _, err := options(c)
	if err != nil {
		t.Fatal(err)
	}
	s, err := spool.Open(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	parent := m.Anchor.Hash
	for _, ref := range m.Refs {
		result := execwire.Result{Ref: ref, Parent: parent, StateRoot: m.HeadRoot, ReceiptsRoot: execwire.Hash{4}, BlockRLP: []byte{1}, ReceiptsRLP: []byte{2}, Steps: []execwire.Step{{Ordinal: 0, Cause: execwire.Finalize, TxIndex: -1}}}
		entry, e := s.Append(t.Context(), result)
		if e != nil {
			t.Fatal(e)
		}
		if ref.Height <= 132 {
			if e = s.Acknowledge(t.Context(), execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: 1, Chain: m.Chain, Ref: ref, Digest: entry.Digest}}); e != nil {
				t.Fatal(e)
			}
		}
		parent = ref.Hash
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := PlanRetention(t.Context(), c, 129)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := os.ReadFile(filepath.Join(fixture, "genesis.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	p := retention.Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash(sha256.Sum256(baseline)), Token: execwire.Hash{6}, Request: request, Visible: request.ACK, Root: m.HeadRoot}
	c.BackupOutput = filepath.Join(root, "matched.json")
	c.IndexerReceipt = filepath.Join(root, "db-receipt.json")
	c.IndexerArtifact = filepath.Join(root, "db.backup")
	externalBytes := []byte("externally authenticated DB fixture; SQL authority is tested separately against PostgreSQL")
	if err = os.WriteFile(c.IndexerArtifact, externalBytes, 0600); err != nil {
		t.Fatal(err)
	}
	pd, _ := retention.Digest(p)
	external := retention.IndexerBackup{Version: 1, PermitDigest: pd, ArtifactDigest: execwire.Hash(sha256.Sum256(externalBytes)), PublicationDigest: execwire.Hash{9}, DatabaseIdentity: "owned/schema", SnapshotIdentity: "native-library-fixture", WALBoundary: "0/123"}
	if err = retention.WriteArtifact(c.IndexerReceipt, external); err != nil {
		t.Fatal(err)
	}
	receipt, err := applyRetention(t.Context(), c, p)
	if err != nil {
		var manifest nativestore.MatchedBackup
		if retention.ReadArtifact(c.BackupOutput, &manifest) == nil {
			for _, file := range manifest.Files {
				f, e := os.Open(filepath.Join(root, manifest.ComponentsDir, filepath.FromSlash(file.Path)))
				if e != nil {
					t.Log("changed component", file.Path)
					continue
				}
				h := sha256.New()
				_, e = io.Copy(h, f)
				_ = f.Close()
				var actual execwire.Hash
				copy(actual[:], h.Sum(nil))
				if e != nil || actual != file.Digest {
					t.Log("changed component", file.Path)
				}
			}
		}
		t.Fatal("real native matched publication", err)
	}
	if info, e := os.Stat(filepath.Join(native, "db", "mdbx.dat")); e != nil {
		t.Fatal(e)
	} else {
		t.Logf("owned native fixture MDBX geometry after compact: %d bytes", info.Size())
	}
	if afterBlocks := allocated(); afterBlocks >= beforeBlocks {
		t.Fatalf("supervised production path did not physically compact MDBX: before=%d after=%d", beforeBlocks, afterBlocks)
	}
	if err = receipt.Match(p); err != nil {
		t.Fatal(err)
	}
	c.RestoreManifest = c.BackupOutput
	for range 2 {
		admitted, e := admitRestore(t.Context(), c, p)
		if e != nil || admitted != receipt {
			t.Fatal("repeat real native/source/indexer admission", admitted, e)
		}
	}
	components := c.BackupOutput + ".components"
	for _, relative := range []string{"native/.arc-retention-checkpoint.ndjson", "source/.exec-spool-state", "indexer/receipt.json"} {
		path := filepath.Join(components, filepath.FromSlash(relative))
		original, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, []byte("corrupt component"), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = admitRestore(t.Context(), c, p); e == nil {
			t.Fatal("corrupt real component admitted", relative)
		}
		if e = os.WriteFile(path, original, 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.Rename(path, path+".held"); e != nil {
			t.Fatal(e)
		}
		if _, e = admitRestore(t.Context(), c, p); e == nil {
			t.Fatal("missing real component admitted", relative)
		}
		if e = os.Rename(path+".held", path); e != nil {
			t.Fatal(e)
		}
	}
	lock := filepath.Join(components, "native", "db", "mdbx.lck")
	originalLock, e := os.ReadFile(lock)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(lock); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(c.IndexerArtifact, lock); e != nil {
		t.Fatal(e)
	}
	if _, e = admitRestore(t.Context(), c, p); e == nil {
		t.Fatal("linked MDBX coordination file admitted")
	}
	if e = os.Remove(lock); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(lock, originalLock, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = admitRestore(t.Context(), c, p); e != nil {
		t.Fatal("same-permit real native recovery", e)
	}
	if err = nativestore.CheckStart(filepath.Join(c.BackupOutput+".components", "native")); err != nativestore.ErrPending {
		t.Fatal("native archive must remain fenced", err)
	}
}
