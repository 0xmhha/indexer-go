//go:build !darwin && !linux

package nativestore

import "os"

func viewDirectory(string) error            { return ErrUnsafe }
func openViewFile(string) (*os.File, error) { return nil, ErrUnsafe }
func tryViewLock(*os.File, bool) error      { return ErrUnsafe }
func unlockView(*os.File) error             { return ErrUnsafe }
func viewWouldBlock(error) bool             { return false }
