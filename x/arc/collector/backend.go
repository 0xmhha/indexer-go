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
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/x/arc"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
)

const arcRevision = "6e764023ee6515fe70573e123ed2db912a7207b4"

var ErrNative = errors.New("arc collector: invalid or unavailable native adapter")

type Options struct {
	Executable, DataDir string
	ScratchDir          string
	Chain               execwire.Chain
	Timeout             time.Duration
	StateProofs         *stateproof.Selection
	Managed             *nativeinput.Identity
}

type Backend struct {
	requiredStateProofs *stateproof.Selection
	chain               execwire.Chain
	input               io.WriteCloser
	output              io.ReadCloser
	cancel              context.CancelFunc
	done                chan struct{}
	gate                chan struct{}
	sequence            uint64
	timeout             time.Duration
	stderr              boundedError
	work                string
	workInfo            os.FileInfo
	cleanup             sync.Once
	closeErr            error
	nativeLease         *os.File
	readView            *nativestore.ReadView
	lifetime            context.Context
	lifeMu              sync.Mutex
	closed              bool
	operations          sync.WaitGroup
	operationGate       chan struct{}
	managed             *nativeinput.Identity
	dataDir             string
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
	Version     uint8                 `json:"version"`
	ID          uint64                `json:"id"`
	Op          string                `json:"op"`
	Height      uint64                `json:"height"`
	Generation  uint64                `json:"generation"`
	StateProofs *stateproof.Selection `json:"state_proofs,omitempty"`
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
	selection, _, err := proofPolicy(o.StateProofs)
	if err != nil {
		return nil, err
	}
	o.StateProofs = selection
	if o.Timeout < 0 || o.Timeout > time.Hour {
		return nil, execwire.ErrLimit
	}
	if o.Managed != nil {
		identity := *o.Managed
		if nativeinput.ValidIdentity(identity) != nil || o.Chain.ID != "5042002" || identity.Genesis != o.Chain.Genesis {
			return nil, ErrNative
		}
		o.Managed = &identity
	}
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
	var lease *os.File
	var view *nativestore.ReadView
	var startup *nativestore.ViewLease
	transferred := false
	defer func() {
		if !transferred {
			if startup != nil {
				_ = startup.Close()
			}
			if view != nil {
				_ = view.Close()
			}
			if lease != nil {
				_ = lease.Close()
			}
		}
	}()
	if o.Managed == nil {
		lease, err = nativestore.AcquireRun(o.DataDir)
	} else {
		view, err = nativestore.OpenReadView(parent, o.DataDir)
		if err == nil {
			timeout := o.Timeout
			if timeout == 0 {
				timeout = time.Minute
			}
			call, stop := context.WithTimeout(parent, timeout)
			startup, err = view.Acquire(call)
			stop()
		}
	}
	if err != nil {
		return nil, err
	}
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
	cmd.Env = nativeinput.ChildEnvironment()
	if view != nil {
		cmd.Args = append(cmd.Args, "--managed-view-fd", "3")
		cmd.ExtraFiles = []*os.File{view.File()}
	}
	b, err := startOwnedBackend(ctx, cancel, cmd, o.Chain, o.Timeout, &backendOwnership{work: work, info: workInfo, lease: lease, view: view, startup: startup, identity: o.Managed, dataDir: o.DataDir})
	if err != nil {
		return nil, errors.Join(err, removeScratch(work, workInfo))
	}
	b.requiredStateProofs = selection
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

type backendOwnership struct {
	work     string
	info     os.FileInfo
	lease    *os.File
	view     *nativestore.ReadView
	startup  *nativestore.ViewLease
	identity *nativeinput.Identity
	dataDir  string
}

func startBackend(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, chain execwire.Chain, timeout time.Duration) (*Backend, error) {
	return startOwnedBackend(ctx, cancel, cmd, chain, timeout, nil)
}
func startOwnedBackend(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, chain execwire.Chain, timeout time.Duration, owner *backendOwnership) (_ *Backend, err error) {
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
	b := &Backend{chain: chain, cancel: cancel, lifetime: ctx, done: make(chan struct{}), gate: make(chan struct{}, 1), operationGate: make(chan struct{}, 1), timeout: timeout}
	b.operationGate <- struct{}{}
	// Install ownership before any child/handshake cancellation can call Close.
	if owner != nil {
		b.work, b.workInfo, b.nativeLease = owner.work, owner.info, owner.lease
		b.readView, b.managed, b.dataDir = owner.view, owner.identity, owner.dataDir
	}
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
	var identity nativeIdentity
	if err == nil && (decode(p.payload, &identity) != nil || identity.ChainID != chain.ID || identity.Genesis != chain.Genesis || identity.Revision != arcRevision) {
		err = ErrNative
	}
	if owner != nil && owner.startup != nil {
		if err == nil {
			err = owner.startup.Validate()
		}
		// An interrupted startup joins the inherited child BEFORE SH is released.
		if err != nil {
			b.interrupt()
			<-b.done
		}
		err = errors.Join(err, owner.startup.Close())
	}
	if err != nil {
		return nil, fmt.Errorf("arc native startup identity: %w; adapter: %s", errors.Join(err, b.Close()), b.stderr.String())
	}
	return b, nil
}

func (b *Backend) Close() error {
	b.lifeMu.Lock()
	b.closed = true
	b.lifeMu.Unlock()
	b.interrupt()
	<-b.done
	b.operations.Wait()
	b.cleanup.Do(func() {
		b.closeErr = removeScratch(b.work, b.workInfo)
		if b.readView != nil {
			b.closeErr = errors.Join(b.closeErr, b.readView.Close())
		}
		if b.nativeLease != nil {
			b.closeErr = errors.Join(b.closeErr, b.nativeLease.Close())
		}
	})
	return b.closeErr
}
func (b *Backend) interrupt() {
	b.cancel()
	_ = b.input.Close()
	_ = b.output.Close()
}
func (b *Backend) reject(err error) error {
	closeErr := b.Close()
	return fmt.Errorf("%w: %w; adapter: %s", ErrNative, errors.Join(err, closeErr), b.stderr.String())
}

func operationCode(op string) uint8 {
	return map[string]uint8{"identity": 1, "head": 2, "canonical": 3, "replay": 4, "replayproofs": 5}[op]
}

type nativeResponse struct {
	payload []byte
	proof   nativeinput.Proof
}

func (b *Backend) query(parent context.Context, op string, height, generation uint64) (nativeResponse, error) {
	if err := parent.Err(); err != nil {
		return nativeResponse{}, err
	}
	ctx, cancel := context.WithTimeout(parent, b.timeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return nativeResponse{}, ctx.Err()
	case <-b.done:
		return nativeResponse{}, ErrNative
	case <-b.gate:
	}
	defer func() { b.gate <- struct{}{} }()
	select {
	case <-b.done:
		return nativeResponse{}, ErrNative
	default:
	}
	if height > math.MaxInt64 || generation > math.MaxInt64 {
		return nativeResponse{}, execwire.ErrLimit
	}
	b.sequence++
	request := nativeRequest{Version: 1, ID: b.sequence, Op: op, Height: height, Generation: generation}
	if op == "replayproofs" {
		request.StateProofs = b.requiredStateProofs
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nativeResponse{}, err
	}
	limitRequest := 1024
	if op == "replayproofs" {
		limitRequest = 16 << 10
	}
	if len(payload) > limitRequest {
		return nativeResponse{}, execwire.ErrLimit
	}
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { b.interrupt(); close(stopped) })
	defer func() {
		if !stop() {
			<-stopped
		}
	}()
	failIO := func(err error) (nativeResponse, error) {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nativeResponse{}, err
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
	var proof nativeinput.Proof
	magic := "AXS1"
	if b.managed != nil {
		magic = "AXM1"
	}
	if string(header[:4]) != magic || binary.BigEndian.Uint64(header[4:12]) != request.ID || header[12] != operationCode(op) {
		return nativeResponse{}, ErrNative
	}
	if b.managed != nil {
		if size == 0 || size > nativeinput.MaxMeta {
			return nativeResponse{}, ErrNative
		}
		var length [4]byte
		if _, err = io.ReadFull(b.output, length[:]); err != nil {
			return failIO(err)
		}
		metadata := make([]byte, size)
		if _, err = io.ReadFull(b.output, metadata); err != nil {
			return failIO(err)
		}
		if nativeinput.Decode(metadata, &proof) != nil {
			return nativeResponse{}, ErrNative
		}
		size = binary.BigEndian.Uint32(length[:])
	}
	limit := uint32(1024)
	if op == "replay" || op == "replayproofs" {
		limit = execwire.MaxBlockBytes
	}
	if size == 0 || size > limit {
		return nativeResponse{}, ErrNative
	}
	result := make([]byte, size)
	if _, err = io.ReadFull(b.output, result); err != nil {
		return failIO(err)
	}
	if err = ctx.Err(); err != nil {
		return failIO(err)
	}
	if b.managed != nil {
		if nativeinput.VerifyCurrent(b.dataDir, proof, *b.managed) != nil {
			return nativeResponse{}, ErrNative
		}
		if op == "identity" || op == "head" {
			if proof.Ref != proof.Head.Tip {
				return nativeResponse{}, ErrNative
			}
		} else if proof.Ref.Height != height {
			return nativeResponse{}, ErrNative
		}
	}
	return nativeResponse{payload: result, proof: proof}, nil
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
	var h nativeHead
	err := b.operation(ctx, func(call context.Context) error {
		p, err := b.query(call, op, height, 0)
		if err != nil {
			return err
		}
		if decode(p.payload, &h) != nil || h.Height > math.MaxInt64 || h.Hash == (execwire.Hash{}) || op == "canonical" && h.Height != height {
			return ErrNative
		}
		if b.managed != nil && (h.Height != p.proof.Ref.Height || h.Hash != p.proof.Ref.Hash) {
			return ErrNative
		}
		return nil
	})
	return h, err
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
	op := "replay"
	if b.requiredStateProofs != nil {
		op = "replayproofs"
	}
	var r execwire.Result
	err := b.operation(ctx, func(call context.Context) error {
		p, err := b.query(call, op, height, generation)
		if err != nil {
			return err
		}
		// Keep the managed view through ALL independent Go/proof validation.
		prepared, err := execution.PrepareWithRequirements(call, arc.New(), b.chain, p.payload, execution.Requirements{StateProofs: b.requiredStateProofs})
		if err != nil {
			return err
		}
		r, err = execwire.UnmarshalResult(p.payload)
		if err != nil || prepared.Ref().Height != height || prepared.Ref().Generation != generation || b.requiredStateProofs != nil && (r.StateProofBundle == nil || r.StateProofBundle.Provider != "arc-native") {
			return ErrNative
		}
		if b.managed != nil {
			return nativeinput.ValidatePayload(p.proof, r)
		}
		return nil
	})
	return r, err
}
