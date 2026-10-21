// Package rpcbatch identifies unusable JSON-RPC batch envelopes. Individual
// method, state and transport errors remain errors of their original reads.
package rpcbatch

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	"github.com/ethereum/go-ethereum/rpc"
)

var ErrUnsupported = errors.New("JSON-RPC state batch is unavailable")

func Unsupported(err error, elems []rpc.BatchElem) bool {
	var status rpc.HTTPError
	if errors.As(err, &status) {
		return status.StatusCode == http.StatusBadRequest || status.StatusCode == http.StatusRequestEntityTooLarge
	}
	var reply rpc.Error
	if errors.As(err, &reply) {
		return reply.ErrorCode() == -32600 // invalid batch envelope, not hash invalid-params
	}
	var shape *json.UnmarshalTypeError
	if errors.As(err, &shape) && shape.Value == "object" && shape.Type != nil && shape.Type.Kind() == reflect.Slice {
		return true // a single response cannot correlate the batch members
	}
	if err != nil || len(elems) == 0 {
		return false
	}
	for _, elem := range elems {
		if !errors.Is(elem.Error, rpc.ErrMissingBatchResponse) {
			return false
		}
	}
	return true
}
