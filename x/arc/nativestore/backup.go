package nativestore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

type IndexerBackup = retention.IndexerBackup

// MatchedBackup binds all three components, never a native-only copy.
type MatchedBackup struct {
	Version       uint16
	Permit        retention.Permit
	Indexer       IndexerBackup
	ComponentsDir string
	Files         []BackupFile
}
type BackupFile struct {
	Path   string
	Bytes  uint64
	Digest execwire.Hash
}

const backupLimit = uint64(64 << 30)

func backupHash(ctx context.Context, path string) (execwire.Hash, uint64, error) {
	if err := PrivateFile(path); err != nil {
		return execwire.Hash{}, 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return execwire.Hash{}, 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx: ctx, r: f}, int64(backupLimit)+1))
	if err != nil || uint64(n) > backupLimit {
		return execwire.Hash{}, 0, errors.Join(err, ErrUnsafe)
	}
	var digest execwire.Hash
	copy(digest[:], h.Sum(nil))
	return digest, uint64(n), nil
}

func backupFiles(ctx context.Context, root string) ([]BackupFile, error) {
	var files []BackupFile
	var total uint64
	for _, component := range []string{"indexer", "native", "source"} {
		base := filepath.Join(root, component)
		count := 0
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			count++
			if count > 1<<20 {
				return ErrUnsafe
			}
			if d.IsDir() {
				info, err := os.Lstat(path)
				if err != nil {
					return err
				}
				if info.Mode().Perm() != 0700 {
					return ErrUnsafe
				}
				_, err = identity(path)
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			// Pinned MDBX explicitly recreates this reader coordination file
			// and modifies it even during RO opens. It is not durable DB state.
			// Never generalize the exception to other files or any path links.
			if filepath.ToSlash(rel) == "native/db/mdbx.lck" {
				if err = PrivateFile(path); err != nil {
					return err
				}
				info, err := os.Lstat(path)
				if err != nil {
					return err
				}
				if info.Size() < 0 || info.Size() > retention.MaxArtifact {
					return ErrUnsafe
				}
				return nil
			}
			digest, size, err := backupHash(ctx, path)
			if err != nil {
				return err
			}
			if size > backupLimit-total {
				return ErrCapacity
			}
			total += size
			files = append(files, BackupFile{Path: filepath.ToSlash(rel), Bytes: size, Digest: digest})
			return nil
		})
		if err != nil {
			return nil, err
		}
		if count < 2 {
			return nil, ErrUnsafe
		}
	}
	return files, nil
}

func ValidateMatchedBackup(ctx context.Context, path string, p retention.Permit, external IndexerBackup, consumer execwire.Ref) (m MatchedBackup, err error) {
	if external.Validate(p) != nil || consumer != p.Visible || consumer.Height < p.Request.Through ||
		p.Visible.Height > p.Request.Head.Height {
		return m, ErrUnsafe
	}
	if !filepath.IsAbs(path) || PrivateFile(path) != nil {
		return m, ErrUnsafe
	}
	if err = retention.ReadArtifact(path, &m); err != nil {
		return m, err
	}
	if m.Version != 1 || m.Permit != p || m.Indexer != external || m.ComponentsDir != filepath.Base(path)+".components" {
		return m, ErrUnsafe
	}
	root := filepath.Join(filepath.Dir(path), m.ComponentsDir)
	var intent backupIntent
	if err = readJSON(filepath.Join(root, ".intent"), &intent); err != nil {
		return m, err
	}
	// Published bundles can be supplied as independently copied artifacts.
	// The recorded inode is exclusively partial-publication cleanup authority;
	// admission verifies owned private files and every committed checksum.
	_, err = identity(root)
	if err != nil || intent.Version != 1 || intent.Permit != p || intent.Indexer != external {
		return m, ErrUnsafe
	}
	var nativeFence fence
	if err = readJSON(filepath.Join(root, "native", ".execution-retention-pending"), &nativeFence); err != nil {
		return m, err
	}
	if nativeFence.Version != 1 || nativeFence.Phase != "done" || nativeFence.Permit != p {
		return m, ErrUnsafe
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return m, err
	}
	if len(entries) != 4 {
		return m, ErrUnsafe
	}
	files, err := backupFiles(ctx, root)
	if err != nil {
		return m, err
	}
	if !reflect.DeepEqual(files, m.Files) {
		return m, ErrUnsafe
	}
	var receipt IndexerBackup
	if err = retention.ReadArtifact(filepath.Join(root, "indexer", "receipt.json"), &receipt); err != nil {
		return m, err
	}
	if receipt != external {
		return m, ErrUnsafe
	}
	digest, _, err := backupHash(ctx, filepath.Join(root, "indexer", "database.backup"))
	if err != nil || digest != external.ArtifactDigest {
		return m, ErrUnsafe
	}
	return m, nil
}

type backupIntent struct {
	Version  uint16
	Permit   retention.Permit
	Indexer  IndexerBackup
	Identity fileIdentity
}

