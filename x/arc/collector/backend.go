// Package collector connects the official read-only Arc replay adapter to the
// shared durable spool and indexer supervision. It never starts a node.
package collector

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/x/arc"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
)

const arcRevision = "6e764023ee6515fe70573e123ed2db912a7207b4"

var ErrNative = errors.New("arc collector: invalid or unavailable native adapter")

type Options struct {
	Executable, DataDir string
	ScratchDir          string
	Chain               execwire.Chain
	Timeout             time.Duration
}

type Backend struct {
	chain       execwire.Chain
	input       io.WriteCloser
	output      io.ReadCloser
	cancel      context.CancelFunc
	done        chan struct{}
	gate        chan struct{}
	sequence    uint64
	timeout     time.Duration
	stderr      boundedError
	work        string
	workInfo    os.FileInfo
	cleanup     sync.Once
	closeErr    error
	nativeLease *os.File
}

type boundedError struct {
	mu    sync.Mutex
	bytes []byte
}

func (b *boundedError) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if remaining := 4096 - len(b.bytes); remaining > 0 {
		b.bytes = append(b.bytes, p[:min(len(p), remaining)]...)
	}
	return len(p), nil
}
func (b *boundedError) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.bytes) }

type nativeRequest struct {
	Version    uint8  `json:"version"`
	ID         uint64 `json:"id"`
	Op         string `json:"op"`
	Height     uint64 `json:"height"`
	Generation uint64 `json:"generation"`
}
type nativeIdentity struct {
	ChainID  string        `json:"chain_id"`
	Genesis  execwire.Hash `json:"genesis"`
	Revision string        `json:"revision"`
}
type nativeHead struct {
	Height uint64        `json:"height"`
	Hash   execwire.Hash `json:"hash"`
}

// Start owns only this read-only child. Native storage is never initialized.
func Start(parent context.Context, o Options) (*Backend, error) {
	chain := map[string]string{"5042": "mainnet", "5042002": "testnet"}[o.Chain.ID]
	if chain == "" || !filepath.IsAbs(o.Executable) || !filepath.IsAbs(o.DataDir) {
		return nil, ErrNative
	}
	if err := execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: o.Chain, Ref: execwire.Ref{Generation: 1, Hash: o.Chain.Genesis}}}); err != nil {
		return nil, err
	}
	info, err := os.Lstat(o.Executable)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
		return nil, ErrNative
	}
	if o.ScratchDir != "" && !filepath.IsAbs(o.ScratchDir) {
		return nil, ErrNative
	}
	o.DataDir, err = filepath.EvalSymlinks(o.DataDir)
	if err != nil {
		return nil, err
	}
	lease, err := nativestore.AcquireRun(o.DataDir)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			_ = lease.Close()
		}
	}()
	work, err := os.MkdirTemp(o.ScratchDir, ".arc-native-")
	if err != nil {
		return nil, err
	}
	workInfo, err := os.Lstat(work)
	if err != nil {
		_ = os.Remove(work)
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, o.Executable, "--chain", chain, "--datadir", o.DataDir, "--scratch-dir", work, "--serve")
	b, err := startBackend(ctx, cancel, cmd, o.Chain, o.Timeout)
	if err != nil {
		return nil, errors.Join(err, removeScratch(work, workInfo))
	}
	b.work, b.workInfo = work, workInfo
	b.nativeLease = lease
	transferred = true
	return b, nil
}

func removeScratch(path string, expected os.FileInfo) error {
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || !os.SameFile(info, expected) {
		return ErrNative
	}
	return os.RemoveAll(path)
}

func startBackend(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, chain execwire.Chain, timeout time.Duration) (_ *Backend, err error) {
	defer func() {
		if err != nil {
			cancel()
		}
	}()
	if timeout == 0 {
		timeout = time.Minute
	}
	if timeout <= 0 || timeout > time.Hour {
		return nil, execwire.ErrLimit
	}
	b := &Backend{chain: chain, cancel: cancel, done: make(chan struct{}), gate: make(chan struct{}, 1), timeout: timeout}
	b.gate <- struct{}{}
	if b.input, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}
	if b.output, err = cmd.StdoutPipe(); err != nil {
		_ = b.input.Close()
		return nil, err
	}
	// The adapter reports no secrets and its errors remain bounded in memory.
	cmd.Stderr = &b.stderr
	if err = cmd.Start(); err != nil {
		_ = b.input.Close()
		_ = b.output.Close()
		return nil, err
	}
	go func() { _ = cmd.Wait(); close(b.done) }()
	p, err := b.query(ctx, "identity", 0, 0)
	if err != nil {
		_ = b.Close()
		return nil, err
	}
	var identity nativeIdentity
	if decode(p, &identity) != nil || identity.ChainID != chain.ID || identity.Genesis != chain.Genesis || identity.Revision != arcRevision {
		_ = b.Close()
		return nil, ErrNative
	}
	return b, nil
}

