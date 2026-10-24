//go:build !darwin && !linux

package spool

import (
	"errors"
	"os"
)

func owned(os.FileInfo) bool          { return false }
func charged(info os.FileInfo) uint64 { return uint64(info.Size()) }
func takeLock(string) (*os.File, error) {
	return nil, errors.New("spool: process locking requires Linux or macOS")
}
func releaseLock(*os.File) error { return nil }
