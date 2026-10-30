package arcingress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativeops"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type maintenanceMock struct {
	ackDigest execwire.Hash
	*rawGCMock
	permit                    *retention.Permit
	receipt                   *retention.Receipt
	active                    bool
	beginCalls, completeCalls int
}

func (s *maintenanceMock) ExecutionRetentionStatus(context.Context) (retention.Permit, bool, *retention.Receipt, error) {
	if s.permit == nil {
		return retention.Permit{}, false, nil, port.ErrNotFound
	}
	return *s.permit, s.receipt == nil, s.receipt, nil
}
func (s *maintenanceMock) BeginExecutionRetention(ctx context.Context, r retention.Request, fresh func(context.Context) error) (retention.Permit, error) {
	s.beginCalls++
	if err := fresh(ctx); err != nil {
		return retention.Permit{}, err
	}
	if s.permit != nil && s.receipt == nil {
		if s.permit.Request != r {
			return retention.Permit{}, port.ErrExecutionConflict
		}
		return *s.permit, nil
	}
	p := retention.Permit{Coverage: "full", BaselineDigest: execwire.Hash{11}, Version: 1, Token: execwire.Hash{byte(s.beginCalls)}, Request: r, Visible: r.ACK, Root: r.HeadRoot}
	s.permit = &p
	s.receipt = nil
	s.control.Maintenance = true
	return p, nil
}
func (s *maintenanceMock) WithExecutionRetention(ctx context.Context, p retention.Permit, f func(context.Context) error) error {
	if s.active || s.permit == nil || *s.permit != p || s.receipt != nil || s.ticket != nil && !s.ticket.Completed {
		return port.ErrExecutionMaintenance
	}
	s.active = true
	defer func() { s.active = false }()
	return f(ctx)
}
func (s *maintenanceMock) CompleteExecutionRetention(ctx context.Context, p retention.Permit, r retention.Receipt, fresh func(context.Context) error) error {
	if s.active || s.permit == nil || *s.permit != p || r.Match(p) != nil {
		return port.ErrExecutionConflict
	}
	if err := fresh(ctx); err != nil {
		return err
	}
	s.completeCalls++
	s.receipt = &r
	s.control.Maintenance = false
	return nil
}
func (s *maintenanceMock) WithExecutionRetentionRelease(ctx context.Context, p retention.Permit, r retention.Receipt, f func(context.Context) error) error {
	if s.active || s.permit == nil || *s.permit != p || s.receipt == nil || *s.receipt != r {
		return port.ErrExecutionMaintenance
	}
	s.active = true
	defer func() { s.active = false }()
	return f(ctx)
}
func maintenanceFixture(t *testing.T, zero bool) (*maintenanceCoordinator, ingress.DrainPoint, *maintenanceMock) {
	t.Helper()
	o, old, _ := rawGCFixture(t)
	if zero {
		o.Window.Now = nil
	}
	s := &maintenanceMock{rawGCMock: old}
	s.control.Visible = o.Permit.Visible
	s.control.Root = o.Permit.Root
	owner, err := s.ExecutionIngressOwner(t.Context())
	require.NoError(t, err)
	policy := owner.Policy
	root := filepath.Dir(o.NativeDataDir)
	binary := filepath.Join(root, "maintenance")
	quoted := "'" + strings.ReplaceAll(os.Args[0], "'", "'\"'\"'") + "'"
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\nGORACE=atexit_sleep_ms=0 exec "+quoted+" -test.run=^TestMaintenanceNativeHelper$ -- --maintenance-helper \"$@\"\n"), 0700))
	var identity nativeinput.Identity
	require.NoError(t, nativeinput.Decode(owner.Binding.NativeIdentity, &identity))
	proof, err := nativeinput.ReadHeadProof(o.NativeDataDir, identity)
	require.NoError(t, err)
	c := &maintenanceCoordinator{cfg: ingress.Config{Module: ModuleName, Native: ingress.NativeConfig{DataDir: o.NativeDataDir}, Collector: ingress.CollectorConfig{SpoolDir: o.Source.Path, MaxBytes: o.Source.MaxBytes, MaxRecords: o.Source.MaxRecords}, Timeout: 5 * time.Second}, deps: ingress.Dependencies{Store: s, Control: s.control, Policy: &policy, Canonical: canonicalFunc(func(ctx context.Context, h uint64) (common.Hash, error) {
		if h != o.Permit.Visible.Height {
			return common.Hash{}, fmt.Errorf("unexpected height")
		}
		return common.Hash(o.Permit.Visible.Hash), ctx.Err()
	})}, raw: o.Window, options: ingress.MaintenanceOptions{Executable: binary, KeepBlocks: 129, ReserveBytes: 1, Timeout: 5 * time.Second}, store: s}
	point := ingress.DrainPoint{Chain: o.Source.Chain, Ref: o.Permit.Visible, Root: o.Permit.Root}
	// The source's actual durable digest, not a synthetic Hello.
	require.NoError(t, spool.WithRetentionEvidence(t.Context(), o.Source, o.Permit.Request.Head, o.Permit.Root, 129, func(_ retention.Request, ack execwire.Hash) error { point.Digest = ack; return nil }))
	s.ackDigest = point.Digest
	raw, err := window.OpenOwnedChecked(o.Window, ownerDomain, func(window.DeletionRecord) error { return window.ErrUnsafeGC })
	require.NoError(t, err)
	var batch []chainfeed.Envelope
	id := model.FeedBlock{Chain: o.Window.Chain, Number: 130, Hash: common.Hash(point.Ref.Hash)}
	for _, kind := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		ref, err := raw.Lookup(t.Context(), id, kind)
		require.NoError(t, err)
		lease, err := raw.Acquire(t.Context(), ref)
		require.NoError(t, err)
		entry, err := lease.Envelope()
		require.NoError(t, err)
		entry.Payload = append([]byte(nil), entry.Payload...)
		require.NoError(t, lease.Close())
		batch = append(batch, entry)
	}
	require.NoError(t, raw.Close())
	decoded, err := decodeBatch(id.Chain, batch)
	require.NoError(t, err)
	receipt := *proof.Receipt
	receipt.InputDigest, err = nativeinput.Digest(nativeinput.ReceiptMeta(receipt), decoded.encoded, decoded.receipts)
	require.NoError(t, err)
	writeNativeArtifact(t, completionPath(o.NativeDataDir, receipt.ImportEpoch, receipt.Height), receipt)
	return c, point, s
}
func TestMaintenanceRawZeroIsDurableBeforeNativeCOW(t *testing.T) {
	c, point, s := maintenanceFixture(t, true)
	stop := errors.New("power loss after raw completion")
	c.after = func(phase string) error {
		if phase == "raw_complete" {
			return stop
		}
		return nil
	}
	_, err := c.run(t.Context(), &point)
	require.ErrorIs(t, err, stop, "zero raw candidates still need a durable completed phase")
	require.Nil(t, s.ticket)
	c.after = nil
	result, err := c.run(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, "complete", result.Phase)
	require.NotZero(t, result.Release)
	require.Equal(t, 1, s.beginCalls)
}

