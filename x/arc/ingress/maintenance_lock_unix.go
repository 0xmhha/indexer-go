//go:build darwin || linux

package arcingress

import (
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"golang.org/x/sys/unix"
	"os"
)

func lockMaintenanceFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err = nativestore.PrivateFile(path); err == nil {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
