//go:build darwin || linux

package execution

import (
	"os"
	"syscall"
)

func executionOwned(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid())
}
