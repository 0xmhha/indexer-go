package rest

import (
	"context"
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/require"
)

type executionSnapshotExecutor struct{ calls int }

func (*executionSnapshotExecutor) FreshSnapshotRequired() bool { return true }
func (e *executionSnapshotExecutor) Execute(ctx context.Context, _ string, _ map[string]interface{}) *graphql.Result {
	e.calls++
	return &graphql.Result{Data: map[string]interface{}{"generation": e.calls, "cancellable": ctx.Done() != nil}}
}
func TestExecutionRESTDoesNotReuseOrDetachPublicSnapshot(t *testing.T) {
	e := &executionSnapshotExecutor{}
	h := NewHandler(e)
	first := serve(h, "GET", "/v1/blocks", nil)
	second := serve(h, "GET", "/v1/blocks", nil)
	require.Equal(t, 2, e.calls, "a response from the old generation must not bypass a new public snapshot")
	require.NotEqual(t, first.Body.String(), second.Body.String())
	require.Equal(t, "no-store", first.Header().Get("Cache-Control"))
	require.Empty(t, first.Header().Get("ETag"))
}
