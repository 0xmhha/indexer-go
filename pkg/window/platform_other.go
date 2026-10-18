//go:build !linux && !darwin

package window

import (
	"errors"
	"os"
)

var errPlatform = errors.New("window: durable volume guard supports Linux and macOS")

func noFollowNonblock() int                       { return 0 }
func lockExclusive(*os.File) error                { return errPlatform }
func renameFlat(*os.Root, string, string) error   { return errPlatform }
func sameDevice(*os.File, *os.File) (bool, error) { return false, errPlatform }
func freeBytes(*os.File) (uint64, error)          { return 0, errPlatform }
func checkMount(*os.File, VolumeSpec) error       { return errPlatform }
