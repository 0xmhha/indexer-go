//go:build darwin || linux

package retention

import (
	"os"
	"syscall"
)

// ExtraFiles/FD3 can arrive as a blocking descriptor. A plain os.NewFile on
// that FD is not registered with Go's poller, so Close cannot interrupt Read.
// Duplicate first, enable nonblocking mode, then create a pollable owned file.
func supervisorPipe(file *os.File) (*os.File, error) {
	defer func() { _ = file.Close() }()
	fd, e := syscall.Dup(int(file.Fd()))
	if e != nil {
		return nil, e
	}
	syscall.CloseOnExec(fd)
	if e = syscall.SetNonblock(fd, true); e != nil {
		_ = syscall.Close(fd)
		return nil, e
	}
	if e = file.Close(); e != nil {
		_ = syscall.Close(fd)
		return nil, e
	}
	return os.NewFile(uintptr(fd), "retention-supervisor-pollable"), nil
}
