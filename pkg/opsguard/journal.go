package opsguard

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
)

// DefaultJournalMaxBytes bounds one journal file; one rotated file is kept.
const DefaultJournalMaxBytes = 8 << 20

// Journal appends JSON lines to a file of at most max bytes. When a line would
// exceed it, the file moves to path+".1", replacing the previous rotation, so
// at most twice max bytes are kept on disk.
type Journal struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

// OpenJournal opens or creates the journal at path.
func OpenJournal(path string, max int64) (*Journal, error) {
	if path == "" {
		return nil, errors.New("opsguard: journal path is empty")
	}
	if max <= 0 {
		max = DefaultJournalMaxBytes
	}
	j := &Journal{path: path, max: max}
	if err := j.open(); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *Journal) open() error {
	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	j.f, j.size = f, st.Size()
	return nil
}

// Write appends v as one JSON line. A nil journal discards it.
func (j *Journal) Write(v any) error {
	if j == nil {
		return nil
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return os.ErrClosed
	}
	if j.size > 0 && j.size+int64(len(line)) > j.max {
		if err := j.f.Close(); err != nil {
			return err
		}
		j.f = nil
		if err := os.Rename(j.path, j.path+".1"); err != nil {
			return err
		}
		if err := j.open(); err != nil {
			return err
		}
	}
	n, err := j.f.Write(line)
	j.size += int64(n)
	return err
}

// Close closes the journal file.
func (j *Journal) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return nil
	}
	err := j.f.Close()
	j.f = nil
	return err
}
