//go:build !darwin && !linux

package window

import "os"

func ownerPrivateFile(os.FileInfo) error { return ErrUnavailable }
