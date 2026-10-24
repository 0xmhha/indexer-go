//go:build !darwin && !linux

package retention

import "os"

func supervisorPipe(*os.File) (*os.File, error) { return nil, ErrUnsafe }
