package opsguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	latchVersion  = 1
	maxDetail     = 512
	maxLatchBytes = 4096
)

// Latch records the first cause of a halt. It holds no secrets: only the
// reason, the chain, a bounded error description and the time.
type Latch struct {
	Version int       `json:"version"`
	Reason  Reason    `json:"reason"`
	Source  Source    `json:"source,omitempty"`
	Chain   string    `json:"chain,omitempty"`
	Detail  string    `json:"detail"`
	At      time.Time `json:"at"`
	// StopDatabase asks the host to stop the database as well (StopsDatabase).
	StopDatabase bool `json:"stopDatabase,omitempty"`
}

// WriteLatch durably records a halt. An existing latch is kept, so the first
// cause survives later consequences of the same halt.
func WriteLatch(path string, l Latch) error {
	if path == "" {
		return errors.New("opsguard: halt latch path is empty")
	}
	l.Version = latchVersion
	if len(l.Detail) > maxDetail {
		l.Detail = l.Detail[:maxDetail]
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".halt-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	_, err = tmp.Write(append(data, '\n'))
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	// Link never replaces an existing latch, unlike rename.
	if err := os.Link(name, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	return syncDir(dir)
}

// ReadLatch returns the latch at path, or nil when there is none.
func ReadLatch(path string) (*Latch, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxLatchBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxLatchBytes {
		return nil, fmt.Errorf("opsguard: halt latch %s exceeds %d bytes", path, maxLatchBytes)
	}
	var l Latch
	if err := json.Unmarshal(data, &l); err != nil || l.Version != latchVersion {
		// An unreadable latch still blocks startup; it is never ignored.
		return &Latch{Version: latchVersion, Reason: ReasonIntegrity, Detail: "unreadable halt latch"}, nil
	}
	return &l, nil
}

// clearLatch removes the latch durably. Only Resume calls it.
func clearLatch(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}

// CheckStartup refuses to start while a halt latch exists.
func CheckStartup(path string) error {
	l, err := ReadLatch(path)
	if err != nil {
		return fmt.Errorf("opsguard: read halt latch: %w", err)
	}
	if l == nil {
		return nil
	}
	return fmt.Errorf("%w at %s: %s %s since %s (%s); resolve the cause, then run the resume check (indexer resume-check)", ErrHalted, path, l.Reason, l.Source, l.At.Format(time.RFC3339), l.Detail)
}
