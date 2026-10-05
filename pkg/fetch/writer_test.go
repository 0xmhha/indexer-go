package fetch

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestWriterRunsCommandsInOrder(t *testing.T) {
	w := newWriter(zap.NewNop())
	ctx := context.Background()
	var mu sync.Mutex
	var order []int
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		i := i
		require.NoError(t, w.do(ctx, "seq", func(context.Context) error {
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			return nil
		}))
	}
	// Concurrent senders: commands never overlap.
	running := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = w.do(ctx, "overlap", func(context.Context) error {
				mu.Lock()
				running++
				require.Equal(t, 1, running, "commands run one at a time")
				running--
				mu.Unlock()
				return nil
			})
		}()
	}
	wg.Wait()
	for i := range order {
		require.Equal(t, i, order[i])
	}

	errBoom := errors.New("boom")
	require.ErrorIs(t, w.do(ctx, "err", func(context.Context) error { return errBoom }), errBoom)
	require.ErrorContains(t, w.do(ctx, "panic", func(context.Context) error { panic("bad") }), "panicked")
	require.NoError(t, w.do(ctx, "after panic", func(context.Context) error { return nil }), "the writer survives a panic")

	// A command issuing another command runs it inline instead of deadlocking.
	require.NoError(t, w.do(ctx, "outer", func(ctx context.Context) error {
		return w.do(ctx, "inner", func(context.Context) error { return nil })
	}))

	w.close()
	require.ErrorIs(t, w.do(ctx, "late", func(context.Context) error { return nil }), ErrWriterClosed)
	w.close() // idempotent
}

// TestOnlyWriterOpensBlockTransactions keeps every state change on the
// writer goroutine: BeginBlock may only be called in writer.go.
func TestOnlyWriterOpensBlockTransactions(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "writer.go" {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "BeginBlock" {
				t.Errorf("%s calls BeginBlock outside the writer", fset.Position(sel.Pos()))
			}
			return true
		})
	}
}
