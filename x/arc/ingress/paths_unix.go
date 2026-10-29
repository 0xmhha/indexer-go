//go:build darwin || linux

package arcingress

import (
	"os"
	"syscall"
)

func privateDirectory(i os.FileInfo) bool {
	s, ok := i.Sys().(*syscall.Stat_t)
	return ok && i.IsDir() && i.Mode().Perm() == 0700 && s.Uid == uint32(os.Geteuid())
}
func ownedExecutable(i os.FileInfo) bool {
	s, ok := i.Sys().(*syscall.Stat_t)
	return ok && i.Mode().IsRegular() && i.Mode().Perm()&0111 != 0 && i.Mode().Perm()&0022 == 0 && s.Uid == uint32(os.Geteuid()) && s.Nlink == 1
}
