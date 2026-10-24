//go:build darwin || linux

package nodestore

import (
	"os"
	"syscall"
)

func identity(path string) (fileIdentity, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return fileIdentity{}, e
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || st.Uid != uint32(os.Geteuid()) {
		return fileIdentity{}, ErrUnsafe
	}
	return fileIdentity{uint64(st.Dev), st.Ino}, nil
}
func regular(path string) error {
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		return ErrUnsafe
	}
	return nil
}
