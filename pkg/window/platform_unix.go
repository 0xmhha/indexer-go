//go:build linux || darwin

package window

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func noFollowNonblock() int { return unix.O_NOFOLLOW | unix.O_NONBLOCK }

func lockExclusive(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return ErrLocked
	}
	return err
}

func renameFlat(root *os.Root, old, next string) error {
	if filepath.Base(old) != old || filepath.Base(next) != next {
		return ErrCorrupt
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return unix.Renameat(int(dir.Fd()), old, int(dir.Fd()), next)
}

func sameDevice(a, b *os.File) (bool, error) {
	var x, y unix.Stat_t
	if err := unix.Fstat(int(a.Fd()), &x); err != nil {
		return false, err
	}
	if err := unix.Fstat(int(b.Fd()), &y); err != nil {
		return false, err
	}
	return x.Dev == y.Dev, nil
}

func freeBytes(f *os.File) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Fstatfs(int(f.Fd()), &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
