//go:build darwin || linux

package nativestore

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func viewDirectory(path string) error {
	if _, err := identity(path); err != nil {
		return err
	}
	i, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if i.Mode().Perm() != 0700 {
		return ErrUnsafe
	}
	return nil
}
func openViewFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err = PrivateFile(path); err == nil {
		i, e := f.Stat()
		if e != nil {
			err = e
		} else {
			p, e := os.Lstat(path)
			if e != nil {
				err = e
			} else if !os.SameFile(i, p) {
				err = ErrUnsafe
			}
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
func tryViewLock(f *os.File, exclusive bool) error {
	mode := unix.LOCK_SH
	if exclusive {
		mode = unix.LOCK_EX
	}
	return unix.Flock(int(f.Fd()), mode|unix.LOCK_NB)
}
func unlockView(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
func viewWouldBlock(err error) bool {
	return errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK)
}
