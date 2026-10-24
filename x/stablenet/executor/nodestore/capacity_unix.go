//go:build darwin || linux

package nodestore

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

func freeBytes(path string) (uint64, error) {
	var stat unix.Statfs_t
	if e := unix.Statfs(path, &stat); e != nil {
		return 0, e
	}
	if stat.Bsize <= 0 {
		return 0, ErrUnsafe
	}
	blocks, size := uint64(stat.Bavail), uint64(stat.Bsize)
	if blocks > ^uint64(0)/size {
		return 0, ErrUnsafe
	}
	return blocks * size, nil
}

func takeLease(dataDir string) (*os.File, error) {
	path := filepath.Join(dataDir, ".execution-retention-lock")
	fd, e := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), path)
	if e = regular(path); e != nil {
		_ = f.Close()
		return nil, e
	}
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		_ = f.Close()
		return nil, ErrPending
	}
	return f, nil
}
