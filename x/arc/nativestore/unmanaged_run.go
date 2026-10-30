package nativestore

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type unmanagedRun struct {
	run, writer *os.File
	once        sync.Once
	err         error
}

func (r *unmanagedRun) Close() error {
	r.once.Do(func() { r.err = errors.Join(r.writer.Close(), r.run.Close()) })
	return r.err
}

// AcquireUnmanagedRun excludes the first importer before inspecting ownership.
// The sibling writer name and run-SH -> writer-EX order match the pinned Rust
// Writer::open_inner. Keep both leases until the legacy child is joined. RO
// clients retain their existing shared run lease and can coexist with it.
func AcquireUnmanagedRun(dataDir string) (result io.Closer, err error) {
	canonical, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		return nil, err
	}
	if canonical != dataDir || !filepath.IsAbs(dataDir) {
		return nil, ErrUnsafe
	}
	if _, err = identity(dataDir); err != nil {
		return nil, err
	}
	run, err := AcquireRun(dataDir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Dir(dataDir), "."+filepath.Base(dataDir)+".arc-native-import-writer")
	writer, err := openViewFile(path)
	if err != nil {
		return nil, errors.Join(err, run.Close())
	}
	lease := &unmanagedRun{run: run, writer: writer}
	defer func() {
		if err != nil {
			err = errors.Join(err, lease.Close())
		}
	}()
	if err = tryViewLock(writer, true); err != nil {
		return nil, errors.Join(ErrUnsafe, err)
	}
	info, err := writer.Stat()
	if err != nil {
		return nil, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, current) || PrivateFile(path) != nil {
		return nil, ErrUnsafe
	}
	for _, name := range []string{
		".arc-native-import.identity.json", ".arc-native-import.head.json",
		".arc-native-import-receipts", ".arc-native-import-epochs",
		".arc-native-import.pending", ".arc-native-bootstrap.pending",
		".arc-native-bootstrap.json",
	} {
		// Any shape, including a partial file or dangling link, is ownership.
		if _, e := os.Lstat(filepath.Join(dataDir, name)); !errors.Is(e, os.ErrNotExist) {
			return nil, errors.Join(ErrUnsafe, e)
		}
	}
	if err = CheckStart(dataDir); err != nil {
		return nil, err
	}
	return lease, nil
}
