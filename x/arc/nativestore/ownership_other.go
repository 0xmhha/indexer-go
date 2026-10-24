//go:build !darwin && !linux

package nativestore

func identity(string) (fileIdentity, error) { return fileIdentity{}, ErrUnsafe }
func regular(string) error                  { return ErrUnsafe }
func PrivateFile(string) error              { return ErrUnsafe }
