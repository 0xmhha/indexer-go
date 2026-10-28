//go:build !darwin && !linux

package metadata

import "os"

func owned(os.FileInfo, bool) bool { return false }