func TestMaintenanceUnfinishedReleaseBlocksStartupAndResume(t *testing.T) {
	c, point, _ := maintenanceFixture(t, true)
	stop := errors.New("power loss after exact file release")
	c.after = func(phase string) error {
		if phase == "files_effects" {
			return stop
		}
		return nil
	}
	_, err := c.run(t.Context(), &point)
	require.ErrorIs(t, err, stop)
	p := &program{cfg: c.cfg, deps: c.deps, opts: c.raw, started: true, draining: &drainRequest{parked: true, point: point}}
	require.ErrorIs(t, p.Resume(t.Context()), port.ErrExecutionMaintenance, "no remaining file fence is not journal completion")
	p.started = false
	require.ErrorIs(t, p.Check(t.Context()), port.ErrExecutionMaintenance, "startup must refuse an unfinished phase before inspecting children")
}
func TestMaintenanceRecoversEachDurableBoundaryWithoutRestartingRaw(t *testing.T) {
	for _, phase := range []string{"prepared", "authorization_effects", "authorized", "raw_effects", "raw_complete", "cow_effects", "cow_complete", "db_effects", "db_complete", "files_effects", "complete"} {
		t.Run(phase, func(t *testing.T) {
			c, point, s := maintenanceFixture(t, false)
			crash := errors.New("owned interruption at " + phase)
			c.after = func(at string) error {
				if at == phase {
					return crash
				}
				return nil
			}
			_, err := c.run(t.Context(), &point)
			require.ErrorIs(t, err, crash)
			c.after = nil
			cold := &maintenanceCoordinator{cfg: c.cfg, deps: c.deps, raw: c.raw, options: c.options}
			result, err := cold.run(t.Context(), nil)
			require.NoError(t, err)
			require.Equal(t, "complete", result.Phase)
			require.False(t, result.Resumed)
			require.NoError(t, result.Release.Match(result.Permit, result.Receipt))
			require.Equal(t, 1, s.beginCalls, "uncertain authorization must adopt the actual pending token")
			require.NotNil(t, s.ticket)
			require.True(t, s.ticket.Completed)
			before := s.completeCalls
			s.control.Visible.Height++
			_, err = cold.run(t.Context(), nil)
			require.NoError(t, err, "completed recovery is read-only after public head advances")
			require.Equal(t, before, s.completeCalls)
		})
	}
}

