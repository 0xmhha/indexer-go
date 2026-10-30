//go:build darwin || linux

package nativeinput

import (
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func openArtifact(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
func privateArtifactDirectory(path string) error {
	i, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := i.Sys().(*syscall.Stat_t)
	if !ok || !i.IsDir() || i.Mode().Perm() != 0700 || st.Uid != uint32(os.Geteuid()) {
		return ErrProof
	}
	return nil
}
