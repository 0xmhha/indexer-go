package execution

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/stretchr/testify/require"
)

func TestExecutionSubmitFilePreservesInputAndAuthenticatesSocket(t *testing.T) {
	chain, result, payload := preparedFixture(t)
	dir, err := os.MkdirTemp("", "e5-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	path := filepath.Join(dir, "block.exr")
	require.NoError(t, os.WriteFile(path, payload, 0600))
	socket := filepath.Join(dir, "s")
	listener, err := listenExecution(socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = conn.Close() }()
		// Forward to the bounded mock only after a real Unix connection exists.
		mock, mockDone := submitServer(t, chain, result, payload, nil)
		defer func() { _ = mock.Close() }()
		c, m := execwire.NewConn(conn), execwire.NewConn(mock)
		for _, fromClient := range []bool{true, false, true, false} {
			a, b := c, m
			if !fromClient {
				a, b = m, c
			}
			f, err := a.Read(context.Background())
			if err == nil {
				err = b.Write(context.Background(), f)
			}
			if err != nil {
				done <- err
				return
			}
		}
		done <- <-mockDone
	}()
	ack, err := SubmitFile(t.Context(), evm.New("evm"), chain, path, socket)
	require.NoError(t, err, "one verified file must reach the existing writer and return an exact ACK")
	require.Equal(t, result.Ref, ack.Header.Ref)
	require.NoError(t, <-done)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, payload, got)
	// Refuse broad permissions and a symlink before connecting to the socket.
	require.NoError(t, os.Chmod(path, 0644))
	_, err = SubmitFile(t.Context(), evm.New("evm"), chain, path, socket)
	require.Error(t, err)
	require.NoError(t, os.Chmod(path, 0600))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(path, link))
	_, err = SubmitFile(t.Context(), evm.New("evm"), chain, link, socket)
	require.Error(t, err)
}
