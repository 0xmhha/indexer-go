//go:build !linux && !darwin

package nativealerts

import "context"

type FileStore struct{}

func OpenFileStore(string) (*FileStore, error)            { return nil, ErrCheckpoint }
func (*FileStore) Read(context.Context) (Snapshot, error) { return Snapshot{}, ErrCheckpoint }
func (*FileStore) Write(context.Context, Snapshot) error  { return ErrCheckpoint }
func (*FileStore) Close() error                           { return nil }
