//go:build !linux && !darwin

package main

import "os"

func regularFile(string) (*os.File, error) { return nil, os.ErrPermission }
func privateOwner(os.FileInfo) bool        { return false }
