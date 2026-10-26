package execution

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"io"
	"net"
	"os"
	"path/filepath"
)

// SubmitFile retains its owned private input on every success/error. It starts
// no application and authenticates the existing private same-UID Unix peer.
func SubmitFile(ctx context.Context, profile chains.BinaryProfile, chain execwire.Chain, path, socket string) (execwire.Frame, error) {
	if err := ctx.Err(); err != nil {
		return execwire.Frame{}, err
	}
	info, err := privateSubmitPath(path, false)
	if err != nil {
		return execwire.Frame{}, err
	}
	if info.Size() <= 0 || info.Size() > execwire.MaxBlockBytes {
		return execwire.Frame{}, execwire.ErrLimit
	}
	f, err := os.Open(path)
	if err != nil {
		return execwire.Frame{}, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil {
		return execwire.Frame{}, err
	}
	if !os.SameFile(info, opened) {
		return execwire.Frame{}, port.ErrExecutionConflict
	}
	payload, err := io.ReadAll(io.LimitReader(f, execwire.MaxBlockBytes+1))
	if err != nil {
		return execwire.Frame{}, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return execwire.Frame{}, err
	}
	if !os.SameFile(info, current) || len(payload) != int(info.Size()) || !info.ModTime().Equal(current.ModTime()) {
		return execwire.Frame{}, port.ErrExecutionConflict
	}
	if profile == nil {
		return execwire.Frame{}, port.ErrInvalidData
	}
	p, err := PrepareContext(ctx, profile, chain, payload, false)
	if err != nil {
		return execwire.Frame{}, err
	}
	info, err = privateSubmitPath(socket, true)
	if err != nil {
		return execwire.Frame{}, err
	}
	raw, err := new(net.Dialer).DialContext(ctx, "unix", socket)
	if err != nil {
		return execwire.Frame{}, err
	}
	defer func() { _ = raw.Close() }()
	current, err = os.Lstat(socket)
	if err != nil {
		return execwire.Frame{}, err
	}
	if !os.SameFile(info, current) {
		return execwire.Frame{}, port.ErrExecutionConflict
	}
	conn, ok := raw.(*net.UnixConn)
	if !ok {
		return execwire.Frame{}, port.ErrInvalidData
	}
	if err := verifyExecutionPeer(conn); err != nil {
		return execwire.Frame{}, err
	}
	return submitPrepared(ctx, raw, chain, payload, p)
}

func privateSubmitPath(path string, socket bool) (os.FileInfo, error) {
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
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	validKind := info.Mode().IsRegular()
	if socket {
		validKind = info.Mode()&os.ModeSocket != 0
	}
	if !validKind || info.Mode().Perm() != 0600 || !executionOwned(info) {
		return nil, port.ErrExecutionConflict
	}
	return info, nil
}
