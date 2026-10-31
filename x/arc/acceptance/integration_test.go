//go:build arc_native

package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	checkpointformat "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/nativediagnostics"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/app"
	"github.com/0xmhha/indexer-go/pkg/app/apptest"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/ethpeer"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc"
	_ "github.com/0xmhha/indexer-go/x/arc/bundle"
	arcdiagnostics "github.com/0xmhha/indexer-go/x/arc/diagnostics"
	arcingress "github.com/0xmhha/indexer-go/x/arc/ingress"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type blockInput struct {
	block                   *types.Block
	encoded, body, receipts []byte
}
type rpcCall struct {
	Method string
	Height uint64
	At     time.Time
}
type network struct {
	mu         sync.Mutex
	blocks     map[uint64]blockInput
	alternate  blockInput
	deepBlocks map[uint64]blockInput
	deep       bool
	height     uint64
	alt        bool
	calls      []rpcCall
	physical   []time.Time
	pause      uint64
	reached    chan struct{}
	release    chan struct{}
	once       sync.Once
}

func (n *network) at(height uint64) (blockInput, bool) {
	if height > n.height {
		return blockInput{}, false
	}
	if n.deep && height > 0 {
		b, ok := n.deepBlocks[height]
		return b, ok
	}
	if n.alt && height == 3 {
		return n.alternate, true
	}
	b, ok := n.blocks[height]
	return b, ok
}
func (n *network) change(height uint64, alternate bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.height = height
	n.alt = alternate
}
func (n *network) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     json.RawMessage
		Method string
		Params []json.RawMessage
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "invalid owned request", 400)
		return
	}
	n.mu.Lock()
	n.physical = append(n.physical, time.Now())
	var value any
	var failure any
	var height uint64
	var wait <-chan struct{}
	switch req.Method {
	case "web3_clientVersion":
		value = "Arc/N6-owned-loopback"
	case "eth_chainId":
		value = hexutil.EncodeUint64(5042002)
	case "eth_blockNumber":
		value = hexutil.EncodeUint64(n.height)
	case "eth_getBlockByNumber":
		var number string
		_ = json.Unmarshal(req.Params[0], &number)
		if number == "latest" {
			height = n.height
		} else {
			height, _ = hexutil.DecodeUint64(number)
		}
		if b, ok := n.at(height); ok {
			value = b.block.Header()
		}
		if n.pause != 0 && height == n.pause {
			n.once.Do(func() { close(n.reached) })
			wait = n.release
		}
	default:
		failure = map[string]any{"code": -32601, "message": "owned acceptance forbids this method"}
	}
	n.calls = append(n.calls, rpcCall{req.Method, height, time.Now()})
	n.mu.Unlock()
	if wait != nil {
		select {
		case <-wait:
		case <-r.Context().Done():
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": value, "error": failure})
}

type statusPacket struct {
	Version uint32
	Network uint64
	TD      *big.Int
	Head    common.Hash
	Genesis common.Hash
	Fork    ethpeer.ForkID
}
type headerQuery struct {
	Origin       rlp.RawValue
	Amount, Skip uint64
	Reverse      bool
}
type headerRequest struct {
	ID    uint64
	Query headerQuery
}
type hashRequest struct {
	ID     uint64
	Hashes []common.Hash
}
type packet struct {
	ID    uint64
	Items []rlp.RawValue
}

func (n *network) peer(t *testing.T) *p2p.Server {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	srv := &p2p.Server{Config: p2p.Config{PrivateKey: key, MaxPeers: 4, NoDiscovery: true, ListenAddr: "127.0.0.1:0", Name: "owned-N6-ETH68", Protocols: []p2p.Protocol{{Name: "eth", Version: 68, Length: 17, Run: func(_ *p2p.Peer, rw p2p.MsgReadWriter) error {
		msg, err := rw.ReadMsg()
		if err != nil {
			return err
		}
		if msg.Code != 0 {
			return errors.New("expected ETH status")
		}
		var remote statusPacket
		if err = msg.Decode(&remote); err != nil {
			return err
		}
		n.mu.Lock()
		head, _ := n.at(n.height)
		zero, _ := n.at(0)
		n.mu.Unlock()
		if remote.Version != 68 || remote.Network != 5042002 || remote.Genesis != zero.block.Hash() {
			return errors.New("foreign ETH status")
		}
		if err = p2p.Send(rw, 0, statusPacket{68, 5042002, new(big.Int), head.block.Hash(), zero.block.Hash(), ethpeer.ForkID{Hash: [4]byte{1, 2, 3, 4}}}); err != nil {
			return err
		}
		for {
			msg, err = rw.ReadMsg()
			if err != nil {
				return err
			}
			if msg.Size > ethpeer.MaxMessage {
				return errors.New("oversize owned ETH request")
			}
			response := packet{}
			var code uint64
			switch msg.Code {
			case 3:
				var q headerRequest
				if err = msg.Decode(&q); err != nil {
					return err
				}
				response.ID = q.ID
				code = 4
				if q.Query.Amount != 1 || q.Query.Skip != 0 || q.Query.Reverse {
					return errors.New("unexpected ETH range")
				}
				n.mu.Lock()
				var b blockInput
				var ok bool
				if len(q.Query.Origin) == 33 {
					var hash common.Hash
					_ = rlp.DecodeBytes(q.Query.Origin, &hash)
					for h := uint64(0); h <= n.height; h++ {
						v, exists := n.at(h)
						if exists && v.block.Hash() == hash {
							b = v
							ok = true
							break
						}
					}
				} else {
					var h uint64
					if e := rlp.DecodeBytes(q.Query.Origin, &h); e == nil {
						b, ok = n.at(h)
					}
				}
				if ok {
					encoded, e := rlp.EncodeToBytes(b.block.Header())
					if e != nil {
						n.mu.Unlock()
						return e
					}
					response.Items = []rlp.RawValue{encoded}
				}
				n.mu.Unlock()
			case 5, 15:
				var q hashRequest
				if err = msg.Decode(&q); err != nil {
					return err
				}
				response.ID = q.ID
				code = msg.Code + 1
				if len(q.Hashes) != 1 {
					return errors.New("unexpected ETH hash list")
				}
				n.mu.Lock()
				for h := uint64(0); h <= n.height; h++ {
					b, ok := n.at(h)
					if ok && b.block.Hash() == q.Hashes[0] {
						if msg.Code == 5 {
							response.Items = []rlp.RawValue{b.body}
						} else {
							response.Items = []rlp.RawValue{b.receipts}
						}
						break
					}
				}
				n.mu.Unlock()
			default:
				if err = msg.Discard(); err != nil {
					return err
				}
				continue
			}
			if err = p2p.Send(rw, code, response); err != nil {
				return err
			}
		}
	}}}}}
	require.NoError(t, srv.Start())
	t.Cleanup(srv.Stop)
	return srv
}
func fixture(t *testing.T) (string, checkpointformat.Manifest, *network) {
	t.Helper()
	root := os.Getenv("INDEXER_ARC_INGRESS_FIXTURE_DIR")
	require.True(t, filepath.IsAbs(root), "explicit owned SDK fixture required; no skip or external fallback")
	var m checkpointformat.Manifest
	b, err := os.ReadFile(filepath.Join(root, "expected.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &m))
	require.Equal(t, "5042002", m.Chain.ID)
	n := &network{blocks: map[uint64]blockInput{}, height: 3, reached: make(chan struct{}), release: make(chan struct{})}
	for h := uint64(0); h <= 131; h++ {
		if h == 0 {
			header, err := os.ReadFile(filepath.Join(root, "header-0.rlp"))
			require.NoError(t, err)
			var v types.Header
			require.NoError(t, rlp.DecodeBytes(header, &v))
			n.blocks[0] = blockInput{block: types.NewBlockWithHeader(&v)}
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("block-%d.rlp", h)))
		require.NoError(t, err)
		var block types.Block
		require.NoError(t, rlp.DecodeBytes(data, &block))
		body, err := rlp.EncodeToBytes(block.Body())
		require.NoError(t, err)
		receipts, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("receipts-%d.rlp", h)))
		require.NoError(t, err)
		n.blocks[h] = blockInput{&block, data, body, receipts}
		if n.deepBlocks == nil {
			n.deepBlocks = map[uint64]blockInput{}
		}
		deepData, e := os.ReadFile(filepath.Join(root, fmt.Sprintf("deep-%d.rlp", h)))
		require.NoError(t, e)
		var deep types.Block
		require.NoError(t, rlp.DecodeBytes(deepData, &deep))
		deepBody, e := rlp.EncodeToBytes(deep.Body())
		require.NoError(t, e)
		n.deepBlocks[h] = blockInput{&deep, deepData, deepBody, receipts}
	}
	data, err := os.ReadFile(filepath.Join(root, "alternate-3.rlp"))
	require.NoError(t, err)
	var alt types.Block
	require.NoError(t, rlp.DecodeBytes(data, &alt))
	body, err := rlp.EncodeToBytes(alt.Body())
	require.NoError(t, err)
	n.alternate = blockInput{&alt, data, body, n.blocks[3].receipts}
	require.Equal(t, common.Hash(m.Chain.Genesis), n.blocks[0].block.Hash())
	require.NotEqual(t, n.blocks[3].block.Hash(), alt.Hash())
	return root, m, n
}
func binary(t *testing.T, name string) string {
	t.Helper()
	root := os.Getenv("INDEXER_ARC_ACCEPTANCE_BIN_DIR")
	require.True(t, filepath.IsAbs(root), "explicit owned native binaries required")
	p := filepath.Join(root, name)
	s, e := os.Lstat(p)
	require.NoError(t, e)
	require.True(t, s.Mode().IsRegular() && s.Mode()&0111 != 0)
	return p
}
func privateDir(t *testing.T, parent, name string) string {
	t.Helper()
	p := filepath.Join(parent, name)
	require.NoError(t, os.Mkdir(p, 0700))
	return p
}
func wrapper(t *testing.T, root, name, target string) (string, string) {
	t.Helper()
	path := filepath.Join(root, name)
	log := filepath.Join(root, name+".log")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexec "+quote(target)+" \"$@\" 2>>"+quote(log)+"\n"), 0700))
	return path, log
}
func configFor(t *testing.T, root, input string, m checkpointformat.Manifest, n *network, srv *p2p.Server) (*config.Config, string, string) {
	t.Helper()
	httpServer := httptest.NewServer(n)
	t.Cleanup(httpServer.Close)
	cfg := apptest.Config(t, httpServer.URL)
	require.Equal(t, "postgres", cfg.Database.Driver, "owned PostgreSQL fixture required")
	cfg.RPC.RateLimit = 5
	cfg.RPC.Timeout = 15 * time.Second
	cfg.Indexer.StatePrefetch = true
	cfg.Indexer.AcquisitionQueue = true
	cfg.Indexer.Finality = "head"
	cfg.Indexer.Confirmations = 0
	cfg.Indexer.StartHeight = 1
	cfg.Notifications.Enabled = false
	cfg.Verifier.Enabled = false
	if cfg.Features == nil {
		cfg.Features = map[string]config.FeatureConfig{}
	}
	for _, name := range append(feature.Defaults(), arc.New().Features()...) {
		off := false
		cfg.Features[name] = config.FeatureConfig{Enabled: &off}
	}
	apptest.Enable(cfg, "address.index", "balance.native", "token.transfers")
	native := filepath.Join(root, "native")
	cmd := exec.Command(binary(t, "arc-native-bootstrap"), "--action", "build", "--target", native, "--state", filepath.Join(input, "genesis.ndjson"), "--expected", filepath.Join(input, "expected.json"))
	cmd.Env = nativeinput.ChildEnvironment()
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "bootstrap: %s", output)
	sourceParent := privateDir(t, root, "source")
	rawParent := privateDir(t, root, "raw-volume")
	privateDir(t, root, "ipc")
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	keyFile := filepath.Join(root, "peer.key")
	require.NoError(t, crypto.SaveECDSA(keyFile, key))
	importer, importLog := wrapper(t, root, "importer", binary(t, "arc-native-import"))
	replayer, replayLog := wrapper(t, root, "replayer", binary(t, "indexer-arc-executor"))
	cfg.Source.Execution = &config.ExecutionSourceConfig{Socket: filepath.Join(root, "ipc", "s"), AnchorHeight: m.Ref.Height, AnchorHash: common.Hash(m.Ref.Hash).Hex(), AnchorRoot: common.Hash(m.Root).Hex(), Timeout: 15 * time.Second, Ingress: &ingress.Config{Module: "arc.native", Window: "raw", InitializeVolume: true, CreateWindow: true, Native: ingress.NativeConfig{Executable: importer, DataDir: native}, Collector: ingress.CollectorConfig{Executable: replayer, SpoolDir: filepath.Join(sourceParent, "spool"), MaxBytes: 64 << 20, MaxRecords: 1024}, Peer: chainfeed.PeerSettings{NetworkID: 5042002, ForkHash: "01020304", KeyFile: keyFile, Peers: []string{srv.Self().URLv4()}, ListenAddr: "127.0.0.1:0"}, MaxRawBlocks: 256, MaxActiveBlocks: 4, MaxActiveBytes: 64 << 20, Timeout: 30 * time.Second}}
	cfg.WindowStorage = window.Layout{Volumes: map[string]window.VolumeSpec{"raw": {Root: rawParent, ID: "owned-N6-raw", AllowDirectory: true, MaxBytes: 64 << 20}}, Windows: map[string]window.WindowConfig{"raw": {Volume: "raw", Path: filepath.Join(rawParent, "blocks"), MaxBytes: 32 << 20, SegmentBytes: 128 << 10, MaxSegments: 256, MaxPayloadBytes: 64 << 10, MaxSegmentRecords: 3, ReadBudgetBytes: 2 << 20, MaxReaders: 2, Retention: 24 * time.Hour}}}
	require.EqualValues(t, 4, cfg.Database.Postgres.MaxConns)
	db, err := postgres.Open(t.Context(), postgres.Options{DSN: cfg.Database.Postgres.DSN, Schema: cfg.Database.Postgres.Schema, MaxConns: 4})
	require.NoError(t, err)
	state, err := os.Open(filepath.Join(input, "genesis.ndjson"))
	require.NoError(t, err)
	_, err = db.BootstrapExecution(t.Context(), state, m, arc.New(), func(ctx context.Context, r execwire.Ref) error {
		if r != m.Ref {
			return port.ErrExecutionConflict
		}
		return ctx.Err()
	})
	require.NoError(t, err)
	require.NoError(t, state.Close())
	require.NoError(t, db.Close())
	return cfg, importLog, replayLog
}

