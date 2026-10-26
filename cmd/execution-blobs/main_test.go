package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestExecutionBlobCommandDelegatesOfflineAndValidatesIdentity(t *testing.T) {
	args := []string{"--chain-id", "1", "--genesis", "0x" + strings.Repeat("11", 32), "--result", "/private/original.exr", "--sidecars", "/private/blob.rlp", "--output", "/private/complete.exr", "--provider", "retained-sidecars"}
	var out bytes.Buffer
	called := false
	fn := func(ctx context.Context, p chains.BinaryProfile, c execwire.Chain, in, sidecar, target, provider string) (execution.BlobAttachment, error) {
		called = true
		require.Equal(t, "evm", p.ID())
		require.Equal(t, "1", c.ID)
		require.Equal(t, "/private/blob.rlp", sidecar)
		require.Equal(t, "/private/original.exr", in)
		require.Equal(t, "/private/complete.exr", target)
		require.Equal(t, "retained-sidecars", provider)
		_, ok := ctx.Deadline()
		require.True(t, ok)
		return execution.BlobAttachment{Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash{2}}}, nil
	}
	require.NoError(t, run(t.Context(), args, &out, fn))
	require.True(t, called, "command must actually call the offline validated composer")
	require.Contains(t, out.String(), `"ref"`)
	called = false
	require.Error(t, run(t.Context(), nil, &out, fn))
	require.False(t, called)
	bad := append(append([]string{}, args...), "--timeout", "2h")
	require.Error(t, run(t.Context(), bad, &out, fn))
	require.False(t, called)
	failure := errors.New("composition failed")
	require.ErrorIs(t, run(t.Context(), args, &out, func(context.Context, chains.BinaryProfile, execwire.Chain, string, string, string, string) (execution.BlobAttachment, error) {
		return execution.BlobAttachment{}, failure
	}), failure)
	out.Reset()
	require.NoError(t, run(t.Context(), []string{"--help"}, &out, fn))
	require.Contains(t, out.String(), "sidecars")
}
