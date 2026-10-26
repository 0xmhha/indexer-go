package nativestore

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedPriority4NativeReclamationBackupRestore(t *testing.T) {
	t.Run("component_files_and_durable_recovery", TestMatchedBackupComponentsInterruptionRecoveryAndNoOverwrite)
	_, p := fixture(t)
	pd, err := retention.Digest(p)
	if err != nil {
		t.Fatal(err)
	}
	good := IndexerBackup{Version: 1, PermitDigest: pd, ArtifactDigest: execwire.Hash{1}, PublicationDigest: execwire.Hash{2}, DatabaseIdentity: "owned-db/schema", SnapshotIdentity: "exported-snapshot", WALBoundary: "0/123"}
	if err = good.Validate(p); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"version", "permit", "artifact", "publication", "database", "snapshot", "wal"} {
		t.Run(field, func(t *testing.T) {
			bad := good
			switch field {
			case "version":
				bad.Version++
			case "permit":
				bad.PermitDigest[0]++
			case "artifact":
				bad.ArtifactDigest = execwire.Hash{}
			case "publication":
				bad.PublicationDigest = execwire.Hash{}
			case "database":
				bad.DatabaseIdentity = ""
			case "snapshot":
				bad.SnapshotIdentity = ""
			case "wal":
				bad.WALBoundary = ""
			}
			if bad.Validate(p) == nil {
				t.Fatal("mixed/missing external indexer receipt accepted")
			}
		})
	}
	if _, err = ValidateMatchedBackup(t.Context(), t.TempDir(), p, good, p.Visible); err == nil {
		t.Fatal("native/source/indexer components are missing; backup must fail closed")
	}
	below := p.Visible
	below.Height = p.Request.Through - 1
	if _, err = ValidateMatchedBackup(t.Context(), t.TempDir(), p, good, below); err == nil {
		t.Fatal("consumer below retained state floor accepted")
	}
}

