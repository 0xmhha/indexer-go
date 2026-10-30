//go:build !darwin && !linux

package nativeinput

import "os"

func openArtifact(string) (*os.File, error) { return nil, ErrProof }
func privateArtifactDirectory(string) error { return ErrProof }
