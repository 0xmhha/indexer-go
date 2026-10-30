package arcingress

import (
	"errors"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"os"
	"path/filepath"
)

// Resolve missing leaves through their nearest existing parent. This is only
// alias/overlap validation, never permission to create or replace a path.
func resolvePlanned(path string) (string, error) {
	path = filepath.Clean(path)
	var leaves []string
	for {
		_, err := os.Lstat(path)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		leaves = append(leaves, filepath.Base(path))
		next := filepath.Dir(path)
		if next == path {
			return "", ErrNative
		}
		path = next
	}
	base, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	for i := len(leaves) - 1; i >= 0; i-- {
		base = filepath.Join(base, leaves[i])
	}
	return base, nil
}
func checkPaths(c ingress.Config, o window.Options, socket string) error {
	for _, dir := range []string{c.Native.DataDir, filepath.Dir(c.Native.DataDir), filepath.Dir(socket), filepath.Dir(c.Collector.SpoolDir)} {
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if !privateDirectory(info) {
			return ErrNative
		}
	}
	for _, path := range []string{c.Native.Executable, c.Collector.Executable} {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !ownedExecutable(info) {
			return ErrNative
		}
	}
	var paths []string
	for _, path := range []string{c.Native.DataDir, o.Path, filepath.Dir(socket), c.Native.Executable, c.Collector.Executable, c.Collector.SpoolDir} {
		resolved, err := resolvePlanned(path)
		if err != nil {
			return err
		}
		for _, prior := range paths {
			if ingress.Overlap(prior, resolved) {
				return ErrNative
			}
		}
		paths = append(paths, resolved)
	}
	return nil
}