func TestMaintenanceChangedBinaryAndCorruptJournalStayHold(t *testing.T) {
	c, point, s := maintenanceFixture(t, true)
	stop := errors.New("authorized")
	c.after = func(phase string) error {
		if phase == "authorized" {
			return stop
		}
		return nil
	}
	_, err := c.run(t.Context(), &point)
	require.ErrorIs(t, err, stop)
	c.after = nil
	before, err := os.ReadFile(maintenancePath(c.cfg.Native.DataDir))
	require.NoError(t, err)
	f, err := os.OpenFile(c.options.Executable, os.O_APPEND|os.O_WRONLY, 0700)
	require.NoError(t, err)
	_, err = f.WriteString("\n#changed\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	_, err = c.run(t.Context(), nil)
	require.ErrorIs(t, err, port.ErrExecutionConflict)
	after, err := os.ReadFile(maintenancePath(c.cfg.Native.DataDir))
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Nil(t, s.ticket)
	require.NoError(t, os.WriteFile(maintenancePath(c.cfg.Native.DataDir), []byte("corrupt"), 0600))
	_, err = c.run(t.Context(), nil)
	require.Error(t, err)
	require.Nil(t, s.ticket)
}

func TestMaintenanceNativeHelper(t *testing.T) {
	if !strings.Contains(strings.Join(os.Args, " "), "--maintenance-helper") {
		return
	}
	value := func(key string) string {
		for i, a := range os.Args {
			if a == key && i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
		return ""
	}
	fail := func(err error) {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	data, err := os.ReadFile(value("--managed-context"))
	fail(err)
	var ctx nativeops.ManagedContext
	fail(nativeinput.Decode(data, &ctx))
	fail(nativeinput.VerifyCurrent(value("--datadir"), ctx.Proof, ctx.Proof.Identity))
	var input json.RawMessage
	fail(json.NewDecoder(os.Stdin).Decode(&input))
	if value("--operation") == "prune" {
		parent := filepath.Dir(ctx.OriginalDataDir)
		if _, e := os.Stat(filepath.Join(parent, "BLOCK")); e == nil {
			fail(os.WriteFile(filepath.Join(parent, "ENTERED"), []byte("ready"), 0600))
			<-time.NewTimer(time.Hour).C
		}
	}

	switch value("--operation") {
	case "inspect", "hold":
		h := ctx.Proof.Head.Tip
		fail(json.NewEncoder(os.Stdout).Encode(nativeops.Observation{Head: execwire.Ref{Height: h.Height, Hash: h.Hash}, Root: h.Root, AnchorRoot: ctx.Proof.Identity.Anchor.Root}))
		if value("--operation") == "hold" {
			_, err = io.Copy(io.Discard, os.Stdin)
			fail(err)
		}
	case "prune", "verify":
		fail(json.NewEncoder(os.Stdout).Encode(struct{ Status string }{"native_verified"}))
	default:
		fail(errors.New("unknown operation"))
	}
	os.Exit(0)
}

func (s *maintenanceMock) ExecutionParts(_ context.Context, _ uint64) ([]port.ExecutionPart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []port.ExecutionPart
	for _, w := range port.ExecutionWriters() {
		out = append(out, port.ExecutionPart{Writer: w, Ref: s.control.Visible, Parent: execwire.Hash{129}, Digest: s.ackDigest})
	}
	return out, nil
}
func (s *maintenanceMock) ExecutionCursor(ctx context.Context, w port.ExecutionWriter) (port.ExecutionPart, error) {
	parts, _ := s.ExecutionParts(ctx, 130)
	for _, p := range parts {
		if p.Writer == w {
			return p, nil
		}
	}
	return port.ExecutionPart{}, port.ErrNotFound
}

type maintenanceRunWriter struct {
	*roundWriterFixture
	lease *os.File
}

func (w *maintenanceRunWriter) Close() { w.roundWriterFixture.Close(); _ = w.lease.Close() }
func liveMaintenanceFixture(t *testing.T) (*program, *maintenanceCoordinator, <-chan struct{}, <-chan error) {
	t.Helper()
	c, point, _ := maintenanceFixture(t, true)
	c.cfg = c.cfg.Normalize()
	c.cfg.Native.Executable = filepath.Join(filepath.Dir(c.cfg.Native.DataDir), "importer")
	c.cfg.Collector.Executable = filepath.Join(filepath.Dir(c.cfg.Native.DataDir), "collector")
	require.NoError(t, os.WriteFile(c.cfg.Native.Executable, []byte("owned protocol fixture"), 0700))
	require.NoError(t, os.WriteFile(c.cfg.Collector.Executable, []byte("owned protocol fixture"), 0700))
	socket := filepath.Join(filepath.Dir(c.cfg.Native.DataDir), "socket")
	require.NoError(t, os.Mkdir(socket, 0700))
	c.deps.Socket = filepath.Join(socket, "execution.sock")
	c.deps.Canonical = roundOracle{head: 130, hashes: map[uint64]common.Hash{130: common.Hash(point.Ref.Hash)}}
	p := &program{cfg: c.cfg, deps: c.deps, opts: c.raw, slots: make(chan struct{}, 8), bytes: semaphore.NewWeighted(256 << 20)}
	p.nativeStart = func(context.Context) (nativeProcess, error) {
		lease, err := nativestore.AcquireRun(c.cfg.Native.DataDir)
		if err != nil {
			return nil, err
		}
		var i nativeIdentity
		err = readArtifact(filepath.Join(c.cfg.Native.DataDir, identityName), &i)
		if err != nil {
			_ = lease.Close()
			return nil, err
		}
		proof, err := nativeinput.ReadHeadProof(c.cfg.Native.DataDir, i)
		if err != nil {
			_ = lease.Close()
			return nil, err
		}
		return &maintenanceRunWriter{roundWriterFixture: &roundWriterFixture{ready: nativeReady{Version: 1, Identity: i, Head: proof.Head}}, lease: lease}, nil
	}
	p.peerStart = func(context.Context, model.FeedBlock) (peerProcess, error) {
		return &roundPeerFixture{input: make(chan []chainfeed.Envelope), streamClosed: make(chan struct{})}, nil
	}
	ready := make(chan struct{}, 4)
	p.collectorRun = func(ctx context.Context, cfg collector.Config) error {
		lease, err := nativestore.AcquireRun(c.cfg.Native.DataDir)
		if err != nil {
			return err
		}
		defer func() { _ = lease.Close() }()
		cfg.OnCommitted(execwire.Header{Version: 1, Chain: point.Chain, Ref: point.Ref, Digest: point.Digest})
		ready <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	t.Cleanup(func() { cancel(); p.Stop() })
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("owned run did not start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("owned run not ready")
	}
	return p, c, ready, done
}
func TestMaintenanceLiveRunDrainsAndResumesBothOwnedWorkers(t *testing.T) {
	p, c, ready, done := liveMaintenanceFixture(t)
	result, err := p.Maintain(t.Context(), c.options)
	require.NoError(t, err)
	require.True(t, result.Resumed)
	require.Equal(t, "complete", result.Phase)
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("resume terminated owned run: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("workers not resumed")
	}
	p.Stop()
	require.Error(t, <-done)
}
func TestMaintenanceStopCancelsAndJoinsBeforeReturningOwnership(t *testing.T) {
	p, c, _, run := liveMaintenanceFixture(t)
	marker := filepath.Join(filepath.Dir(c.cfg.Native.DataDir), "BLOCK")
	require.NoError(t, os.WriteFile(marker, []byte("wait"), 0600))
	done := make(chan error, 1)
	go func() { _, err := p.Maintain(t.Context(), c.options); done <- err }()
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(filepath.Dir(marker), "ENTERED")); return err == nil }, 5*time.Second, time.Millisecond)
	stopped := make(chan struct{})
	go func() { p.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop deadlocked with coordinator recovery mutex")
	}
	require.Error(t, <-done)
	require.Error(t, <-run)
	require.NoError(t, os.Remove(marker))
	result, err := RecoverMaintenance(t.Context(), c.cfg, c.deps, c.raw, c.options)
	require.NoError(t, err)
	require.False(t, result.Resumed)
}