// PublishMatchedBackup copies only while its caller holds BOTH producer
// snapshots. The final private manifest is published atomically without
// overwrite. No successful manifest exists until every component is durable.
// This seals supplied external DB evidence; it does not certify a SQL dump's
// provenance or authorize activation. The live DB coordinator must validate
// that external receipt separately before allowing activation.
func PublishMatchedBackup(ctx context.Context, path string, p retention.Permit, external IndexerBackup, native, source, artifact string, verify func(string) error) error {
	return publishMatchedBackup(ctx, path, p, external, native, source, artifact, verify, hooks{})
}
func publishMatchedBackup(ctx context.Context, path string, p retention.Permit, external IndexerBackup, native, source, artifact string, verify func(string) error, h hooks) error {
	if !filepath.IsAbs(path) || external.Validate(p) != nil || p.Visible.Height > p.Request.Head.Height || verify == nil {
		return ErrUnsafe
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return ErrUnsafe
	}
	if _, err = identity(parent); err != nil {
		return err
	}
	root := path + ".components"
	for _, src := range []string{native, source, artifact} {
		canonical, err := filepath.EvalSymlinks(src)
		if err != nil {
			return err
		}
		if canonical != filepath.Clean(src) || !filepath.IsAbs(src) ||
			strings.HasPrefix(root+string(os.PathSeparator), src+string(os.PathSeparator)) ||
			strings.HasPrefix(src+string(os.PathSeparator), root+string(os.PathSeparator)) {
			return ErrUnsafe
		}
	}
	var total uint64
	for _, src := range []string{native, source} {
		size, e := treeBytes(ctx, src)
		if e != nil {
			return e
		}
		if size > backupLimit-total {
			return ErrCapacity
		}
		total += size
	}
	ai, e := os.Stat(artifact)
	if e != nil {
		return e
	}
	if ai.Size() < 0 || uint64(ai.Size()) > backupLimit-total {
		return ErrCapacity
	}
	total += uint64(ai.Size())
	available, e := freeBytes(parent)
	if e != nil {
		return e
	}
	if available < total+(1<<30) {
		return ErrCapacity
	}
	_, nativeMarker, _, _, e := paths(Options{DataDir: native})
	if e != nil {
		return e
	}
	var nativeFence fence
	if e = readJSON(nativeMarker, &nativeFence); e != nil {
		return e
	}
	if nativeFence.Permit != p || nativeFence.Phase != "done" || nativeFence.Version != 1 || !sameDir(native, nativeFence.Replacement) {
		return ErrUnsafe
	}
	digest, _, err := backupHash(ctx, artifact)
	if err != nil || digest != external.ArtifactDigest {
		return ErrUnsafe
	}
	if _, err = os.Lstat(path); err == nil {
		_, err = ValidateMatchedBackup(ctx, path, p, external, p.Visible)
		return err
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Recovery discards only this permit's recorded private partial component
	// directory. A replacement inode, mixed receipt or foreign layout survives.
	if _, err = os.Lstat(root); err == nil {
		var old backupIntent
		if err = readJSON(filepath.Join(root, ".intent"), &old); err != nil {
			return err
		}
		id, err := identity(root)
		if err != nil || old.Version != 1 || old.Permit != p || old.Indexer != external || old.Identity != id {
			return ErrUnsafe
		}
		if err = removeTree(root); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.Mkdir(root, 0700); err != nil {
		return err
	}
	id, err := identity(root)
	if err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(root, ".intent"), backupIntent{1, p, external, id}); err != nil {
		return err
	}
	if err = h.at("backup_intent"); err != nil {
		return err
	}
	for _, component := range []struct{ name, path string }{{"native", native}, {"source", source}} {
		dest := filepath.Join(root, component.name)
		if err = os.Mkdir(dest, 0700); err != nil {
			return err
		}
		if err = copyTreeWithOwner(ctx, component.path, dest, true); err != nil {
			return err
		}
		if err = h.at("backup_" + component.name); err != nil {
			return err
		}
	}
	if err = writeJSON(filepath.Join(root, "native", ".execution-retention-pending"), nativeFence); err != nil {
		return err
	}
	indexer := filepath.Join(root, "indexer")
	if err = os.Mkdir(indexer, 0700); err != nil {
		return err
	}
	in, err := os.Open(artifact)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(filepath.Join(indexer, "database.backup"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, contextReader{ctx: ctx, r: in})
	if err = errors.Join(copyErr, out.Sync(), out.Close()); err != nil {
		return err
	}
	if err = retention.WriteArtifact(filepath.Join(indexer, "receipt.json"), external); err != nil {
		return err
	}
	if err = h.at("backup_indexer"); err != nil {
		return err
	}
	files, err := backupFiles(ctx, root)
	if err != nil {
		return err
	}
	// Re-read the copied DB bytes, not only the supplied receipt.
	copied, _, err := backupHash(ctx, filepath.Join(indexer, "database.backup"))
	if err != nil || copied != external.ArtifactDigest {
		return ErrUnsafe
	}
	m := MatchedBackup{Version: 1, Permit: p, Indexer: external, ComponentsDir: filepath.Base(root), Files: files}
	if _, err = json.Marshal(m); err != nil {
		return err
	}
	if err = syncTree(root); err != nil {
		return err
	}
	if err = syncDir(parent); err != nil {
		return err
	}
	if err = h.at("backup_durable"); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = verify(root); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = publishBackupManifest(path, m); err != nil {
		return err
	}
	if err = h.at("backup_published"); err != nil {
		return err
	}
	_, err = ValidateMatchedBackup(ctx, path, p, external, p.Visible)
	return err
}

func publishBackupManifest(path string, m MatchedBackup) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(data) > retention.MaxArtifact {
		return ErrUnsafe
	}
	parent := filepath.Dir(path)
	f, err := os.CreateTemp(parent, ".matched-manifest-")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	_, writeErr := f.Write(data)
	if err = errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	if err = os.Link(f.Name(), path); err != nil {
		return err
	}
	if err = os.Remove(f.Name()); err != nil {
		return err
	}
	return syncDir(parent)
}

// ValidateIndexerArtifact binds the explicitly supplied external DB material to
// its independently authenticated receipt before any native work or admission.
func ValidateIndexerArtifact(ctx context.Context, path string, r IndexerBackup) error {
	digest, _, err := backupHash(ctx, path)
	if err != nil {
		return err
	}
	if digest != r.ArtifactDigest {
		return ErrUnsafe
	}
	return nil
}
