package jsonrpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

func init() {
	RegisterMethod("test_echo", func(_ context.Context, d MethodDeps, params json.RawMessage) (interface{}, *Error) {
		if d.Logger == nil {
			return nil, NewError(InternalError, "no logger", nil)
		}
		return string(params), nil
	})
}

func TestRegisteredMethodIsServed(t *testing.T) {
	st, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	srv := NewServer(st, zap.NewNop())
	got, rpcErr := srv.HandleMethodDirect(context.Background(), "test_echo", json.RawMessage(`[1]`))
	require.Nil(t, rpcErr)
	require.Equal(t, "[1]", got)
	_, rpcErr = srv.HandleMethodDirect(context.Background(), "test_missing", nil)
	require.Equal(t, MethodNotFound, rpcErr.Code)
}
