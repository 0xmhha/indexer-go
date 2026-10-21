//go:build !linux && !darwin

package opsguard

import (
	"errors"
	"io/fs"
)

var errUnsupported = errors.New("opsguard: capacity probes need linux or darwin")

// FreeBytes is unsupported on this platform.
func FreeBytes(string) (uint64, error) { return 0, errUnsupported }

func allocated(fs.FileInfo) (int64, uint64, bool) { return 0, 0, false }
