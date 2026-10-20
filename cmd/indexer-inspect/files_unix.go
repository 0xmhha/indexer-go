//go:build linux || darwin

package main

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func regularFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		if err == nil {
			err = os.ErrPermission
		}
		return nil, err
	}
	return f, nil
}

func privateOwner(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0600 && st.Uid == uint32(os.Getuid()) && st.Nlink == 1
}