type running struct {
	a      *app.App
	store  *postgres.Store
	cancel context.CancelFunc
	done   chan struct{}
	runErr error
	once   sync.Once
}

func start(t *testing.T, cfg *config.Config) *running {
	t.Helper()
	a, err := app.NewApp(cfg, zap.NewNop(), false, "arc")
	require.NoError(t, err)
	s, ok := a.Storage().(*postgres.Store)
	require.True(t, ok)
	ctx, cancel := context.WithCancel(t.Context())
	r := &running{a: a, store: s, cancel: cancel, done: make(chan struct{})}
	go func() { r.runErr = a.Run(ctx); close(r.done) }()
	t.Cleanup(func() { r.stop(t) })
	return r
}
func (r *running) stop(t *testing.T) {
	t.Helper()
	r.once.Do(func() {
		r.cancel()
		select {
		case <-r.done:
			require.ErrorIs(t, r.runErr, context.Canceled)
		case <-time.After(30 * time.Second):
			t.Fatal("owned App did not join")
		}
		r.a.Shutdown()
	})
}
func (r *running) wait(t *testing.T, height, generation uint64, hash common.Hash) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.done:
			t.Fatalf("owned App exited before public boundary: %v", r.runErr)
		case <-ctx.Done():
			t.Fatalf("public boundary not reached: height=%d generation=%d: %v", height, generation, ctx.Err())
		case <-ticker.C:
			c, e := r.store.ExecutionStatus(ctx)
			if e == nil && c.Visible.Height == height && c.Generation == generation && c.Visible.Hash == execwire.Hash(hash) {
				return
			}
		}
	}
}

