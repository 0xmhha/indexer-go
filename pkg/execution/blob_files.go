package execution

import (
	"context"
	"crypto/sha256"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"io"
	"os"
	"path/filepath"
)

type BlobAttachment struct {
	Ref    execwire.Ref  `json:"ref"`
	Digest execwire.Hash `json:"digest"`
	Bytes  int64         `json:"bytes"`
}

func readPrivateBlobFile(ctx context.Context, path string, max int64) ([]byte, os.FileInfo, error) {
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	if filepath.Clean(path) != path {
		return nil, nil, port.ErrInvalidData
	}
	info, e := privateSubmitPath(path, false)
	if e != nil {
		return nil, nil, e
	}
	if info.Size() <= 0 || info.Size() > max {
		return nil, nil, execwire.ErrLimit
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, nil, e
	}
	defer func() { _ = f.Close() }()
	opened, e := f.Stat()
	if e != nil {
		return nil, nil, e
	}
	if !os.SameFile(info, opened) {
		return nil, nil, port.ErrExecutionConflict
	}
	data, e := io.ReadAll(io.LimitReader(f, max+1))
	if e != nil {
		return nil, nil, e
	}
	if len(data) != int(info.Size()) || !privateBlobUnchanged(path, info) {
		return nil, nil, port.ErrExecutionConflict
	}
	if e = ctx.Err(); e != nil {
		return nil, nil, e
	}
	return data, info, nil
}
func privateBlobUnchanged(path string, info os.FileInfo) bool {
	now, e := privateSubmitPath(path, false)
	return e == nil && os.SameFile(info, now) && info.Size() == now.Size() && info.ModTime().Equal(now.ModTime())
}

// AttachBlobFiles publishes a NEW private immutable EXR1. It starts no service,
// rewrites no source/generation, obtains no ACK and grants no deletion rights.
type InputAttachment = BlobAttachment

func AttachBlobFiles(ctx context.Context, p chains.BinaryProfile, chain execwire.Chain, input, sidecars, output, provider string) (out BlobAttachment, err error) {
	return composePrivateInput(ctx, input, sidecars, output, execwire.MaxBlobResponseBytes, func(payload, response []byte) ([]byte, error) {
		return AttachBlobInput(ctx, p, chain, payload, provider, response)
	})
}
func composePrivateInput(ctx context.Context, input, sidecars, output string, responseLimit int64, compose func([]byte, []byte) ([]byte, error)) (out InputAttachment, err error) {
	if e := ctx.Err(); e != nil {
		return out, e
	}
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return out, port.ErrInvalidData
	}
	dir := filepath.Dir(output)
	parent, e := os.Lstat(dir)
	if e != nil {
		return out, e
	}
	if !parent.IsDir() || parent.Mode().Perm() != 0700 || !executionOwned(parent) {
		return out, port.ErrExecutionConflict
	}
	if _, e = os.Lstat(output); e == nil {
		return out, os.ErrExist
	} else if !os.IsNotExist(e) {
		return out, e
	}
	payload, inputInfo, e := readPrivateBlobFile(ctx, input, execwire.MaxBlockBytes)
	if e != nil {
		return out, e
	}
	var response []byte
	var sidecarInfo os.FileInfo
	if sidecars != "" {
		response, sidecarInfo, e = readPrivateBlobFile(ctx, sidecars, responseLimit)
		if e != nil {
			return out, e
		}
	}
	data, e := compose(payload, response)
	if e != nil {
		return out, e
	}
	result, e := execwire.UnmarshalResult(data)
	if e != nil {
		return out, e
	}
	if !privateBlobUnchanged(input, inputInfo) || sidecarInfo != nil && !privateBlobUnchanged(sidecars, sidecarInfo) {
		return out, port.ErrExecutionConflict
	}
	f, e := os.CreateTemp(dir, ".blob-input-*")
	if e != nil {
		return out, e
	}
	tmp := f.Name()
	info, e := f.Stat()
	if e != nil {
		_ = f.Close()
		return out, e
	}
	removeTemp := func() {
		if current, e := os.Lstat(tmp); e == nil && os.SameFile(info, current) {
			_ = os.Remove(tmp)
		}
	}
	defer removeTemp()
	defer func() { _ = f.Close() }()
	if _, e = f.Write(data); e != nil {
		return out, e
	}
	if e = f.Sync(); e != nil {
		return out, e
	}
	if e = f.Close(); e != nil {
		return out, e
	}
	if e = ctx.Err(); e != nil {
		return out, e
	}
	current, e := os.Lstat(dir)
	if e != nil {
		return out, e
	}
	if !os.SameFile(parent, current) {
		return out, port.ErrExecutionConflict
	}
	// link is an atomic no-replace publish; both names refer to the fsynced inode.
	if e = os.Link(tmp, output); e != nil {
		return out, e
	}
	removeTemp()
	published, e := privateSubmitPath(output, false)
	if e != nil {
		return out, e
	}
	if !os.SameFile(info, published) {
		return out, port.ErrExecutionConflict
	}
	directory, e := os.Open(dir)
	if e != nil {
		return out, e
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return out, syncErr
	}
	if closeErr != nil {
		return out, closeErr
	}
	if e = ctx.Err(); e != nil {
		return out, e
	}
	return BlobAttachment{Ref: result.Ref, Digest: execwire.Hash(sha256.Sum256(data)), Bytes: int64(len(data))}, nil
}
