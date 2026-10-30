//go:build !darwin && !linux

package arcingress

import "os"

func lockMaintenanceFile(string) (*os.File, error) { return nil, ErrNative }