func ack(t *testing.T, path string, ref execwire.Ref) {
	t.Helper()
	require.Eventually(t, func() bool { s, e := spool.InspectMetadata(t.Context(), path); return e == nil && s.Committed == ref }, 30*time.Second, 10*time.Millisecond, "source needs exact committed ACK")
}
func executionCounts(t *testing.T, path string) map[string]int {
	t.Helper()
	b, e := os.ReadFile(path)
	require.NoError(t, e)
	out := map[string]int{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "ARC_ACCEPTANCE_EVM ") {
			out[line]++
		}
	}
	return out
}
func ownedRoot(t *testing.T) string {
	t.Helper()
	p, e := os.MkdirTemp("/tmp", "arc-n6-")
	require.NoError(t, e)
	p, e = filepath.EvalSymlinks(p)
	require.NoError(t, e)
	require.NoError(t, os.Chmod(p, 0700))
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(p)) })
	return p
}
func TestArcNativeIntegratedAcceptance(t *testing.T) {
	input, m, n := fixture(t)
	peer := n.peer(t)
	root := ownedRoot(t)
	var err error
	cfg, importLog, replayLog := configFor(t, root, input, m, n, peer)
	var rawClockOffset atomic.Int64
	cfg.WindowStorage.Now = func() time.Time { return time.Now().Add(time.Duration(rawClockOffset.Load())) }
	n.mu.Lock()
	n.pause = 3
	n.mu.Unlock()
	r := start(t, cfg)
	select {
	case <-n.reached:
	case <-r.done:
		t.Fatalf("App stopped before final fresh: %v", r.runErr)
	case <-time.After(time.Minute):
		t.Fatal("final fresh request missing")
	}
	c, err := r.store.ExecutionStatus(t.Context())
	require.NoError(t, err)
	require.Less(t, c.Visible.Height, uint64(3), "no public block while fresh response withheld")
	close(n.release)
	r.wait(t, 3, 1, n.blocks[3].block.Hash())
	ref := execwire.Ref{Generation: 1, Height: 3, Hash: execwire.Hash(n.blocks[3].block.Hash())}
	ack(t, cfg.Source.Execution.Ingress.Collector.SpoolDir, ref)
	t.Log("phase: fresh-gated publication and exact source ACK reached3")
	var originalPart port.ExecutionPart
	for height := uint64(1); height <= 3; height++ {
		parts, err := r.store.ExecutionParts(t.Context(), height)
		require.NoError(t, err)
		require.Len(t, parts, 7)
		for _, part := range parts {
			require.NotEqual(t, execwire.Hash{}, part.Digest)
			if part.Writer == port.ExecutionBlocks {
				if height == 1 {
					originalPart = part
				}
				a, e := r.store.ExecutionIngressArchive(t.Context(), part)
				require.NoError(t, e)
				require.Len(t, a.Datasets, 3)
				encoded, e := rlp.EncodeToBytes(n.blocks[height].block.Header())
				require.NoError(t, e)
				require.Equal(t, encoded, a.Datasets[0].Payload)
				require.Equal(t, n.blocks[height].body, a.Datasets[1].Payload)
				require.Equal(t, n.blocks[height].receipts, a.Datasets[2].Payload)
			}
		}
	}
	original, err := arcingress.InspectNativeControl(t.Context(), cfg.Source.Execution.Ingress.Native.DataDir)
	require.NoError(t, err)
	require.EqualValues(t, 3, original.Head.RequestID)
	require.Len(t, executionCounts(t, importLog), 3, "one actual first EVM per normal block")
	require.Len(t, executionCounts(t, replayLog), 3, "one actual second EVM per normal block")
	for _, log := range []string{importLog, replayLog} {
		for _, count := range executionCounts(t, log) {
			require.Equal(t, 1, count, "normal path must enter each block's EVM exactly once per stage")
		}
	}
	t.Log("phase: actual import/replay counts are exactly one each for normal blocks1..3")
	lease, err := r.store.ReserveQueryReader(t.Context())
	require.NoError(t, err)
	handler, err := graphql.NewHandler(r.store, zap.NewNop())
	require.NoError(t, err)
	response := handler.Execute(lease.Context(t.Context()), "{ latestHeight }", nil)
	require.Empty(t, response.Errors)
	require.NotNil(t, response.Data)
	lease.Close()
	seq, err := r.store.LastOutboxSeq(t.Context())
	require.NoError(t, err)
	require.NotZero(t, seq)
	r.stop(t)
	retransmit, e := os.ReadFile(filepath.Join(input, "result-3.exr"))
	require.NoError(t, e)
	cfg.Source.Execution.Ingress.InitializeVolume = false
	cfg.Source.Execution.Ingress.CreateWindow = false
	leaseFile, err := nativestore.AcquireExclusiveRun(cfg.Source.Execution.Ingress.Native.DataDir)
	require.NoError(t, err)
	require.NoError(t, leaseFile.Close())
	r = start(t, cfg)
	r.wait(t, 3, 1, n.blocks[3].block.Hash())
	ack(t, cfg.Source.Execution.Ingress.Collector.SpoolDir, ref)
	after, err := r.store.LastOutboxSeq(t.Context())
	require.NoError(t, err)
	require.Equal(t, seq, after, "restart must not republish ACKed block")
	conn, e := net.DialTimeout("unix", cfg.Source.Execution.Socket, 5*time.Second)
	require.NoError(t, e)
	reply, e := execution.SubmitResult(t.Context(), conn, arc.New(), m.Chain, retransmit)
	require.NoError(t, e)
	require.Equal(t, ref, reply.Header.Ref)
	confirmed, e := r.store.LastOutboxSeq(t.Context())
	require.NoError(t, e)
	require.Equal(t, seq, confirmed, "lost-ACK retransmission cannot publish twice")
	require.Len(t, executionCounts(t, importLog), 3)
	require.Len(t, executionCounts(t, replayLog), 3)
	for _, log := range []string{importLog, replayLog} {
		for _, count := range executionCounts(t, log) {
			require.Equal(t, 1, count, "restart/exact duplicate must not execute again")
		}
	}
	t.Log("phase: restart and exact lost-ACK retransmission preserve publication/outbox and execution counts")
	n.change(3, true)
	r.wait(t, 3, 2, n.alternate.block.Hash())
	ack(t, cfg.Source.Execution.Ingress.Collector.SpoolDir, execwire.Ref{Generation: 2, Height: 3, Hash: execwire.Hash(n.alternate.block.Hash())})
	n.change(3, false)
	r.wait(t, 3, 3, n.blocks[3].block.Hash())
	ack(t, cfg.Source.Execution.Ingress.Collector.SpoolDir, execwire.Ref{Generation: 3, Height: 3, Hash: execwire.Hash(n.blocks[3].block.Hash())})
	t.Log("phase: short reorg and return preserve separate receiver generations2/3")
	n.change(131, false)
	r.wait(t, 131, 3, n.blocks[131].block.Hash())
	ack(t, cfg.Source.Execution.Ingress.Collector.SpoolDir, execwire.Ref{Generation: 3, Height: 131, Hash: execwire.Hash(n.blocks[131].block.Hash())})
	// Advance only this owned raw window's clock. Its24h minimum, 129-block
	// floor, exact ACK/SQL/epoch guards and real canonical clock remain intact.
	rawClockOffset.Store(int64(25 * time.Hour))
	for stage, path := range map[string]string{"import": importLog, "replay": replayLog} {
		counts := executionCounts(t, path)
		for h := uint64(4); h <= 131; h++ {
			key := fmt.Sprintf("ARC_ACCEPTANCE_EVM %s %d %s 3", stage, h, n.blocks[h].block.Hash().Hex())
			require.Equal(t, 1, counts[key], "one actual EVM in each stable-generation stage")
		}
	}
	t.Log("phase: full owned chain131 durably published and ACKed; starting explicit E3 after controlled raw age25h")
	// A fixed owned wrapper fails exactly one prune child after raw completion.
	// Recovery uses the same executable bytes/options and the exact permit.
	failMarker := filepath.Join(root, "prune-fail-once")
	require.NoError(t, os.WriteFile(failMarker, []byte("owned failure injection"), 0600))
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	maintenanceWrapper := filepath.Join(root, "maintenance-wrapper")
	script := "#!/bin/sh\nprevious=''\nfor arg in \"$@\"; do\n  if [ \"$previous\" = '--operation' ] && [ \"$arg\" = 'prune' ] && [ -f " + quote(failMarker) + " ]; then\n    rm -- " + quote(failMarker) + "\n    exit 42\n  fi\n  previous=\"$arg\"\ndone\nexec " + quote(binary(t, "arc-native-maintenance")) + " \"$@\"\n"
	require.NoError(t, os.WriteFile(maintenanceWrapper, []byte(script), 0700))
	optionsMaintenance := ingress.MaintenanceOptions{Executable: maintenanceWrapper, KeepBlocks: 129, ReserveBytes: 1 << 20, Timeout: 3 * time.Minute}
	interrupted, err := r.a.MaintainExecution(t.Context(), optionsMaintenance)
	require.Error(t, err, "injected real prune child must fail")
	require.False(t, interrupted.Resumed)
	_, e = os.Stat(failMarker)
	require.ErrorIs(t, e, os.ErrNotExist, "controlled failure must have been reached")
	pending, e := r.store.InspectExecutionIngress(t.Context())
	require.NoError(t, e)
	require.True(t, pending.Control.Maintenance)
	require.True(t, pending.Retention.Pending)
	require.NotNil(t, pending.Raw)
	require.True(t, pending.Raw.Completed)
	journal, e := arcingress.InspectMaintenance(t.Context(), cfg.Source.Execution.Ingress.Native.DataDir)
	require.NoError(t, e)
	require.Equal(t, "raw_complete", journal.Phase)
	require.Error(t, r.a.ResumeExecution(t.Context()), "incomplete phase cannot resume workers")
	artifact := filepath.Join(root, "pending-sql.json")
	encoded, e := json.Marshal(pending)
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(artifact, encoded, 0600))
	diagnostic := nativediagnostics.Check(t.Context(), []string{"--native", cfg.Source.Execution.Ingress.Native.DataDir, "--raw", cfg.WindowStorage.Windows["raw"].Path, "--spool", cfg.Source.Execution.Ingress.Collector.SpoolDir, "--ingress-artifact", artifact}, arcdiagnostics.Product())
	require.Equal(t, nativediagnostics.Hold, diagnostic.Status, "%+v", diagnostic.Findings)
	wrong := optionsMaintenance
	wrong.Executable = binary(t, "arc-native-maintenance")
	_, e = r.a.RecoverExecutionMaintenance(t.Context(), wrong)
	require.ErrorIs(t, e, port.ErrExecutionConflict, "different binary cannot recover a pending journal")
	result, err := r.a.RecoverExecutionMaintenance(t.Context(), optionsMaintenance)
	require.NoError(t, err)
	t.Log("phase: real prune-child failure preserved SQL permit/raw completion/journal; mismatched recovery refused; exact recovery resumed")
	require.True(t, result.Resumed)
	require.Equal(t, "complete", result.Phase)
	current, err := arcingress.InspectNativeControl(t.Context(), cfg.Source.Execution.Ingress.Native.DataDir)
	require.NoError(t, err)
	require.Equal(t, original.Identity, current.Identity, "maintenance/reorg cannot move original anchor or instance")
	view, err := r.store.InspectExecutionIngress(t.Context())
	require.NoError(t, err)
	require.False(t, view.Control.Maintenance)
	require.False(t, view.RawPending)
	require.NotNil(t, view.Retention)
	require.False(t, view.Retention.Pending)
	require.NotNil(t, view.Retention.Receipt)
	require.NotNil(t, view.Raw, "positive raw GC must produce a durable ticket")
	require.True(t, view.Raw.Completed)
	require.Greater(t, result.Permit.Request.Through, uint64(0))
	archived, e := r.store.ExecutionIngressArchive(t.Context(), originalPart)
	require.NoError(t, e, "original-generation prefix archive must remain queryable after reorg/GC")
	require.Len(t, archived.Datasets, 3)
	source, e := spool.InspectMetadata(t.Context(), cfg.Source.Execution.Ingress.Collector.SpoolDir)
	require.NoError(t, e)
	require.Greater(t, source.Base.Height, uint64(0))
	require.Equal(t, m.Ref, source.Anchor)
	r.stop(t)
	options, e := cfg.WindowStorage.OptionsFor("raw", model.FeedChain{ID: m.Chain.ID, GenesisHash: common.Hash(m.Chain.Genesis)})
	require.NoError(t, e)
	raw, e := window.OpenOwnedChecked(options, "arc.native/ain1/v1", func(window.DeletionRecord) error { return window.ErrUnsafeGC })
	require.NoError(t, e)
	_, e = raw.Lookup(t.Context(), model.FeedBlock{Chain: options.Chain, Number: 1, Hash: n.blocks[1].block.Hash()}, model.DatasetHeaders)
	require.ErrorIs(t, e, window.ErrNotFound, "raw bytes must actually have been deleted after permanent archive proof")
	require.Greater(t, raw.Stats().Records, 0)
	var deletedRef window.Reference
	require.NoError(t, json.Unmarshal(archived.Datasets[0].Reference, &deletedRef))
	_, e = os.Stat(filepath.Join(options.Path, fmt.Sprintf("%020d.raw", deletedRef.Segment)))
	require.ErrorIs(t, e, os.ErrNotExist, "the original payload segment must actually be absent")
	require.NoError(t, raw.Close())
	r = start(t, cfg)
	r.wait(t, 131, 3, n.blocks[131].block.Hash())
	t.Log("phase: positive raw deletion, preserved original-generation SQL archive, native/source COW and post-GC restart verified")

	n.mu.Lock()
	n.deep = true
	n.mu.Unlock()
	require.Eventually(t, func() bool {
		c, e := r.store.ExecutionStatus(t.Context())
		return e == nil && (c.ReorgPending || c.Rewinding)
	}, time.Minute, 25*time.Millisecond, "deep fork below retained floor must fence publication")
	blocked, e := r.store.ExecutionStatus(t.Context())
	require.NoError(t, e)
	require.EqualValues(t, 131, blocked.Visible.Height)
	require.Equal(t, m.Ref, blocked.Anchor)
	require.ErrorIs(t, r.store.ReadSnapshot(t.Context(), func(context.Context) error { return nil }), port.ErrExecutionRewinding)
	r.once.Do(func() {
		r.cancel()
		select {
		case <-r.done:
			require.Error(t, r.runErr)
		case <-time.After(30 * time.Second):
			t.Fatal("fenced App did not join")
		}
		r.a.Shutdown()
	})
	t.Log("phase: deep fork below retained floor fences API/publication and joins without rewriting original anchor")
	n.mu.Lock()
	calls := append([]rpcCall(nil), n.calls...)
	physical := append([]time.Time(nil), n.physical...)
	n.mu.Unlock()
	require.NotEmpty(t, calls)
	for _, call := range calls {
		require.Contains(t, []string{"web3_clientVersion", "eth_chainId", "eth_blockNumber", "eth_getBlockByNumber"}, call.Method)
	}
	require.Greater(t, len(physical), 131, "actual shared final-canonical transport must be used")
	t.Log("qualified owned official Arc App/ETH68/native first+second execution/SQL seven parts/API/ACK/restart/reorg/retention; no operating chain or performance claim")
}

