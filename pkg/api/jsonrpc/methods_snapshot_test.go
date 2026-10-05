package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/storage"
)

// TestMethodsStayServed calls every method listed in testdata/methods.txt:
// none may answer "method not found", so moving handlers between packages
// cannot drop a method unnoticed. Add new methods to the list.
func TestMethodsStayServed(t *testing.T) {
	f, err := os.Open("testdata/methods.txt")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	st, err := storage.NewPebbleStorage(storage.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	srv := NewServer(st, zap.NewNop())

	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		method := strings.TrimSpace(sc.Text())
		if method == "" || strings.HasPrefix(method, "#") {
			continue
		}
		n++
		for _, params := range []string{"{}", "[]"} {
			_, rpcErr := srv.HandleMethodDirect(context.Background(), method, json.RawMessage(params))
			if rpcErr != nil {
				require.NotEqual(t, MethodNotFound, rpcErr.Code, "%s is no longer served", method)
			}
		}
	}
	require.NoError(t, sc.Err())
	require.Greater(t, n, 0)
}
