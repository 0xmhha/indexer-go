//go:build darwin

package exporter

import (
	"syscall"
	"unsafe"
)

// Darwin's LOCAL_PEERCRED returns struct xucred (76 bytes on arm64/amd64).
// Keep the isolated module's production imports standard-library only.
func sourcePeerUID(fd int) (uint32, error) {
	var cred struct {
		Version, UID uint32
		NGroups      int16
		Groups       [16]uint32
	}
	size := uint32(unsafe.Sizeof(cred))
	_, _, errno := syscall.Syscall6(syscall.SYS_GETSOCKOPT, uintptr(fd), 0, 1, uintptr(unsafe.Pointer(&cred)), uintptr(unsafe.Pointer(&size)), 0)
	if errno != 0 {
		return 0, errno
	}
	if size != uint32(unsafe.Sizeof(cred)) || cred.Version != 0 {
		return 0, syscall.EINVAL
	}
	return cred.UID, nil
}
