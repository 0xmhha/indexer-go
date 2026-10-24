//go:build !darwin && !linux

package nodestore

func identity(string) (fileIdentity, error) { return fileIdentity{}, ErrUnsafe }
func regular(string) error                  { return ErrUnsafe }
