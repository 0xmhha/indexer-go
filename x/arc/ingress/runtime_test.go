package arcingress

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
	"math/big"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func rawFixture(t *testing.T, chain model.FeedChain, parent nativeRef, h uint64) []chainfeed.Envelope {
	t.Helper()
	header := &types.Header{Number: new(big.Int).SetUint64(h), ParentHash: common.Hash(parent.Hash), Root: common.Hash(parent.Root), TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, UncleHash: types.EmptyUncleHash, Difficulty: big.NewInt(0), GasLimit: 30000000}
	head, err := rlp.EncodeToBytes(header)
	require.NoError(t, err)
	body, err := rlp.EncodeToBytes([]rlp.RawValue{{0xc0}, {0xc0}})
	require.NoError(t, err)
	id := model.FeedBlock{Chain: chain, Number: h, Hash: header.Hash()}
	var out []chainfeed.Envelope
	for i, kind := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		out = append(out, chainfeed.Envelope{Block: id, Kind: kind, Codec: evmdata.Codec(kind), Payload: [][]byte{head, body, {0xc0}}[i]})
	}
	return out
}

type fixedCanonical map[uint64]common.Hash

func (f fixedCanonical) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	if err := ctx.Err(); err != nil {
		return common.Hash{}, err
	}
	h, ok := f[n]
	if !ok {
		return common.Hash{}, ErrNative
	}
	return h, nil
}

type batchFixture struct{ input <-chan []chainfeed.Envelope }

func (s batchFixture) NextBatch(ctx context.Context) ([]chainfeed.Envelope, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case b := <-s.input:
		return b, nil
	}
}
func (s batchFixture) Next(context.Context) (chainfeed.Envelope, error) {
	return chainfeed.Envelope{}, ErrNative
}
func (s batchFixture) Close() error { return nil }
func rawProgram(t *testing.T) *program {
	t.Helper()
	root := t.TempDir()
	chain := model.FeedChain{ID: "5042002", GenesisHash: common.Hash{1}}
	o := window.Options{Volume: window.VolumeSpec{Root: root, ID: "native-fixture", AllowDirectory: true}, Path: filepath.Join(root, "raw"), Chain: chain, MaxBytes: 8 << 20, SegmentBytes: 128 << 10, MaxPayloadBytes: 16 << 10, MaxSegmentRecords: 32, MaxSegments: 16, ReadBudgetBytes: 1 << 20, MaxReaders: 4, Retention: 24 * time.Hour}
	var err error
	o, err = o.Normalize()
	require.NoError(t, err)
	require.NoError(t, window.InitializeVolume(o.Volume))
	r, c := validNativeReady()
	r.Head = nativeHead{Version: 1, Instance: r.Identity.Instance, ImportEpoch: 1, ReadEpoch: 1, Tip: r.Identity.Anchor}
	c.Visible = c.Anchor
	c.Root = c.AnchorRoot
	control := rawControl{Version: 1, Instance: r.Identity.Instance, Chain: chain, Anchor: r.Identity.Anchor, RawEpoch: 1, Received: r.Identity.Anchor}
	state, err := json.Marshal(control)
	require.NoError(t, err)
	raw, err := window.CreateOwned(o, ownerDomain, state)
	require.NoError(t, err)
	p := &program{cfg: ingress.Config{MaxRawBlocks: 10, MaxActiveBlocks: 2, MaxActiveBytes: 8 << 20}.Normalize(), opts: o, raw: raw, control: control, identity: r.Identity, head: r.Head, slots: make(chan struct{}, 2), bytes: semaphore.NewWeighted(8 << 20)}
	p.deps.Control = c
	p.deps.Store = &mutableStatus{control: c}
	t.Cleanup(p.Stop)
	return p
}

type mutableStatus struct {
	mu      sync.Mutex
	control port.ExecutionControl
}

func (s *mutableStatus) ExecutionStatus(context.Context) (port.ExecutionControl, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.control, nil
}

type canonicalFunc func(context.Context, uint64) (common.Hash, error)

func (f canonicalFunc) HashAt(ctx context.Context, h uint64) (common.Hash, error) { return f(ctx, h) }
func TestNativeRawAdmissionRejectsGenerationChangeDuringFreshBeforeDispatch(t *testing.T) {
	p := rawProgram(t)
	batch := rawFixture(t, p.control.Chain, p.identity.Anchor, 1)
	require.NoError(t, p.persist(t.Context(), batch))
	s := p.deps.Store.(*mutableStatus)
	p.deps.Canonical = canonicalFunc(func(context.Context, uint64) (common.Hash, error) {
		s.mu.Lock()
		s.control.Generation++
		s.control.Rewinding = true
		s.mu.Unlock()
		return batch[0].Block.Hash, nil
	})
	release := make(chan struct{})
	close(release)
	w := &blockedWriter{identity: p.identity, started: make(chan struct{}), release: release}
	err := p.importNext(t.Context(), w, p.head, p.control.Chain, func([]window.Reference) error { t.Error("fenced native result advertised"); return nil })
	require.Error(t, err, "SQL generation changed while canonical response was in flight")
	select {
	case <-w.started:
		t.Fatal("native execution started after generation fence changed")
	default:
	}
}

