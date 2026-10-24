package exporter

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"

	"github.com/0xmhha/indexer-go/execwire/spool"
)

func dialExecution(ctx context.Context, path string) (net.Conn, error) {
	if !filepath.IsAbs(path) {
		return nil, spool.ErrConflict
	}
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if !dir.IsDir() || dir.Mode().Perm() != 0700 || !sourceOwned(dir) {
		return nil, spool.ErrConflict
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, net.ErrClosed
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 || !sourceOwned(info) {
		return nil, spool.ErrConflict
	}
	raw, err := new(net.Dialer).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = raw.Close()
		}
	}()
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) {
		return nil, net.ErrClosed
	}
	conn, isUnix := raw.(*net.UnixConn)
	if !isUnix {
		return nil, spool.ErrConflict
	}
	fd, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var peerErr error
	err = fd.Control(func(n uintptr) {
		uid, e := sourcePeerUID(int(n))
		peerErr = e
		if e == nil && uid != uint32(os.Geteuid()) {
			peerErr = spool.ErrConflict
		}
	})
	if err = errors.Join(err, peerErr); err != nil {
		return nil, err
	}
	ok = true
	return raw, nil
}
