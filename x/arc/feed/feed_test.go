package arcfeed_test

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rlp"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/ethpeer"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/rpcpool"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/storage/postgres"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/0xmhha/indexer-go/x/arc"
	arcfeed "github.com/0xmhha/indexer-go/x/arc/feed"
)

type oracle struct {
	chain *testchain.Chain
	wrong atomic.Bool
	calls atomic.Int32
}

func (o *oracle) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	if err := ctx.Err(); err != nil {
		return common.Hash{}, err
	}
	o.calls.Add(1)
	if n == 1 && o.wrong.Load() {
		return common.Hash{99}, nil
	}
	b := o.chain.Block(n)
	if b == nil {
		return common.Hash{}, errors.New("fixture missing")
	}
	return b.Block.Hash(), nil
}
func fixtures(t *testing.T) (*testchain.Chain, *testchain.EthPeerServer, *oracle, ethpeer.Config, capture.SessionConfig) {
	t.Helper()
	a := testchain.NewAccount(1)
	to := testchain.NewAccount(2).Address
	c := testchain.NewChain(5042002, map[common.Address]*big.Int{a.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)})
	for i := 0; i < 3; i++ {
		c.AddBlock(testchain.TxSpec{From: a, Tx: &types.DynamicFeeTx{To: &to, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(10)}, GasUsed: 21000, Failed: i == 1, Logs: []*types.Log{{Address: to, Topics: []common.Hash{{4}}, Data: []byte("fixture")}}})
	}
	c.SetHead(3)
	s, err := testchain.NewEthPeerServer(c)
	require.NoError(t, err)
	t.Cleanup(s.Close)
	ch := model.FeedChain{ID: "5042002", GenesisHash: c.Block(0).Block.Hash()}
	anchor := model.FeedBlock{Chain: ch, Hash: ch.GenesisHash}
	peer := ethpeer.Config{Chain: ch, NetworkID: 5042002, Checkpoint: anchor, ForkID: ethpeer.ForkID{Hash: s.Fork}, Key: testchain.NewAccount(4).Key, Peers: []*enode.Node{s.Node()}, Timeout: time.Second, PollInterval: time.Millisecond}
	root := t.TempDir()
	vol := window.VolumeSpec{Root: root, ID: "arc-pipeline", AllowDirectory: true, MaxBytes: 6 << 20}
	require.NoError(t, window.InitializeVolume(vol))
	layout := window.Layout{Volumes: map[string]window.VolumeSpec{"test": vol}, Windows: map[string]window.WindowConfig{"arc": {Volume: "test", Path: filepath.Join(root, "raw"), MaxBytes: 4 << 20, SegmentBytes: 64 << 10, MaxPayloadBytes: 32 << 10, MaxSegmentRecords: 8}}}
	return c, s, &oracle{chain: c}, peer, capture.SessionConfig{Storage: layout, Instance: "arc", Plan: evmdata.BasePlan(), Anchor: anchor, Create: true}
}
func db(t *testing.T) *storage.PebbleStorage {
	t.Helper()
	s, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s
}
func until3(cancel context.CancelFunc) func(model.CaptureIdentity) {
	return func(id model.CaptureIdentity) {
		if id.Block.Number == 3 {
			cancel()
		}
	}
}

