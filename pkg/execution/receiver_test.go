package execution

import (
	"context"
	"crypto/sha256"
	"errors"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func receiverFixture(t *testing.T, b *controlledBackend) (*Receiver, execwire.Result, []byte) {
	t.Helper()
	chain, result, payload := preparedFixture(t)
	store := newControllerStore()
	store.status.ExecutionConfig = port.ExecutionConfig{Chain: chain, Anchor: execwire.Ref{Generation: 1, Hash: result.Parent}}
	store.status.Visible = store.status.Anchor
	r := &Receiver{Chain: chain, Profile: evm.New("evm"), Store: store, Window: 2, Timeout: time.Second, Confirm: func(context.Context, []byte) (bool, error) { return false, nil }}
	r.OpenController = func(ctx context.Context) (*Controller, error) {
		return NewController(ctx, Limits{MaxBlocks: 2, MaxBytes: 16 << 20}, store, b, func(context.Context) error { store.recover(); return nil })
	}
	return r, result, payload
}

func receiverNext(t *testing.T, previous execwire.Result) (execwire.Result, []byte) {
	t.Helper()
	var old types.Block
	require.NoError(t, rlp.DecodeBytes(previous.BlockRLP, &old))
	h := old.Header()
	h.Number = new(big.Int).SetUint64(previous.Ref.Height + 1)
	h.ParentHash = common.Hash(previous.Ref.Hash)
	h.Time++
	b := types.NewBlockWithHeader(h).WithBody(*old.Body())
	r := previous
	r.Parent = previous.Ref.Hash
	r.Ref.Height++
	r.Ref.Hash = execwire.Hash(b.Hash())
	r.Time = h.Time
	var err error
	r.BlockRLP, err = rlp.EncodeToBytes(b)
	require.NoError(t, err)
	payload, err := execwire.MarshalResult(r)
	require.NoError(t, err)
	return r, payload
}

func TestReceiverPipelinesNextPartsWhilePublicationWaits(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	b := &controlledBackend{pubBlock: make(chan struct{}), pubEntered: make(chan struct{}, 2)}
	r, first, payload := receiverFixture(t, b)
	second, payload2 := receiverNext(t, first)
	ctx, conn, finish, _ := startReceiverSession(t, r)
	defer finish()
	require.NoError(t, conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: execwire.Ref{Generation: 1, Hash: first.Parent}}}))
	_, err := conn.Read(ctx)
	require.NoError(t, err)
	for i, data := range [][]byte{payload, payload2} {
		ref := first.Ref
		if i == 1 {
			ref = second.Ref
		}
		parts, err := execwire.Split(r.Chain, ref, data, execwire.MaxFrameBytes)
		require.NoError(t, err)
		for _, part := range parts {
			require.NoError(t, conn.Write(ctx, part))
		}
		if i == 0 {
			select {
			case <-b.pubEntered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	require.Eventually(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return len(b.seen[port.ExecutionBlocks]) == 2 }, time.Second, time.Millisecond)
	close(b.pubBlock)
	for _, ref := range []execwire.Ref{first.Ref, second.Ref} {
		ack, err := conn.Read(ctx)
		require.NoError(t, err)
		require.Equal(t, execwire.Committed, ack.Kind)
		require.Equal(t, ref, ack.Header.Ref)
	}
}

func TestReceiverLostACKConfirmsPrefixWithoutReapplyingParts(t *testing.T) {
	b := &controlledBackend{}
	r, result, payload := receiverFixture(t, b)
	store := r.Store.(*controllerStoreProbe)
	store.status.Visible = result.Ref
	confirmed := 0
	r.Confirm = func(_ context.Context, p []byte) (bool, error) {
		confirmed++
		require.Equal(t, payload, p)
		return true, nil
	}
	ctx, conn, finish, _ := startReceiverSession(t, r)
	defer finish()
	require.NoError(t, conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: execwire.Ref{Generation: 1, Hash: result.Parent}}}))
	hello, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, result.Ref, hello.Header.Ref)
	parts, err := execwire.Split(r.Chain, result.Ref, payload, execwire.MaxFrameBytes)
	require.NoError(t, err)
	for _, part := range parts {
		require.NoError(t, conn.Write(ctx, part))
	}
	ack, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, execwire.Committed, ack.Kind)
	require.Equal(t, 1, confirmed)
	b.mu.Lock()
	require.Empty(t, b.seen)
	require.Empty(t, b.published)
	b.mu.Unlock()
}

func TestReceiverHelloNeverTriggersRewindOrACK(t *testing.T) {
	r, result, _ := receiverFixture(t, &controlledBackend{})
	r.Reorg = func(context.Context, *Controller) error {
		t.Error("Hello triggered rewind")
		return port.ErrExecutionConflict
	}
	ctx, conn, finish, _ := startReceiverSession(t, r)
	defer finish()
	require.NoError(t, conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: result.Ref}}))
	hello, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, execwire.Hello, hello.Kind)
	require.Zero(t, hello.Header.Ref.Height)
	require.Equal(t, uint64(1), hello.Header.Ref.Generation)
}

