// Package nativeops connects the pinned offline Arc adapter to E3 supervision.
// It never starts a node or adds RPC clients.
package nativeops

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type Native struct {
	Executable string
	Chain      execwire.Chain
	Timeout    time.Duration
	AnchorRoot execwire.Hash
}
type Observation struct {
	Head             execwire.Ref
	Root, AnchorRoot execwire.Hash
}
type apply struct {
	Request      retention.Request
	PermitDigest execwire.Hash
}
type bounded struct {
	bytes.Buffer
	limit int
}

func (b *bounded) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, execwire.ErrLimit
	}
	return b.Buffer.Write(p)
}
func (n Native) start(parent context.Context, path, operation string, input any) (*exec.Cmd, *childLifetime, error) {
	chain := map[string]string{"5042": "mainnet", "5042002": "testnet"}[n.Chain.ID]
	if chain == "" || !filepath.IsAbs(n.Executable) || !filepath.IsAbs(path) {
		return nil, nil, retention.ErrUnsafe
	}
	info, e := os.Lstat(n.Executable)
	if e != nil {
		return nil, nil, e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
		return nil, nil, retention.ErrUnsafe
	}
	timeout := n.Timeout
	if timeout == 0 {
		timeout = time.Hour
	}
	if timeout <= 0 || timeout > time.Hour {
		return nil, nil, execwire.ErrLimit
	}
	var encoded []byte
	if input != nil {
		encoded, e = json.Marshal(input)
		if e != nil || len(encoded) > retention.MaxArtifact {
			return nil, nil, retention.ErrUnsafe
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	work, e := os.MkdirTemp(filepath.Dir(path), ".arc-maintenance-")
	if e != nil {
		cancel()
		return nil, nil, e
	}
	workInfo, e := os.Lstat(work)
	if e != nil {
		cancel()
		return nil, nil, e
	}
	life := &childLifetime{cancel: cancel, path: work, info: workInfo}
	cmd := exec.CommandContext(ctx, n.Executable, "--chain", chain, "--datadir", path, "--operation", operation, "--scratch-dir", work)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "TZ=" + os.Getenv("TZ"), "TMPDIR=" + os.Getenv("TMPDIR")}
	cmd.WaitDelay = 2 * time.Second
	if input != nil {
		cmd.Stdin = bytes.NewReader(append(encoded, '\n'))
	}
	return cmd, life, nil
}
func (n Native) run(ctx context.Context, path, operation string, input, out any) (err error) {
	cmd, life, e := n.start(ctx, path, operation, input)
	if e != nil {
		return e
	}
	defer func() { err = errors.Join(err, life.Close()) }()
	stdout := &bounded{limit: 64 << 10}
	stderr := &bounded{limit: 4096}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if e = cmd.Run(); e != nil {
		return fmt.Errorf("arc native %s: %w: %s", operation, e, stderr.String())
	}
	dec := json.NewDecoder(&stdout.Buffer)
	dec.DisallowUnknownFields()
	if e = dec.Decode(out); e != nil {
		return e
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return retention.ErrUnsafe
	}
	return ctx.Err()
}
func (n Native) Inspect(ctx context.Context, path string, r retention.Request) (Observation, error) {
	var o Observation
	e := n.run(ctx, path, "inspect", r, &o)
	if e == nil && (o.Head.Generation != 0 || o.Head.Hash == (execwire.Hash{}) || o.Root == (execwire.Hash{}) || o.AnchorRoot == (execwire.Hash{}) || o.Head.Height < r.Anchor.Height) {
		e = retention.ErrUnsafe
	}
	return o, e
}
func (n Native) Prune(ctx context.Context, path string, p retention.Permit) error {
	return n.apply(ctx, path, "prune", p)
}
func (n Native) Verify(ctx context.Context, path string, p retention.Permit) error {
	return n.apply(ctx, path, "verify", p)
}
func (n Native) apply(ctx context.Context, path, operation string, p retention.Permit) error {
	if p.Validate() != nil || p.Request.Chain != n.Chain {
		return retention.ErrUnsafe
	}
	digest, e := retention.Digest(p)
	if e != nil {
		return e
	}
	var reply struct{ Status string }
	if e = n.run(ctx, path, operation, apply{p.Request, digest}, &reply); e != nil {
		return e
	}
	if reply.Status != "native_verified" {
		return retention.ErrUnsafe
	}
	return nil
}

type held struct {
	input  io.WriteCloser
	output io.ReadCloser
	cmd    *exec.Cmd
	life   *childLifetime
	once   sync.Once
	err    error
}

func (h *held) Close() error {
	h.once.Do(func() {
		_ = h.input.Close()
		h.err = h.cmd.Wait()
		_ = h.output.Close()
		h.err = errors.Join(h.err, h.life.Close())
	})
	return h.err
}
func (n Native) Lock(ctx context.Context, path string, r retention.Request) (io.Closer, error) {
	if r.Validate() != nil || r.Chain != n.Chain {
		return nil, retention.ErrUnsafe
	}
	cmd, life, e := n.start(ctx, path, "hold", nil)
	if e != nil {
		return nil, e
	}
	input, e := cmd.StdinPipe()
	if e != nil {
		return nil, errors.Join(e, life.Close())
	}
	output, e := cmd.StdoutPipe()
	if e != nil {
		_ = input.Close()
		return nil, errors.Join(e, life.Close())
	}
	stderr := &bounded{limit: 4096}
	cmd.Stderr = stderr
	if e = cmd.Start(); e != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, errors.Join(e, life.Close())
	}
	h := &held{input: input, output: output, cmd: cmd, life: life}
	failed := func(e error) (io.Closer, error) { life.cancel(); return nil, errors.Join(e, h.Close()) }
	if e = json.NewEncoder(input).Encode(r); e != nil {
		return failed(e)
	}
	line, e := bufio.NewReader(io.LimitReader(output, (64<<10)+1)).ReadBytes('\n')
	if e != nil || len(line) > 64<<10 {
		return failed(retention.ErrUnsafe)
	}
	var o Observation
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if dec.Decode(&o) != nil || o.Head != r.Head || o.Root != r.HeadRoot || o.AnchorRoot == (execwire.Hash{}) || n.AnchorRoot != (execwire.Hash{}) && o.AnchorRoot != n.AnchorRoot {
		return failed(retention.ErrUnsafe)
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return failed(retention.ErrUnsafe)
	}
	return h, nil
}

// Close runs only after the owned native child joins. Never remove a replaced
// directory or follow its internal RocksDB primary symlink into native storage.
type childLifetime struct {
	cancel context.CancelFunc
	path   string
	info   os.FileInfo
}

func (l *childLifetime) Close() error {
	l.cancel()
	info, e := os.Lstat(l.path)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || !os.SameFile(info, l.info) {
		return retention.ErrUnsafe
	}
	return os.RemoveAll(l.path)
}
