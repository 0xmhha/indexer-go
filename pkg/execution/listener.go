package execution

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

type executionListener struct {
	*net.UnixListener
	path string
	info os.FileInfo
	once sync.Once
	err  error
}

func (l *executionListener) Close() error {
	l.once.Do(func() {
		l.err = l.UnixListener.Close()
		info, err := os.Lstat(l.path)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			l.err = errors.Join(l.err, err)
			return
		}
		if !os.SameFile(l.info, info) || info.Mode()&os.ModeSocket == 0 {
			l.err = errors.Join(l.err, port.ErrExecutionConflict)
			return
		}
		l.err = errors.Join(l.err, os.Remove(l.path))
	})
	return l.err
}
func listenExecution(path string) (*executionListener, error) {
	return listenExecutionReady(path, nil)
}
func listenExecutionReady(path string, beforePublish func() error) (*executionListener, error) {
	if !filepath.IsAbs(path) {
		return nil, port.ErrInvalidData
	}
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if !dir.IsDir() || dir.Mode().Perm() != 0700 || !executionOwned(dir) {
		return nil, port.ErrExecutionConflict
	}
	if _, err = os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return nil, port.ErrExecutionConflict
	}
	// Bind and set permissions privately. Publishing the final pathname only
	// after listen is ready prevents clients observing partial initialization.
	tmpDir, err := os.MkdirTemp(filepath.Dir(path), ".e-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmpDir) }()
	tmp := filepath.Join(tmpDir, "s")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: tmp, Net: "unix"})
	if err != nil {
		return nil, err
	}
	l.SetUnlinkOnClose(false)
	info, err := os.Lstat(tmp)
	if err != nil {
		_ = l.Close()
		return nil, err
	}
	owned := &executionListener{UnixListener: l, path: tmp, info: info}
	if !executionOwned(info) || info.Mode()&os.ModeSocket == 0 {
		_ = owned.Close()
		return nil, port.ErrExecutionConflict
	}
	if err = os.Chmod(tmp, 0600); err != nil {
		_ = owned.Close()
		return nil, err
	}
	if beforePublish != nil {
		if err = beforePublish(); err != nil {
			_ = owned.Close()
			return nil, err
		}
	}
	if err = publishExecutionSocket(tmp, path); err != nil {
		_ = owned.Close()
		return nil, err
	}
	owned.path = path
	return owned, nil
}
func verifyExecutionPeer(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var peerErr error
	err = raw.Control(func(fd uintptr) {
		uid, e := executionPeerUID(int(fd))
		peerErr = e
		if e == nil && uid != uint32(os.Geteuid()) {
			peerErr = port.ErrExecutionConflict
		}
	})
	return errors.Join(err, peerErr)
}

// Run owns only its newly created private socket. Existing paths are never
// replaced. It serializes authenticated sessions and joins them before return.
func (r *Receiver) Run(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.Profile == nil || r.Store == nil || r.OpenController == nil || r.Confirm == nil {
		return port.ErrInvalidData
	}
	l, err := listenExecution(path)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = l.Close(); close(done) })
	defer func() {
		if !stop() {
			<-done
		}
		_ = l.Close()
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.OnReady != nil {
		if err := r.OnReady(); err != nil {
			return err
		}
	}
	for {
		conn, err := l.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if err = verifyExecutionPeer(conn); err != nil {
			_ = conn.Close()
			if r.OnSessionError != nil {
				r.OnSessionError(err)
			}
			continue
		}
		err = r.serveConn(ctx, conn)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			continue
		}
		if r.OnSessionError != nil {
			r.OnSessionError(err)
		}
		retry := errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, port.ErrExecutionRewinding) || errors.Is(err, port.ErrExecutionStale) || errors.Is(err, port.ErrExecutionCanonicalLag)
		if !retry && (r.RetrySession == nil || !r.RetrySession(err)) {
			return err
		}
	}
}
