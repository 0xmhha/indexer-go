//go:build darwin || linux

package metadata

import (
	"os"
	"syscall"
)

func owned(i os.FileInfo, file bool) bool {
	st, ok := i.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid()) && (!file || st.Nlink == 1)
}
