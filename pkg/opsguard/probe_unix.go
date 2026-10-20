//go:build linux || darwin

package opsguard

import (
	"io/fs"
	"syscall"

	"golang.org/x/sys/unix"
)

// FreeBytes reports the bytes available to unprivileged writers on the
// filesystem holding path.
func FreeBytes(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

// allocated returns the allocated bytes and device of an entry.
func allocated(info fs.FileInfo) (int64, uint64, bool) {
	// os.FileInfo carries the syscall package's Stat_t, not x/sys/unix's.
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return st.Blocks * 512, uint64(st.Dev), true //nolint:unconvert,gosec // Dev is int32 on darwin; device numbers are not negative
}