func (b *Backend) Close() error {
	b.cancel()
	_ = b.input.Close()
	_ = b.output.Close()
	<-b.done
	b.cleanup.Do(func() {
		b.closeErr = removeScratch(b.work, b.workInfo)
		if b.nativeLease != nil {
			b.closeErr = errors.Join(b.closeErr, b.nativeLease.Close())
		}
	})
	return b.closeErr
}
func (b *Backend) reject(err error) error {
	closeErr := b.Close()
	return fmt.Errorf("%w: %w; adapter: %s", ErrNative, errors.Join(err, closeErr), b.stderr.String())
}

func operationCode(op string) uint8 {
	return map[string]uint8{"identity": 1, "head": 2, "canonical": 3, "replay": 4}[op]
}

func (b *Backend) query(parent context.Context, op string, height, generation uint64) ([]byte, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, b.timeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.done:
		return nil, ErrNative
	case <-b.gate:
	}
	defer func() { b.gate <- struct{}{} }()
	select {
	case <-b.done:
		return nil, ErrNative
	default:
	}
	if height > math.MaxInt64 || generation > math.MaxInt64 {
		return nil, execwire.ErrLimit
	}
	b.sequence++
	request := nativeRequest{Version: 1, ID: b.sequence, Op: op, Height: height, Generation: generation}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = b.Close(); close(stopped) })
	defer func() {
		if !stop() {
			<-stopped
		}
	}()
	failIO := func(err error) ([]byte, error) {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, b.reject(err)
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
	if _, err = io.Copy(b.input, bytes.NewReader(append(length[:], payload...))); err != nil {
		return failIO(err)
	}
	var header [17]byte
	if _, err = io.ReadFull(b.output, header[:]); err != nil {
		return failIO(err)
	}
	size := binary.BigEndian.Uint32(header[13:])
	limit := uint32(1024)
	if op == "replay" {
		limit = execwire.MaxBlockBytes
	}
	if string(header[:4]) != "AXS1" || binary.BigEndian.Uint64(header[4:12]) != request.ID || header[12] != operationCode(op) || size == 0 || size > limit {
		return nil, b.reject(ErrNative)
	}
	result := make([]byte, size)
	if _, err = io.ReadFull(b.output, result); err != nil {
		return failIO(err)
	}
	if err = ctx.Err(); err != nil {
		return failIO(err)
	}
	return result, nil
}

func decode(p []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(p))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return ErrNative
	}
	return nil
}
func (b *Backend) head(ctx context.Context, op string, height uint64) (nativeHead, error) {
	p, err := b.query(ctx, op, height, 0)
	if err != nil {
		return nativeHead{}, err
	}
	var h nativeHead
	if decode(p, &h) != nil || h.Height > math.MaxInt64 || h.Hash == (execwire.Hash{}) || op == "canonical" && h.Height != height {
		return h, b.reject(ErrNative)
	}
	return h, nil
}
func (b *Backend) Head(ctx context.Context) (execwire.Ref, error) {
	h, err := b.head(ctx, "head", 0)
	return execwire.Ref{Height: h.Height, Hash: h.Hash}, err
}
func (b *Backend) Canonical(ctx context.Context, height uint64) (execwire.Hash, error) {
	h, err := b.head(ctx, "canonical", height)
	return h.Hash, err
}
func (b *Backend) Replay(ctx context.Context, height, generation uint64) (execwire.Result, error) {
	if height == 0 || generation == 0 {
		return execwire.Result{}, execwire.ErrLimit
	}
	p, err := b.query(ctx, "replay", height, generation)
	if err != nil {
		return execwire.Result{}, err
	}
	// The chain-specific Go decoder independently authenticates the raw block
	// and receipt commitments before any shared spool append can make it durable.
	prepared, err := execution.Prepare(arc.New(), b.chain, p)
	if err != nil {
		return execwire.Result{}, b.reject(err)
	}
	r, err := execwire.UnmarshalResult(p)
	if err != nil || prepared.Ref().Height != height || prepared.Ref().Generation != generation {
		return execwire.Result{}, b.reject(ErrNative)
	}
	return r, nil
}
