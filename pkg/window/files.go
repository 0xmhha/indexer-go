package window

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/trace"
	"strings"
	"time"
)

// All names passed to renameFlat are fixed internal basenames. openat-based
// roots pin the original directories across an unmount/path replacement;
// no file operation falls back to reopening an absolute internal-disk path.
func syncDir(root *os.Root) error {
	region := trace.StartRegion(context.Background(), "window.directorySync")
	defer region.End()
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func openRegular(root *os.Root, name string, flags int) (*os.File, error) {
	f, err := root.OpenFile(name, flags|noFollowNonblock(), 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = fmt.Errorf("window: %s is not a regular file", name)
		}
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}

func readSmall(root *os.Root, name string, max int64) ([]byte, error) {
	f, err := openRegular(root, name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err == nil && int64(len(data)) > max {
		err = ErrCorrupt
	}
	return data, errors.Join(err, f.Close())
}

func writeAtomic(root *os.Root, name string, data []byte) error {
	if err := writeSyncedRename(root, name, data); err != nil {
		return err
	}
	return syncDir(root)
}

// writeSyncedRename makes file contents durable and renames the file, but
// DOES NOT make the directory change durable. The caller must sync the parent
// before publishing success or creating anything dependent on this name.
func writeSyncedRename(root *os.Root, name string, data []byte) error {
	pending, err := prepareRename(root, name, data)
	if err != nil {
		return err
	}
	defer pending.close()
	return pending.commit()
}

// A prepared temporary is private and not authoritative. Preparing independent
// files before either sync lets the filesystem flush them together. Each file
// still receives its own full Sync/Close before its original ordered rename;
// the caller retains responsibility for the parent directory barrier.
type preparedRename struct {
	root *os.Root
	name string
	file *os.File
}

func prepareRename(root *os.Root, name string, data []byte) (*preparedRename, error) {
	tmp := name + ".tmp"
	// A stale temporary is never authoritative. Do not follow it as a link.
	if err := root.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := openRegular(root, tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(data); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return &preparedRename{root: root, name: name, file: f}, nil
}

func (p *preparedRename) close() {
	if p.file != nil {
		_ = p.file.Close()
		p.file = nil
	}
}

func (p *preparedRename) commit() error {
	err := p.file.Sync()
	err = errors.Join(err, p.file.Close())
	p.file = nil
	if err != nil {
		return err
	}
	return renameFlat(p.root, p.name+".tmp", p.name)
}

func writeJSON(root *os.Root, name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeAtomic(root, name, data)
}

// mkdirWithin uses only Go 1.24 Root methods, syncing each new parent entry.
func mkdirWithin(root *os.Root, rel string) error {
	if !filepath.IsLocal(rel) {
		return errors.New("window: path escapes volume")
	}
	prefix := ""
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		prefix = filepath.Join(prefix, part)
		if err := root.Mkdir(prefix, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		parent, err := root.OpenRoot(filepath.Dir(prefix))
		if err != nil {
			return err
		}
		err = errors.Join(syncDir(parent), parent.Close())
		if err != nil {
			return err
		}
	}
	return nil
}

type volumeMarker struct {
	Version      int
	ID           string
	MaxBytes     int64 `json:",omitempty"`
	MinFreeBytes int64 `json:",omitempty"`
}

func markerFor(spec VolumeSpec) volumeMarker {
	v := 1
	if spec.MaxBytes > 0 {
		v = 2
	}
	return volumeMarker{Version: v, ID: spec.ID, MaxBytes: spec.MaxBytes, MinFreeBytes: spec.MinFreeBytes}
}

// InitializeVolume is explicit and idempotent for the same ID and limits. It never
// creates a missing root or registers a different/unmounted volume for Open.
func InitializeVolume(spec VolumeSpec) error {
	if !filepath.IsAbs(spec.Root) || !validVolumeID(spec.ID) || spec.MaxBytes < 0 || spec.MaxBytes > 1<<60 || spec.MinFreeBytes < 0 || (spec.MaxBytes == 0 && spec.MinFreeBytes != 0) || (spec.MaxBytes > 0 && spec.MaxBytes <= VolumeOverheadBytes) {
		return errors.New("window: invalid volume specification")
	}
	root, err := os.OpenRoot(spec.Root)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	if err := checkMount(dir, spec); err != nil {
		return err
	}
	lock, err := openRegular(root, ".indexer-init.lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := lockExclusive(lock); err != nil {
		return err
	}
	data, err := readSmall(root, ".indexer-volume.json", maxMeta)
	if err == nil {
		var old volumeMarker
		if json.Unmarshal(data, &old) != nil || old != markerFor(spec) {
			return ErrConflict
		}
		if spec.MaxBytes > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return volumeLock(ctx, root, func() error { _, err := readBudget(root, spec); return err })
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if spec.MaxBytes > 0 {
		if err := initializeBudget(root, spec); err != nil {
			return err
		}
	} else {
		if _, err := root.Lstat(budgetFile); !errors.Is(err, os.ErrNotExist) {
			return ErrConflict
		}
	}
	return writeJSON(root, ".indexer-volume.json", markerFor(spec))
}
