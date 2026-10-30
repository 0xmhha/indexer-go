package execution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

type originProbe func(context.Context, port.ExecutionIngressInput) (port.ExecutionIngressArchive, func(), error)

func (f originProbe) ResolveExecutionOrigin(ctx context.Context, in port.ExecutionIngressInput) (port.ExecutionIngressArchive, func(), error) {
	return f(ctx, in)
}

type originPartProbe struct {
	partProbe
	archives      int
	beforeArchive func()
}

func (s *originPartProbe) ApplyExecutionIngress(ctx context.Context, a port.ExecutionIngressArchive) error {
	if ctx.Value(backendKey{}) != port.ExecutionBlocks || a.Ref.Height == 0 {
		return port.ErrExecutionConflict
	}
	s.archives++
	if s.beforeArchive != nil {
		s.beforeArchive()
	}
	return nil
}

func ingressArchiveForInput(t *testing.T, in port.ExecutionIngressInput) port.ExecutionIngressArchive {
	t.Helper()
	parts, err := evmdata.Items(in.BlockRLP, 4)
	require.NoError(t, err)
	body, err := rlp.EncodeToBytes(parts[1:])
	require.NoError(t, err)
	a := port.ExecutionIngressArchive{Version: 1, OwnerDigest: execwire.Hash{9}, Ref: in.Ref, Parent: in.Parent, Digest: in.Digest, InputDigest: execwire.Hash{8}, NativeEpoch: 2, NativeReadEpoch: 9, NativeRequestID: 1, ObservedRawEpoch: 17, Provenance: []byte(`{}`)}
	for i, kind := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		payload := [][]byte{parts[0], body, in.ReceiptsRLP}[i]
		a.Datasets = append(a.Datasets, port.ExecutionIngressDataset{Kind: kind, Codec: evmdata.Codec(kind), Reference: []byte(`{}`), Digest: execwire.Hash(sha256.Sum256(payload)), PayloadBytes: uint64(len(payload)), Payload: bytes.Clone(payload)})
	}
	return a
}

func TestStoreBackendOriginUsesTheSameBlocksCallbackAndReleasesOnSQLFailure(t *testing.T) {
	chain, _, raw := preparedFixture(t)
	p, err := Prepare(evm.New("evm"), chain, raw)
	require.NoError(t, err)
	store := &originPartProbe{}
	calls, released := 0, 0
	store.beforeArchive = func() { require.Equal(t, 0, released) }
	b := &StoreBackend{Store: store, Origin: originProbe(func(ctx context.Context, in port.ExecutionIngressInput) (port.ExecutionIngressArchive, func(), error) {
		require.Nil(t, ctx.Value(backendKey{}))
		calls++
		return ingressArchiveForInput(t, in), func() { released++ }, nil
	})}
	for _, w := range port.ExecutionWriters() {
		require.NoError(t, b.Part(t.Context(), w, p))
	}
	require.Equal(t, 1, calls)
	require.Equal(t, 1, released)
	require.Equal(t, 1, store.archives)
	require.Len(t, store.parts, 7)
	failure := errors.New("SQL admission canceled")
	store.fail = failure
	require.ErrorIs(t, b.Part(t.Context(), port.ExecutionBlocks, p), failure)
	require.Equal(t, 2, calls)
	require.Equal(t, 2, released)
}

