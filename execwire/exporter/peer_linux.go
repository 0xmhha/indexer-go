//go:build linux

package exporter

import "syscall"

func sourcePeerUID(fd int) (uint32, error) {
	cred, err := syscall.GetsockoptUcred(fd, syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	if err != nil {
		return 0, err
	}
	return cred.Uid, nil
}
