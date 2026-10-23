package graphql

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type snapshotMarker struct{}
type executionSnapshotProbe struct {
	*mockStorage
	calls int
	fail  error
}

func (s *executionSnapshotProbe) ReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	s.calls++
	if s.fail != nil {
		return s.fail
	}
	return fn(context.WithValue(ctx, snapshotMarker{}, true))
}
func (s *executionSnapshotProbe) GetLatestHeight(ctx context.Context) (uint64, error) {
	if ctx.Value(snapshotMarker{}) != true {
		return 0, errors.New("missing read snapshot")
	}
	return 9, nil
}
func TestExecutionGraphQLReadSnapshotCoversHTTPAndFixedDocuments(t *testing.T) {
	s := &executionSnapshotProbe{mockStorage: &mockStorage{}}
	h, err := NewHandler(s, zap.NewNop())
	require.NoError(t, err)
	result := h.Execute(t.Context(), `{ latestHeight }`, nil)
	require.Empty(t, result.Errors)
	require.Equal(t, 1, s.calls)
	var rr *httptest.ResponseRecorder
	// Explicit JSON media type is part of the adapter's request contract.
	req := httptest.NewRequest("POST", "/graphql", strings.NewReader(`{"query":"{ latestHeight }"}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Contains(t, rr.Body.String(), `"latestHeight":"9"`)
	require.Equal(t, 2, s.calls)
	s.fail = port.ErrExecutionRewinding
	result = h.Execute(t.Context(), `{ latestHeight }`, nil)
	require.NotEmpty(t, result.Errors)
	require.Contains(t, result.Errors[0].Message, "rewind")
}
func TestExecutionGraphQLMutationKeepsWritableContext(t *testing.T) {
	s := &executionSnapshotProbe{mockStorage: &mockStorage{}}
	b := NewSchemaBuilder(s, zap.NewNop()).WithCoreQueries()
	b.mutations["writeProbe"] = &graphql.Field{Type: graphql.Boolean, Resolve: func(p graphql.ResolveParams) (interface{}, error) {
		if p.Context.Value(snapshotMarker{}) != nil {
			return nil, errors.New("mutation inside read snapshot")
		}
		return true, nil
	}}
	schema, err := b.Build()
	require.NoError(t, err)
	h := &Handler{schema: schema, logger: zap.NewNop()}
	result := h.Execute(t.Context(), `mutation { writeProbe }`, nil)
	require.Empty(t, result.Errors)
	require.Zero(t, s.calls)
}

type executionStateProbe struct{ *executionSnapshotProbe }

func (s executionStateProbe) VisibleExecutionAccount(ctx context.Context, _ execwire.Address) (port.ExecutionAccountView, error) {
	if ctx.Value(snapshotMarker{}) != true {
		return port.ExecutionAccountView{}, errors.New("missing snapshot")
	}
	return port.ExecutionAccountView{ExecutionStatePoint: port.ExecutionStatePoint{Ref: execwire.Ref{Generation: 2, Height: 3, Hash: execwire.Hash{1}}, Root: execwire.Hash{2}}, Account: execwire.Account{Exists: true, Nonce: 18446744073709551615, Balance: execwire.Word{31: 9}, Extra: 18446744073709551615}}, nil
}
func (s executionStateProbe) VisibleExecutionStorage(context.Context, execwire.Address, execwire.Word) (port.ExecutionStorageView, error) {
	return port.ExecutionStorageView{Value: execwire.Word{31: 7}}, nil
}
func (s executionStateProbe) VisibleExecutionCode(context.Context, execwire.Address) (port.ExecutionCodeView, error) {
	return port.ExecutionCodeView{Code: []byte{0x60, 0}}, nil
}
func TestExecutionGraphQLStateValuesPreservePrecision(t *testing.T) {
	s := executionStateProbe{&executionSnapshotProbe{mockStorage: &mockStorage{}}}
	h, err := NewHandler(s, zap.NewNop())
	require.NoError(t, err)
	result := h.Execute(t.Context(), `{ executionAccount(address:"0x0000000000000000000000000000000000000001") { height generation nonce balance extra exists } executionCode(address:"0x0000000000000000000000000000000000000001") { code } }`, nil)
	require.Empty(t, result.Errors)
	values := result.Data.(map[string]interface{})
	account := values["executionAccount"].(map[string]interface{})
	require.Equal(t, "18446744073709551615", account["nonce"])
	require.Equal(t, "9", account["balance"])
	require.Equal(t, "18446744073709551615", account["extra"])
	require.Equal(t, "0x6000", values["executionCode"].(map[string]interface{})["code"])
}