func TestArcPeerCaptureRoundTrip(t *testing.T) {
	c, server, fresh, peer, session := fixtures(t)
	store := db(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := arcfeed.Run(ctx, store, fresh, peer, session, capture.PipelineOptions{QueueReferences: 1, OnCaptured: until3(cancel)})
	require.ErrorIs(t, err, context.Canceled)
	facts, err := capture.New(store, peer.Chain, fresh)
	require.NoError(t, err)
	state, err := facts.State(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 3, state.Through.Number)
	for n := uint64(1); n <= 3; n++ {
		digest, _ := session.Plan.Digest()
		in, err := facts.Read(context.Background(), model.CaptureIdentity{Block: model.FeedBlock{Chain: peer.Chain, Number: n, Hash: c.Block(n).Block.Hash()}, Generation: 1, PlanVersion: 1, PlanDigest: digest})
		require.NoError(t, err)
		require.Len(t, in.Payloads, 3)
		for _, p := range in.Payloads {
			var expected []byte
			switch p.Kind {
			case model.DatasetHeaders:
				expected, err = rlp.EncodeToBytes(c.Block(n).Block.Header())
			case model.DatasetBodies:
				expected, err = rlp.EncodeToBytes(c.Block(n).Block.Body())
			case model.DatasetReceipts:
				expected, err = rlp.EncodeToBytes(c.Block(n).Receipts)
			}
			require.NoError(t, err)
			require.Equal(t, expected, p.Bytes)
		}
	}
	require.GreaterOrEqual(t, server.Requests(), 9)
	require.GreaterOrEqual(t, fresh.calls.Load(), int32(7), "bootstrap and each commit keep fresh reads")
	opts, err := session.Storage.OptionsFor("arc", peer.Chain)
	require.NoError(t, err)
	raw, err := facts.OpenWindow(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, 9, raw.Stats().Records)
	require.NoError(t, raw.Close())
}

func TestArcCanonicalChecksUseSharedLimitedRPC(t *testing.T) {
	c, _, _, peer, session := fixtures(t)
	node := testchain.NewServer(c)
	t.Cleanup(node.Close)
	pool, err := rpcpool.New(rpcpool.Config{Endpoints: []string{node.URL()}, RateLimit: 5, Burst: 1, CoalesceReads: true})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rpc, err := gethrpc.DialOptions(ctx, pool.Primary(), gethrpc.WithHTTPClient(&http.Client{Transport: pool}))
	require.NoError(t, err)
	defer rpc.Close()
	store := db(t)
	fresh := sourcerpc.New(rpc, arc.New())
	require.ErrorIs(t, arcfeed.Run(ctx, store, fresh, peer, session, capture.PipelineOptions{OnCaptured: until3(cancel)}), context.Canceled)
	loads := node.BlockLoads()
	for n := uint64(1); n <= 3; n++ {
		require.Equal(t, 1, loads[n], "each capture commit requires a fresh canonical HTTP read")
	}
	calls := node.Calls()
	require.EqualValues(t, calls["eth_getBlockByNumber"], pool.Status()[0].Requests, "all canonical reads traverse the shared pool")
	require.Zero(t, calls["eth_getBlockReceipts"], "execution payloads come from P2P")
	require.Zero(t, calls["eth_getBalance"], "unsupported state is never silently supplemented")
}

type gated struct {
	v                evmdata.Verifier
	entered, release chan struct{}
	once             atomic.Bool
}

func (g *gated) Assemble(ctx context.Context, w *window.Store, s capture.State, refs []window.Reference) (capture.Input, error) {
	if g.once.CompareAndSwap(false, true) {
		close(g.entered)
		select {
		case <-g.release:
		case <-ctx.Done():
			return capture.Input{}, ctx.Err()
		}
	}
	return g.v.Assemble(ctx, w, s, refs)
}
func TestArcIntakeContinuesWhileProcessorWaits(t *testing.T) {
	_, _, fresh, peer, session := fixtures(t)
	store := db(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := arcfeed.New(ctx, peer, fresh)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	g := &gated{v: evmdata.Verifier{Profile: arc.New(), Provenance: "test-eth68"}, entered: make(chan struct{}), release: make(chan struct{})}
	received := make(chan struct{})
	done := make(chan error, 1)
	r, p, err := capture.PipelineTasks(client, g, capture.PipelineOptions{QueueReferences: 1, PollInterval: time.Millisecond, OnCaptured: until3(cancel), OnReceived: func(ref window.Reference) {
		if ref.Block.Number == 3 && ref.Kind == model.DatasetReceipts {
			close(received)
		}
	}})
	require.NoError(t, err)
	opts, err := session.Storage.OptionsFor("arc", peer.Chain)
	require.NoError(t, err)
	go func() {
		done <- capture.RunSession(ctx, store, fresh, capture.SessionOptions{Window: opts, Plan: session.Plan, Anchor: session.Anchor, Create: true}, r, p)
	}()
	select {
	case <-g.entered:
	case err := <-done:
		t.Fatalf("processor did not enter: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-received:
	case err := <-done:
		t.Fatalf("intake blocked behind processor: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	facts, err := capture.New(store, peer.Chain, fresh)
	require.NoError(t, err)
	state, err := facts.State(ctx)
	require.NoError(t, err)
	require.Zero(t, state.Through.Number, "durable raw is not a capture ACK")
	close(g.release)
	require.ErrorIs(t, <-done, context.Canceled)
	state, err = facts.State(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 3, state.Through.Number)
}

func TestArcPartialRawRestart(t *testing.T) {
	c, _, fresh, peer, session := fixtures(t)
	store := db(t)
	opts, err := session.Storage.OptionsFor("arc", peer.Chain)
	require.NoError(t, err)
	raw, err := window.Create(opts)
	require.NoError(t, err)
	header, err := rlp.EncodeToBytes(c.Block(1).Block.Header())
	require.NoError(t, err)
	body, err := rlp.EncodeToBytes(c.Block(1).Block.Body())
	require.NoError(t, err)
	for i, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies} {
		_, err := raw.Append(context.Background(), chainfeed.Envelope{Block: model.FeedBlock{Chain: peer.Chain, Number: 1, Hash: c.Block(1).Block.Hash()}, Kind: k, Codec: evmdata.Codec(k), Payload: [][]byte{header, body}[i]})
		require.NoError(t, err)
	}
	require.NoError(t, raw.Close())
	session.Create = false
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.ErrorIs(t, arcfeed.Run(ctx, store, fresh, peer, session, capture.PipelineOptions{OnCaptured: until3(cancel)}), context.Canceled)
	raw, err = window.Open(opts)
	require.NoError(t, err)
	require.Equal(t, 9, raw.Stats().Records, "partial inputs are deduplicated and resumed")
	require.NoError(t, raw.Close())
}

func TestArcRejectsCorruptionAndStaleCanonical(t *testing.T) {
	for _, mode := range []string{"body", "receipts", "request-id", "fresh"} {
		t.Run(mode, func(t *testing.T) {
			_, server, fresh, peer, session := fixtures(t)
			store := db(t)
			if mode == "fresh" {
				fresh.wrong.Store(true)
			} else {
				server.SetHook(func(code uint64, p *testchain.EthPacket) {
					if mode == "request-id" {
						p.ID++
					}
					if mode == "body" && code == 6 {
						v, _ := rlp.EncodeToBytes(&types.Body{})
						p.Items = []rlp.RawValue{v}
					}
					if mode == "receipts" && code == 16 {
						p.Items = []rlp.RawValue{{0xc0}}
					}
				})
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := arcfeed.Run(ctx, store, fresh, peer, session, capture.PipelineOptions{})
			require.Error(t, err)
			facts, e := capture.New(store, peer.Chain, fresh)
			require.NoError(t, e)
			state, e := facts.State(context.Background())
			require.NoError(t, e)
			require.Zero(t, state.Through.Number)
			if mode == "fresh" {
				require.ErrorIs(t, err, capture.ErrCanonical)
			}
		})
	}
}

func postgresDB(t *testing.T) *postgres.Store {
	t.Helper()
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("requires disposable fsync-on PostgreSQL")
	}
	schema := "arc_peer_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	store, err := postgres.Open(context.Background(), postgres.Options{DSN: dsn, Schema: schema, MaxConns: 4})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
		c, err := pgx.Connect(context.Background(), dsn)
		require.NoError(t, err)
		_, err = c.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, err)
		require.NoError(t, c.Close(context.Background()))
	})
	return store
}

func TestArcPeerCapturePostgres(t *testing.T) {
	store := postgresDB(t)
	_, _, fresh, peer, session := fixtures(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.ErrorIs(t, arcfeed.Run(ctx, store, fresh, peer, session, capture.PipelineOptions{QueueReferences: 1, OnCaptured: until3(cancel)}), context.Canceled)
	facts, err := capture.New(store, peer.Chain, fresh)
	require.NoError(t, err)
	state, err := facts.State(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 3, state.Through.Number)
	events, err := facts.Events(context.Background(), 0, 20)
	require.NoError(t, err)
	require.Len(t, events, 3)
}

func TestArcMissingReceiptIsExplicitGap(t *testing.T) {
	_, server, fresh, peer, session := fixtures(t)
	store := db(t)
	server.SetHook(func(code uint64, p *testchain.EthPacket) {
		if code == 16 {
			p.Items = nil
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := arcfeed.Run(ctx, store, fresh, peer, session, capture.PipelineOptions{})
	require.ErrorIs(t, err, ethpeer.ErrHistoryGap)
	facts, err := capture.New(store, peer.Chain, fresh)
	require.NoError(t, err)
	state, err := facts.State(context.Background())
	require.NoError(t, err)
	require.Zero(t, state.Through.Number)
}
func TestArcUnsupportedPlanFailsBeforePeerOrDisk(t *testing.T) {
	_, server, fresh, peer, session := fixtures(t)
	session.Plan.Required = append(session.Plan.Required, model.DatasetRequirement{Kind: model.DatasetStorage, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}})
	err := arcfeed.Run(context.Background(), nil, fresh, peer, session, capture.PipelineOptions{})
	require.ErrorIs(t, err, chainfeed.ErrUnsupported)
	require.Zero(t, server.Requests())
	require.Zero(t, fresh.calls.Load())
}
