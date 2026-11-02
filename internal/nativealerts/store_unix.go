//go:build linux || darwin

package nativealerts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const checkpointName = "alerts.json"

var errAbsent = errors.New("alert: no checkpoint")

// FileStore holds the admitted private directory descriptor and a nonblocking
// cooperative writer lock. It never creates/provisions a directory or opens a
// path from file contents. Calls/Close are single-owner, not concurrent.
type FileStore struct {
	dir   *os.File
	last  ID
	fault func(string) error // owned fault injection; nil in production
}

func OpenFileStore(path string) (*FileStore, error) {
	if runtime.GOOS == "darwin" {
		for _, prefix := range []string{"/tmp/", "/var/"} {
			if strings.HasPrefix(path, prefix) {
				path = "/private" + path
				break
			}
		}
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexByte(path, 0) >= 0 {
		return nil, ErrCheckpoint
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrCheckpoint
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if e != nil {
			return nil, ErrCheckpoint
		}
		fd = next
	}
	s := &FileStore{dir: os.NewFile(uintptr(fd), "private alert directory")}
	if !s.safeDirectory() || unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		_ = s.Close()
		return nil, ErrCheckpoint
	}
	return s, nil
}
func (s *FileStore) Close() error {
	if s == nil || s.dir == nil {
		return nil
	}
	f := s.dir
	s.dir = nil
	return f.Close()
}
func (s *FileStore) safeDirectory() bool {
	if s == nil || s.dir == nil {
		return false
	}
	info, err := s.dir.Stat()
	if err != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.IsDir() && info.Mode().Perm() == 0700 && info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 && st.Uid == uint32(os.Geteuid())
}
func privateFile(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0600 && info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 && st.Uid == uint32(os.Geteuid()) && st.Nlink == 1 && info.Size() <= MaxCheckpointBytes
}
func (s *FileStore) read(ctx context.Context) (Snapshot, ID, error) {
	if ctx.Err() != nil || !s.safeDirectory() {
		return Snapshot{}, ID{}, ErrCheckpoint
	}
	fd, err := unix.Openat(int(s.dir.Fd()), checkpointName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return Snapshot{}, ID{}, errAbsent
	}
	if err != nil {
		return Snapshot{}, ID{}, ErrCheckpoint
	}
	f := os.NewFile(uintptr(fd), checkpointName)
	defer func() { _ = f.Close() }()
	before, err := f.Stat()
	if err != nil || !privateFile(before) {
		return Snapshot{}, ID{}, ErrCheckpoint
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxCheckpointBytes+1))
	if err != nil || len(data) > MaxCheckpointBytes {
		return Snapshot{}, ID{}, ErrCheckpoint
	}
	after, err := f.Stat()
	if err != nil || !privateFile(after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return Snapshot{}, ID{}, ErrCheckpoint
	}
	var named unix.Stat_t
	if unix.Fstatat(int(s.dir.Fd()), checkpointName, &named, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return Snapshot{}, ID{}, ErrCheckpoint
	}
	st := after.Sys().(*syscall.Stat_t)
	if uint64(named.Ino) != uint64(st.Ino) || uint64(named.Dev) != uint64(st.Dev) || ctx.Err() != nil {
		return Snapshot{}, ID{}, ErrCheckpoint
	}
	result, err := decodeSnapshot(data)
	if err != nil {
		return Snapshot{}, ID{}, ErrCheckpoint
	}
	return result, digest(data), nil
}
func (s *FileStore) Read(ctx context.Context) (Snapshot, error) {
	v, id, err := s.read(ctx)
	if err != nil {
		return Snapshot{}, ErrCheckpoint
	}
	s.last = id
	return v, nil
}
func (s *FileStore) step(name string) error {
	if s.fault != nil {
		return s.fault(name)
	}
	return nil
}

func (s *FileStore) Write(ctx context.Context, snapshot Snapshot) error {
	if ctx.Err() != nil || !s.safeDirectory() || validateSnapshot(snapshot) != nil {
		return ErrCheckpoint
	}
	_, current, err := s.read(ctx)
	if err != nil && !errors.Is(err, errAbsent) {
		return ErrCheckpoint
	}
	// Existing data must first have been loaded by this owner. Refuse silent
	// replacement of a different valid checkpoint, even under the same UID.
	if current != s.last {
		return ErrCheckpoint
	}
	data, err := json.Marshal(snapshot)
	if err != nil || len(data) > MaxCheckpointBytes {
		return ErrCheckpoint
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return ErrCheckpoint
	}
	name := ".alerts-" + hex.EncodeToString(random[:])
	dfd := int(s.dir.Fd())
	fd, err := unix.Openat(dfd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return ErrCheckpoint
	}
	defer func() { _ = unix.Unlinkat(dfd, name, 0) }()
	f := os.NewFile(uintptr(fd), name)
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil || ctx.Err() != nil || s.step("prepared") != nil {
		return ErrCheckpoint
	}
	_, check, readErr := s.read(ctx)
	if readErr != nil && !errors.Is(readErr, errAbsent) || check != current || !s.safeDirectory() {
		return ErrCheckpoint
	}
	if unix.Renameat(dfd, name, dfd, checkpointName) != nil {
		return ErrCheckpoint
	}
	s.last = digest(data)
	if s.step("published") != nil || s.dir.Sync() != nil {
		return ErrCheckpoint
	}
	return nil
}
