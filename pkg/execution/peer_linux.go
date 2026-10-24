//go:build linux

package execution

import "golang.org/x/sys/unix"

func publishExecutionSocket(from, to string) error {
	return unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
}

func executionPeerUID(fd int) (uint32, error) {
	c, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, err
	}
	return c.Uid, nil
}