func TestArcNativeIntegratedRefusesPendingAndWrongRoot(t *testing.T) {
	for _, kind := range []string{"pending", "root"} {
		t.Run(kind, func(t *testing.T) {
			input, m, n := fixture(t)
			n.change(1, false)
			if kind == "root" {
				b := n.blocks[1].block
				h := b.Header()
				h.Root[0] ^= 1
				bad := b.WithSeal(h)
				encoded, e := rlp.EncodeToBytes(bad)
				require.NoError(t, e)
				old := n.blocks[1]
				old.block = bad
				old.encoded = encoded
				n.blocks[1] = old
			}
			peer := n.peer(t)
			cfg, _, replayLog := configFor(t, ownedRoot(t), input, m, n, peer)
			if kind == "pending" {
				path := filepath.Join(cfg.Source.Execution.Ingress.Native.DataDir, ".arc-native-import.pending")
				fault := []byte("owned incomplete intent")
				require.NoError(t, os.WriteFile(path, fault, 0600))
				a, e := app.NewApp(cfg, zap.NewNop(), false, "arc")
				if a != nil {
					a.Shutdown()
				}
				require.Error(t, e)
				after, e := os.ReadFile(path)
				require.NoError(t, e)
				require.Equal(t, fault, after)
				return
			}
			r := start(t, cfg)
			select {
			case <-r.done:
				require.Error(t, r.runErr)
				require.NotErrorIs(t, r.runErr, context.Canceled)
			case <-time.After(time.Minute):
				t.Fatal("SDK must refuse incorrect state root")
			}
			c, e := r.store.ExecutionStatus(t.Context())
			require.NoError(t, e)
			require.Equal(t, m.Ref, c.Visible)
			native, e := arcingress.InspectNativeControl(t.Context(), cfg.Source.Execution.Ingress.Native.DataDir)
			require.NoError(t, e)
			require.Equal(t, native.Identity.Anchor, native.Head.Tip)
			raw, e := arcingress.InspectRawControl(t.Context(), cfg.WindowStorage.Windows["raw"].Path)
			require.NoError(t, e)
			require.EqualValues(t, 1, raw.Control.Received.Height)
			if contents, e := os.ReadFile(replayLog); e == nil {
				require.NotContains(t, string(contents), "ARC_ACCEPTANCE_EVM replay 1 ")
			}
			r.once.Do(func() { r.cancel(); r.a.Shutdown() })
		})
	}
}
