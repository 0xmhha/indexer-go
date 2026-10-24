package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/stretchr/testify/require"
)

func TestSubmitUsesExplicitArcIdentityAndRetainedFile(t *testing.T) {
	genesis := "0x" + strings.Repeat("11", 32)
	var out bytes.Buffer
	called := false
	err := run(t.Context(), []string{"--chain-id", "5042002", "--genesis", genesis, "--result", "/private/result.exr", "--socket", "/private/socket"}, &out,
		func(ctx context.Context, p chains.BinaryProfile, chain execwire.Chain, path, socket string) (execwire.Frame, error) {
			called = true
			require.Equal(t, "arc", p.ID())
			require.Equal(t, "5042002", chain.ID)
			require.Equal(t, byte(0x11), chain.Genesis[0])
			require.Equal(t, "/private/result.exr", path)
			require.Equal(t, "/private/socket", socket)
			_, bounded := ctx.Deadline()
			require.True(t, bounded)
			return execwire.Frame{Kind: execwire.Committed}, nil
		})
	require.NoError(t, err)
	require.True(t, called)
	require.Contains(t, out.String(), `"Kind":3`)
	for _, args := range [][]string{{}, {"--chain-id", "1"}, {"--chain-id", "5042002", "--genesis", "0x0"}, {"--timeout", "0s"}} {
		require.Error(t, run(t.Context(), args, &out, func(context.Context, chains.BinaryProfile, execwire.Chain, string, string) (execwire.Frame, error) {
			t.Fatal("invalid arguments reached the transport")
			return execwire.Frame{}, nil
		}))
	}
}

func TestSubmitHelpExplainsFlagsWithoutConnecting(t *testing.T) {
	var out bytes.Buffer
	err := run(t.Context(), []string{"--help"}, &out, func(context.Context, chains.BinaryProfile, execwire.Chain, string, string) (execwire.Frame, error) {
		t.Fatal("help must not connect or submit")
		return execwire.Frame{}, nil
	})
	require.NoError(t, err)
	for _, flag := range []string{"-chain-id", "-genesis", "-result", "-socket", "-timeout"} {
		require.Contains(t, out.String(), flag)
	}
}
