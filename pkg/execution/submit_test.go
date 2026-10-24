package execution

import (
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/stretchr/testify/require"
)

func submitServer(t *testing.T, chain execwire.Chain, result execwire.Result, payload []byte, mutate func(*execwire.Frame)) (net.Conn, <-chan error) {
	t.Helper()
	a, b := net.Pipe()
	done := make(chan error, 1)
	go func() {
		defer func() { _ = b.Close() }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c := execwire.NewConn(b)
		_, err := c.Read(ctx)
		if err != nil {
			done <- err
			return
		}
		anchor := execwire.Ref{Generation: result.Ref.Generation, Height: result.Ref.Height - 1, Hash: result.Parent}
		err = c.Write(ctx, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: anchor}})
		if err != nil {
			done <- err
			return
		}
		assembler, err := execwire.NewAssembler(execwire.Limits{Blocks: 1, Bytes: execwire.MaxBlockBytes, TTL: time.Minute})
		if err != nil {
			done <- err
			return
		}
		defer assembler.Reset()
		for {
			f, err := c.Read(ctx)
			if err != nil {
				done <- err
				return
			}
			got, complete, err := assembler.Add(time.Now(), f)
			if err != nil {
				done <- err
				return
			}
			if !complete {
				continue
			}
			if string(got) != string(payload) {
				done <- errors.New("payload changed")
				return
			}
			ack := execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: result.Ref, Digest: execwire.Hash(sha256.Sum256(payload))}}
			if mutate != nil {
				mutate(&ack)
			}
			done <- c.Write(ctx, ack)
			return
		}
	}()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	return a, done
}

func TestExecutionSubmitRequiresExactCommittedProof(t *testing.T) {
	chain, result, payload := preparedFixture(t)
	for _, mutate := range []func(*execwire.Frame){nil,
		func(f *execwire.Frame) { f.Header.Digest[0]++ },
		func(f *execwire.Frame) { f.Header.Ref.Hash[0]++ },
		func(f *execwire.Frame) { f.Header.Ref.Generation++ },
		func(f *execwire.Frame) { f.Header.Chain.ID = "2" },
		func(f *execwire.Frame) { f.Kind = execwire.Hello; f.Header.Digest = execwire.Hash{} },
	} {
		conn, done := submitServer(t, chain, result, payload, mutate)
		ack, err := SubmitResult(t.Context(), conn, evm.New("evm"), chain, payload)
		serverErr := <-done
		if mutate == nil {
			require.NoError(t, err, "only the exact persisted publication can complete the transfer")
			require.Equal(t, result.Ref, ack.Header.Ref)
		} else {
			require.Error(t, err)
		}
		require.NoError(t, serverErr)
	}
}

func TestExecutionSubmitHelloCannotPromoteOrRewriteGeneration(t *testing.T) {
	chain, result, payload := preparedFixture(t)
	for _, mutate := range []func(*execwire.Frame){
		func(f *execwire.Frame) { f.Header.Ref.Generation++ },
		func(f *execwire.Frame) { f.Header.Ref.Hash[0]++ },
		func(f *execwire.Frame) { f.Header.Chain.ID = "2" },
	} {
		a, b := net.Pipe()
		done := make(chan error, 1)
		go func() {
			defer func() { _ = b.Close() }()
			c := execwire.NewConn(b)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			hello, err := c.Read(ctx)
			if err == nil {
				mutate(&hello)
				err = c.Write(ctx, hello)
			}
			if err == nil {
				_, err = c.Read(ctx)
				if err == nil {
					done <- errors.New("invalid Hello promoted a result")
					return
				}
			}
			done <- nil
		}()
		_, err := SubmitResult(t.Context(), a, evm.New("evm"), chain, payload)
		require.Error(t, err)
		require.NoError(t, <-done)
	}
	require.Equal(t, uint64(1), result.Ref.Generation)
}

func TestExecutionSubmitCancellationClosesConnection(t *testing.T) {
	chain, _, payload := preparedFixture(t)
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer func() { _ = b.Close() }()
		c := execwire.NewConn(b)
		_, err := c.Read(t.Context())
		if err == nil {
			cancel()
			_, err = c.Read(t.Context())
		}
		done <- err
	}()
	_, err := SubmitResult(ctx, a, evm.New("evm"), chain, payload)
	require.ErrorIs(t, err, context.Canceled)
	require.Error(t, <-done)
}
