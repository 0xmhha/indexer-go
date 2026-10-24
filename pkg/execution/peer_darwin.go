//go:build darwin

package execution

import "golang.org/x/sys/unix"

func publishExecutionSocket(from, to string) error { return unix.RenamexNp(from, to, unix.RENAME_EXCL) }

func executionPeerUID(fd int) (uint32, error) {
	c, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, err
	}
	return c.Uid, nil
}
