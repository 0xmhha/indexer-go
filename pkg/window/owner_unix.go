//go:build darwin || linux

package window

import (
	"os"
	"syscall"
)

func ownerPrivateFile(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return ErrCorrupt
	}
	return nil
}
