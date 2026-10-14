package rpcbatch

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

type replyError int

func (e replyError) Error() string  { return "RPC error" }
func (e replyError) ErrorCode() int { return int(e) }

func TestStateBatchCapabilityPreservesReadAndQuotaErrors(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         error
		members     []rpc.BatchElem
		unsupported bool
	}{
		{"bad envelope", rpc.HTTPError{StatusCode: http.StatusBadRequest}, nil, true},
		{"envelope too large", rpc.HTTPError{StatusCode: http.StatusRequestEntityTooLarge}, nil, true},
		{"quota", rpc.HTTPError{StatusCode: http.StatusTooManyRequests}, nil, false},
		{"server unavailable", rpc.HTTPError{StatusCode: http.StatusServiceUnavailable}, nil, false},
		{"cancelled", context.Canceled, nil, false},
		{"invalid request", replyError(-32600), nil, true},
		{"invalid hash selector", nil, []rpc.BatchElem{{Error: replyError(-32602)}}, false},
		{"missing state", nil, []rpc.BatchElem{{Error: replyError(-32000)}}, false},
		{"all uncorrelated", nil, []rpc.BatchElem{{Error: rpc.ErrMissingBatchResponse}, {Error: rpc.ErrMissingBatchResponse}}, true},
		{"one missing response", nil, []rpc.BatchElem{{}, {Error: rpc.ErrMissingBatchResponse}}, false},
		{"object envelope", &json.UnmarshalTypeError{Value: "object", Type: reflect.TypeOf([]json.RawMessage{})}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.unsupported, Unsupported(tc.err, tc.members)) })
	}
}
