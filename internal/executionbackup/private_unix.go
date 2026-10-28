//go:build darwin || linux

package executionbackup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/0xmhha/indexer-go/execwire/retention"
	"golang.org/x/sys/unix"
)

// openDir walks held directory descriptors. Parent symlink substitutions cannot
// redirect the next open; no absolute path is reopened after validation.
func openDir(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexByte(path, 0) >= 0 {
		return nil, retention.ErrUnsafe
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}

func privateInfo(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0600 && st.Uid == uint32(os.Geteuid()) && st.Nlink == 1
}

func openPrivate(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, retention.ErrUnsafe
	}
	dir, err := openDir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	fd, err := unix.Openat(int(dir.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !privateInfo(info) {
		_ = f.Close()
		return nil, retention.ErrUnsafe
	}
	return f, nil
}

func publishReceipt(ctx context.Context, path string, receipt retention.IndexerBackup) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "HOLD" {
		return retention.ErrUnsafe
	}
	dir, err := openDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	info, err := dir.Stat()
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0700 {
		return retention.ErrUnsafe
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	name := ".receipt-" + hex.EncodeToString(random[:])
	dfd := int(dir.Fd())
	fd, err := unix.Openat(dfd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Unlinkat(dfd, name, 0) }()
	f := os.NewFile(uintptr(fd), name)
	_, writeErr := f.Write(data)
	if err = errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = unix.Linkat(dfd, name, dfd, filepath.Base(path), 0); err != nil {
		return err
	}
	if err = unix.Unlinkat(dfd, name, 0); err != nil {
		return err
	}
	return dir.Sync()
}
