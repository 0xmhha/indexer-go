//go:build !darwin && !linux

package nodestore

import "os"

func takeLease(string) (*os.File, error) { return nil, ErrUnsafe }

func freeBytes(string) (uint64, error) { return 0, ErrUnsafe }
