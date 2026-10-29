//go:build !darwin && !linux

package arcingress

import "os"

func privateDirectory(os.FileInfo) bool { return false }
func ownedExecutable(os.FileInfo) bool  { return false }
