//go:build darwin || linux

package spool

import (
	"errors"
	"os"
	"syscall"
)

func owned(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid()) && s.Nlink == 1
}
func charged(info os.FileInfo) uint64 {
	s, ok := info.Sys().(*syscall.Stat_t)
	if ok && s.Blocks > 0 {
		return max(uint64(info.Size()), uint64(s.Blocks)*512)
	}
	return uint64(info.Size())
}
func takeLock(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err := regularPrivate(path); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return f, nil
}
func releaseLock(f *os.File) error {
	if f == nil {
		return nil
	}
	unlock := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	closeErr := f.Close()
	return errors.Join(unlock, closeErr)
}
