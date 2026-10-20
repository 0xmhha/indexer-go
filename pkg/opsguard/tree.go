package opsguard

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
)

// DefaultTreeEntries bounds one directory size walk.
const DefaultTreeEntries = 1_000_000

// ctxCheckEvery is how many entries are walked between cancellation checks.
const ctxCheckEvery = 1024

// TreeBytes sums the allocated bytes under root, as du does. It follows no
// symbolic links, stays on root's filesystem and fails rather than returning
// a partial sum when it cannot read an entry or exceeds maxEntries.
func TreeBytes(ctx context.Context, root string, maxEntries int) (int64, error) {
	if maxEntries <= 0 {
		maxEntries = DefaultTreeEntries
	}
	var total int64
	var device uint64
	entries := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entries++
		if entries > maxEntries {
			return fmt.Errorf("opsguard: %s has more than %d entries", root, maxEntries)
		}
		if entries%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size, dev, ok := allocated(info)
		if !ok {
			return errors.New("opsguard: allocated size unavailable")
		}
		if path == root {
			device = dev
		} else if dev != device {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		total += size
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}
