package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
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
func TestExecutionJSONRPCReadSnapshotAndRewindError(t *testing.T) {
	s := &executionSnapshotProbe{mockStorage: &mockStorage{}}
	h := NewHandler(s, zap.NewNop())
	defer h.Close()
	_, err := h.HandleMethod(t.Context(), "getLatestHeight", nil)
	require.Nil(t, err)
	require.Equal(t, 1, s.calls)
	s.fail = port.ErrExecutionRewinding
	_, err = h.HandleMethod(t.Context(), "getLatestHeight", nil)
	require.NotNil(t, err)
	require.Contains(t, err.Message, "snapshot")
}

type executionStateProbe struct{ *executionSnapshotProbe }

func (s executionStateProbe) VisibleExecutionAccount(ctx context.Context, _ execwire.Address) (port.ExecutionAccountView, error) {
	if ctx.Value(snapshotMarker{}) != true {
		return port.ExecutionAccountView{}, errors.New("missing snapshot")
	}
	return port.ExecutionAccountView{Account: execwire.Account{Exists: true, Nonce: 18446744073709551615, Balance: execwire.Word{31: 9}}}, nil
}
func (s executionStateProbe) VisibleExecutionStorage(context.Context, execwire.Address, execwire.Word) (port.ExecutionStorageView, error) {
	return port.ExecutionStorageView{Value: execwire.Word{31: 7}}, nil
}
func (s executionStateProbe) VisibleExecutionCode(context.Context, execwire.Address) (port.ExecutionCodeView, error) {
	return port.ExecutionCodeView{Code: []byte{0x60, 0}}, nil
}
func TestExecutionJSONRPCStateValuesAndStrictParameters(t *testing.T) {
	s := executionStateProbe{&executionSnapshotProbe{mockStorage: &mockStorage{}}}
	h := NewHandler(s, zap.NewNop())
	defer h.Close()
	result, err := h.HandleMethod(t.Context(), "getExecutionAccount", json.RawMessage(`{"address":"0x0000000000000000000000000000000000000001"}`))
	require.Nil(t, err)
	require.Equal(t, "18446744073709551615", result.(map[string]interface{})["nonce"])
	_, err = h.HandleMethod(t.Context(), "getExecutionAccount", json.RawMessage(`{"address":"0x12"}`))
	require.Equal(t, InvalidParams, err.Code)
	_, err = h.HandleMethod(t.Context(), "getExecutionStorage", json.RawMessage(`{"address":"0x0000000000000000000000000000000000000001","slot":"0x01"}`))
	require.Equal(t, InvalidParams, err.Code)
}
