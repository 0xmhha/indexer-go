//go:build darwin || linux

package exporter

import (
	"os"
	"syscall"
)

func sourceOwned(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid())
}