func TestIngressArchiveLosslessOpaqueHeaderAndOptionalWithdrawals(t *testing.T) {
	for _, withdrawals := range []bool{false, true} {
		header, err := rlp.EncodeToBytes([]rlp.RawValue{{0x80}, {0xc1, 0x80}, {0x82, 0xab, 0xcd}})
		require.NoError(t, err)
		items := []rlp.RawValue{{0xc0}, {0xc0}}
		if withdrawals {
			items = append(items, rlp.RawValue{0xc0})
		}
		block, err := rlp.EncodeToBytes(append([]rlp.RawValue{header}, items...))
		require.NoError(t, err)
		in := port.ExecutionIngressInput{Ref: execwire.Ref{Generation: 7, Height: 11, Hash: execwire.Hash{1}}, Parent: execwire.Hash{2}, Digest: execwire.Hash{3}, BlockRLP: block, ReceiptsRLP: []byte{0xc0}}
		a := ingressArchiveForInput(t, in)
		require.NoError(t, matchIngressArchive(a, in))
		require.Equal(t, header, a.Datasets[0].Payload)
		require.NotEqual(t, a.NativeEpoch, a.Ref.Generation)
		in.BlockRLP = append(bytes.Clone(block), 0)
		require.Error(t, matchIngressArchive(a, in))
	}
	// Even if matching full bytes were supplied, a header string is no header
	// list. The layout checker must reject it before SQL; opaque list fields stay.
	in := port.ExecutionIngressInput{Ref: execwire.Ref{Generation: 7, Height: 11, Hash: execwire.Hash{1}}, Parent: execwire.Hash{2}, Digest: execwire.Hash{3}, BlockRLP: []byte{0xc3, 0x80, 0xc0, 0xc0}, ReceiptsRLP: []byte{0xc0}}
	a := ingressArchiveForInput(t, in)
	require.Error(t, matchIngressArchive(a, in), "non-list header must be refused")
}

func TestStoreBackendConcurrentOriginsOwnInputAndJoinCancellation(t *testing.T) {
	chain, _, raw := preparedFixture(t)
	p, err := Prepare(evm.New("evm"), chain, raw)
	require.NoError(t, err)
	original := bytes.Clone(p.result.BlockRLP)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	arrived, gate := make(chan struct{}, 2), make(chan struct{})
	open := sync.OnceFunc(func() { close(gate) })
	var calls, releases atomic.Int32
	store := &partProbe{}
	failure := errors.New("origin interrupted before SQL")
	b := &StoreBackend{Store: store, Origin: originProbe(func(ctx context.Context, in port.ExecutionIngressInput) (port.ExecutionIngressArchive, func(), error) {
		i := calls.Add(1) - 1
		in.BlockRLP[i] ^= 0xff
		arrived <- struct{}{}
		select {
		case <-gate:
		case <-ctx.Done():
		}
		return port.ExecutionIngressArchive{}, func() { releases.Add(1) }, failure
	})}
	var wg sync.WaitGroup
	var results [2]error
	wg.Add(2)
	for i := range 2 {
		go func() { defer wg.Done(); results[i] = b.Part(ctx, port.ExecutionBlocks, p) }()
	}
	defer func() { cancel(); open(); wg.Wait() }()
	for range 2 {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	require.Equal(t, original, p.result.BlockRLP)
	cancel()
	wg.Wait()
	for _, err := range results {
		require.ErrorIs(t, err, context.Canceled)
	}
	require.EqualValues(t, 2, releases.Load())
	require.Empty(t, store.parts)
}

func TestStoreBackendOriginFailurePrecedesAnySQLAndAppliesOnlyToBlocks(t *testing.T) {
	chain, _, raw := preparedFixture(t)
	p, err := Prepare(evm.New("evm"), chain, raw)
	require.NoError(t, err)
	store := &originPartProbe{}
	failure := errors.New("raw/native origin unavailable")
	calls, released := 0, 0
	b := &StoreBackend{Store: store, Origin: originProbe(func(ctx context.Context, in port.ExecutionIngressInput) (port.ExecutionIngressArchive, func(), error) {
		calls++
		require.Nil(t, ctx.Value(backendKey{}), "native/raw resolution must be outside SQL")
		require.Equal(t, p.Ref(), in.Ref)
		require.Equal(t, p.Digest(), in.Digest)
		require.True(t, bytes.Equal(in.BlockRLP, p.result.BlockRLP))
		in.BlockRLP[0] ^= 0xff
		return port.ExecutionIngressArchive{}, func() { released++ }, failure
	})}
	original := bytes.Clone(p.result.BlockRLP)
	require.ErrorIs(t, b.Part(t.Context(), port.ExecutionBlocks, p), failure)
	require.Equal(t, 1, calls)
	require.Equal(t, 1, released)
	require.Empty(t, store.parts, "failed origin cannot open a transaction or advance a cursor")
	require.Equal(t, original, p.result.BlockRLP, "resolver cannot mutate shared Prepared input")
	require.NoError(t, b.Part(t.Context(), port.ExecutionAccounts, p))
	require.Equal(t, 1, calls)
}