func TestReceiverRewindRequestWaitsForCanonicalView(t *testing.T) {
	for _, lag := range []bool{true, false} {
		t.Run(map[bool]string{true: "lag", false: "converged"}[lag], func(t *testing.T) {
			r, result, _ := receiverFixture(t, &controlledBackend{})
			store := r.Store.(*controllerStoreProbe)
			unavailable := errors.New("canonical view has not converged")
			validated, rewound := 0, 0
			r.ValidateReorg = func(_ context.Context, ref execwire.Ref) error {
				validated++
				require.Equal(t, result.Ref, ref)
				if lag {
					return unavailable
				}
				return nil
			}
			r.Reorg = func(ctx context.Context, c *Controller) error {
				rewound++
				return c.Rewind(ctx, store.status.Visible, execwire.Hash{9})
			}
			ctx, conn, finish, done := startReceiverSession(t, r)
			defer finish()
			require.NoError(t, conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: execwire.Ref{Generation: 1, Hash: result.Parent}}}))
			_, err := conn.Read(ctx)
			require.NoError(t, err)
			require.NoError(t, conn.Write(ctx, execwire.Frame{Kind: execwire.Rewind, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: result.Ref}}))
			select {
			case err := <-done:
				require.Error(t, err)
				if lag {
					require.ErrorIs(t, err, unavailable)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.Equal(t, 1, validated)
			want := uint64(1)
			if !lag {
				want = 2
				require.Equal(t, 1, rewound)
			} else {
				require.Zero(t, rewound)
			}
			status, err := store.ExecutionStatus(ctx)
			require.NoError(t, err)
			require.Equal(t, want, status.Generation)
		})
	}
}

func TestReceiverFreshDetectedReorgReturnsRetryableNewGeneration(t *testing.T) {
	r, result, payload := receiverFixture(t, &controlledBackend{})
	store := r.Store.(*controllerStoreProbe)
	store.status.Visible = result.Ref
	reorg := errors.New("fresh canonical changed")
	r.Confirm = func(context.Context, []byte) (bool, error) { return false, reorg }
	r.ReorgOnError = func(err error) bool { return errors.Is(err, reorg) }
	r.Reorg = func(ctx context.Context, c *Controller) error {
		return c.Rewind(ctx, store.status.Anchor, execwire.Hash{9})
	}
	ctx, conn, finish, done := startReceiverSession(t, r)
	defer finish()
	require.NoError(t, conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: store.status.Anchor}}))
	_, err := conn.Read(ctx)
	require.NoError(t, err)
	parts, err := execwire.Split(r.Chain, result.Ref, payload, execwire.MaxFrameBytes)
	require.NoError(t, err)
	for _, part := range parts {
		require.NoError(t, conn.Write(ctx, part))
	}
	select {
	case err := <-done:
		require.ErrorIs(t, err, port.ErrExecutionRewinding)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	status, err := store.ExecutionStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), status.Generation)
}

func startReceiverSession(t *testing.T, r *Receiver) (context.Context, *execwire.Conn, func(), <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	server, client := net.Pipe()
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() { defer close(exited); defer func() { _ = server.Close() }(); done <- r.serveConn(ctx, server) }()
	conn := execwire.NewConn(client)
	finish := func() {
		cancel()
		_ = conn.Close()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("receiver did not join")
		}
	}
	return ctx, conn, finish, done
}

func TestReceiverACKRequiresPublicationAndJoinsCancellation(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	b := &controlledBackend{pubBlock: make(chan struct{}), pubEntered: make(chan struct{}, 1)}
	r, result, payload := receiverFixture(t, b)
	ctx, conn, finish, _ := startReceiverSession(t, r)
	defer finish()
	anchor := execwire.Ref{Generation: 1, Hash: result.Parent}
	require.NoError(t, conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: r.Chain, Ref: anchor}}))
	hello, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, execwire.Hello, hello.Kind)
	require.Equal(t, anchor, hello.Header.Ref)
	parts, err := execwire.Split(r.Chain, result.Ref, payload, execwire.MaxHeaderBytes+128)
	require.NoError(t, err)
	for _, part := range parts {
		require.NoError(t, conn.Write(ctx, part))
	}
	select {
	case <-b.pubEntered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	wait, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = conn.Read(wait)
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(b.pubBlock)
	ack, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, execwire.Committed, ack.Kind)
	require.Equal(t, result.Ref, ack.Header.Ref)
	require.Equal(t, execwire.Hash(sha256.Sum256(payload)), ack.Header.Digest)
}

func TestReceiverRejectsIdentityAndIncompletePartsWithoutACK(t *testing.T) {
	for _, kind := range []string{"chain", "payload_ref", "partial"} {
		t.Run(kind, func(t *testing.T) {
			b := &controlledBackend{}
			r, result, payload := receiverFixture(t, b)
			ctx, conn, finish, done := startReceiverSession(t, r)
			defer finish()
			anchor := execwire.Ref{Generation: 1, Hash: result.Parent}
			chain := r.Chain
			if kind == "chain" {
				chain.ID = "2"
			}
			require.NoError(t, conn.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: anchor}}))
			if kind != "chain" {
				_, err := conn.Read(ctx)
				require.NoError(t, err)
				ref := result.Ref
				if kind == "payload_ref" {
					ref.Hash[0]++
				}
				parts, err := execwire.Split(r.Chain, ref, payload, execwire.MaxHeaderBytes+128)
				require.NoError(t, err)
				if kind == "partial" {
					parts = parts[:1]
				}
				for _, part := range parts {
					if err = conn.Write(ctx, part); err != nil {
						break
					}
				}
				if kind == "partial" {
					_ = conn.Close()
				}
			}
			select {
			case err := <-done:
				require.Error(t, err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			b.mu.Lock()
			require.Empty(t, b.published)
			b.mu.Unlock()
		})
	}
}