type blockedWriter struct {
	identity nativeIdentity
	started  chan struct{}
	release  <-chan struct{}
	once     sync.Once
}

func (w *blockedWriter) Import(ctx context.Context, m importMeta, b, rs []byte, root, rr execHash) (importReceipt, error) {
	w.once.Do(func() { close(w.started) })
	select {
	case <-ctx.Done():
		return importReceipt{}, ctx.Err()
	case <-w.release:
	}
	_, digest, err := encodeInput(m, b, rs)
	return importReceipt{Version: 1, Identity: w.identity, RequestID: m.RequestID, ImportEpoch: m.ImportEpoch, ReadEpoch: m.RequestID + 1, Height: m.Height, Hash: m.Hash, Parent: m.Parent, ParentRoot: m.ParentRoot, Root: root, ReceiptsRoot: rr, InputDigest: digest}, err
}
func TestNativeCaptureContinuesWhileWriterBlockedAndCancellationJoins(t *testing.T) {
	p := rawProgram(t)
	batches := make(chan []chainfeed.Envelope, 3)
	canonical := fixedCanonical{}
	parent := p.identity.Anchor
	for h := uint64(1); h <= 3; h++ {
		b := rawFixture(t, p.control.Chain, parent, h)
		batches <- b
		canonical[h] = b[0].Block.Hash
		parent = nativeRef{Height: h, Hash: execwire.Hash(b[0].Block.Hash), Root: parent.Root}
	}
	p.deps.Canonical = canonical
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wake := make(chan struct{}, 1)
	captureDone := make(chan error, 1)
	writerDone := make(chan error, 1)
	release := make(chan struct{})
	w := &blockedWriter{identity: p.identity, started: make(chan struct{}), release: release}
	observed := make(chan uint64, 3)
	go func() { captureDone <- p.capture(ctx, batchFixture{input: batches}, wake) }()
	go func() {
		writerDone <- p.consume(ctx, w, wake, func(r []window.Reference) error { observed <- r[0].Block.Number; return nil })
	}()
	select {
	case <-w.started:
	case <-time.After(5 * time.Second):
		t.Fatal("native writer did not start")
	}
	require.Eventually(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.control.Received.Height == 3 }, 5*time.Second, time.Millisecond)
	p.mu.Lock()
	require.Zero(t, p.head.Tip.Height)
	p.mu.Unlock()
	require.Empty(t, observed, "raw-only blocks must not be advertised")
	close(release)
	for h := uint64(1); h <= 3; h++ {
		select {
		case got := <-observed:
			require.Equal(t, h, got)
		case <-time.After(5 * time.Second):
			t.Fatal("durable backlog did not drain")
		}
	}
	cancel()
	require.ErrorIs(t, <-captureDone, context.Canceled)
	require.ErrorIs(t, <-writerDone, context.Canceled)
	require.Len(t, p.slots, 0)
}
func TestNativeRawQuotaPreservesExactRetryPrefixAndNeverAdvancesControl(t *testing.T) {
	p := rawProgram(t)
	p.cfg.MaxRawBlocks = 1
	b := rawFixture(t, p.control.Chain, p.identity.Anchor, 1)
	_, err := p.raw.Append(t.Context(), b[0])
	require.NoError(t, err)
	require.NoError(t, p.persist(t.Context(), b), "durable partial prefix is not charged twice")
	b2 := rawFixture(t, p.control.Chain, nativeRef{Height: 1, Hash: execwire.Hash(b[0].Block.Hash), Root: p.identity.Anchor.Root}, 2)
	require.ErrorIs(t, p.persist(t.Context(), b2), window.ErrCapacity)
	require.Equal(t, uint64(1), p.control.Received.Height)
	require.Equal(t, 3, p.raw.Stats().Records)
	_, err = p.raw.Lookup(t.Context(), b[0].Block, b[0].Kind)
	require.NoError(t, err)
	require.True(t, errors.Is(p.persist(t.Context(), b), ErrNative), "repeated old capture cannot silently move intake backward")
}
func TestNativeRawVerificationPreservesExactHeaderAndRejectsIncompleteOrDiscontinuous(t *testing.T) {
	chain := model.FeedChain{ID: "5042002", GenesisHash: common.Hash{1}}
	parent := nativeRef{Hash: execwire.Hash{1}, Root: execwire.Hash{2}}
	batch := rawFixture(t, chain, parent, 1)
	b, err := verifyBatch(chain, parent, batch)
	require.NoError(t, err)
	require.Equal(t, batch, b.envelopes)
	var parts []rlp.RawValue
	require.NoError(t, rlp.DecodeBytes(b.encoded, &parts))
	require.Equal(t, []byte(batch[0].Payload), []byte(parts[0]))
	require.Equal(t, []byte{0xc0}, b.receipts)
	_, err = verifyBatch(chain, parent, batch[:2])
	require.Error(t, err)
	parent.Hash = execwire.Hash{7}
	_, err = verifyBatch(chain, parent, batch)
	require.Error(t, err)
}
