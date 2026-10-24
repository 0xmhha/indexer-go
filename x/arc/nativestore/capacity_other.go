//go:build !darwin && !linux

package nativestore

import "os"

func takeLease(string) (*os.File, error) { return nil, ErrUnsafe }

func freeBytes(string) (uint64, error) { return 0, ErrUnsafe }

func takeRunLease(string) (*os.File, error) { return nil, ErrUnsafe }