func TestMatchedBackupComponentsInterruptionRecoveryAndNoOverwrite(t *testing.T) {
	for _, phase := range []string{"backup_intent", "backup_native", "backup_source", "backup_indexer", "backup_durable", "backup_published"} {
		t.Run(phase, func(t *testing.T) {
			o, p := fixture(t)
			canonical, err := filepath.EvalSymlinks(o.DataDir)
			if err != nil {
				t.Fatal(err)
			}
			o.DataDir = canonical
			if err = Compact(t.Context(), o, p); err != nil {
				t.Fatal(err)
			}
			parent := filepath.Dir(o.DataDir)
			source := filepath.Join(parent, "source")
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "manifest"), []byte("source snapshot"), 0600); err != nil {
				t.Fatal(err)
			}
			artifact := filepath.Join(parent, "postgres.backup")
			data := []byte("externally authenticated PostgreSQL snapshot with retained WAL")
			if err := os.WriteFile(artifact, data, 0600); err != nil {
				t.Fatal(err)
			}
			pd, _ := retention.Digest(p)
			receipt := IndexerBackup{Version: 1, PermitDigest: pd, ArtifactDigest: execwire.Hash(sha256.Sum256(data)), PublicationDigest: execwire.Hash{2}, DatabaseIdentity: "owned/schema", SnapshotIdentity: "snapshot", WALBoundary: "0/123"}
			output := filepath.Join(parent, "matched.json")
			verify := func(root string) error {
				b, err := os.ReadFile(filepath.Join(root, "native", "CURRENT"))
				if err != nil || string(b) != "state and code" {
					return ErrUnsafe
				}
				return nil
			}
			interrupted := errors.New("interrupted")
			err = publishMatchedBackup(t.Context(), output, p, receipt, o.DataDir, source, artifact, verify, hooks{phase: func(at string) error {
				if at == phase {
					return interrupted
				}
				return nil
			}})
			if !errors.Is(err, interrupted) {
				t.Fatal("expected phase interruption", err)
			}
			if err = PublishMatchedBackup(t.Context(), output, p, receipt, o.DataDir, source, artifact, verify); err != nil {
				t.Fatal("same-permit recovery", err)
			}
			manifest, err := ValidateMatchedBackup(t.Context(), output, p, receipt, p.Visible)
			if err != nil || len(manifest.Files) < 4 {
				t.Fatal("matched components", manifest, err)
			}
			if err = PublishMatchedBackup(t.Context(), output, p, receipt, o.DataDir, source, artifact, verify); err != nil {
				t.Fatal("idempotent publication", err)
			}
			if err = CheckStart(filepath.Join(output+".components", "native")); !errors.Is(err, ErrPending) {
				t.Fatal("archived native pending intent must forbid activation", err)
			}
			relocated := filepath.Join(parent, "relocated")
			if err = os.Mkdir(relocated, 0700); err != nil {
				t.Fatal(err)
			}
			relocatedManifest := filepath.Join(relocated, filepath.Base(output))
			encoded, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(relocatedManifest, encoded, 0600); err != nil {
				t.Fatal(err)
			}
			relocatedComponents := relocatedManifest + ".components"
			if err = os.Mkdir(relocatedComponents, 0700); err != nil {
				t.Fatal(err)
			}
			if err = copyTreeWithOwner(t.Context(), output+".components", relocatedComponents, true); err != nil {
				t.Fatal(err)
			}
			if _, err = ValidateMatchedBackup(t.Context(), relocatedManifest, p, receipt, p.Visible); err != nil {
				t.Fatal("validated supplied backup copy must admit without reusing archived inode authority", err)
			}
			if err = CheckStart(filepath.Join(relocatedComponents, "native")); !errors.Is(err, ErrPending) {
				t.Fatal("relocated snapshot lost pending intent", err)
			}
			for _, component := range []string{"native/", "source/", "indexer/"} {
				var selected string
				for _, file := range manifest.Files {
					if strings.HasPrefix(file.Path, component) {
						selected = filepath.Join(output+".components", filepath.FromSlash(file.Path))
						break
					}
				}
				if selected == "" {
					t.Fatal("component absent from manifest", component)
				}
				original, err := os.ReadFile(selected)
				if err != nil {
					t.Fatal(err)
				}
				for _, damage := range []string{"missing", "corrupt", "linked"} {
					if err = os.Remove(selected); err != nil {
						t.Fatal(err)
					}
					if damage == "corrupt" {
						err = os.WriteFile(selected, []byte("corrupt component"), 0600)
					}
					if damage == "linked" {
						err = os.Symlink(artifact, selected)
					}
					if err != nil {
						t.Fatal(err)
					}
					if _, err = ValidateMatchedBackup(t.Context(), output, p, receipt, p.Visible); err == nil {
						t.Fatal("invalid component admitted", component, damage)
					}
					if damage != "missing" {
						if err = os.Remove(selected); err != nil {
							t.Fatal(err)
						}
					}
					if err = os.WriteFile(selected, original, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			mixed := receipt
			mixed.SnapshotIdentity = "foreign"
			if _, err = ValidateMatchedBackup(t.Context(), output, p, mixed, p.Visible); err == nil {
				t.Fatal("mixed snapshot accepted")
			}
			below := p.Visible
			below.Height = p.Request.Through - 1
			if _, err = ValidateMatchedBackup(t.Context(), output, p, receipt, below); err == nil {
				t.Fatal("old consumer admitted")
			}
			file := filepath.Join(output+".components", "source", "manifest")
			if err = os.WriteFile(file, []byte("corrupt source"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = ValidateMatchedBackup(t.Context(), output, p, receipt, p.Visible); err == nil {
				t.Fatal("corrupt source accepted")
			}
			if err = PublishMatchedBackup(t.Context(), output, p, receipt, o.DataDir, source, artifact, verify); err == nil {
				t.Fatal("corrupt published backup overwritten")
			}
		})
	}
}

func TestMatchedSnapshotCancellationRetainsExactNativeFence(t *testing.T) {
	o, p := fixture(t)
	if err := Compact(t.Context(), o, p); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	finish := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- WithRetentionSnapshot(ctx, o, p, func(string) error { close(entered); <-finish; return nil })
	}()
	<-entered
	if _, err := AcquireRun(o.DataDir); !errors.Is(err, ErrPending) {
		t.Fatal("snapshot admitted native owner", err)
	}
	cancel()
	close(finish)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled snapshot returned success", err)
	}
	if err := CheckStart(o.DataDir); !errors.Is(err, ErrPending) {
		t.Fatal("cancelled snapshot cleared fence", err)
	}
	other := p
	other.Token[0]++
	if err := WithRetentionSnapshot(t.Context(), o, other, func(string) error { t.Fatal("foreign permit copied"); return nil }); err == nil {
		t.Fatal("foreign snapshot accepted")
	}
	if err := WithRetentionSnapshot(t.Context(), o, p, func(string) error { return nil }); err != nil {
		t.Fatal("same-permit snapshot recovery", err)
	}
}
