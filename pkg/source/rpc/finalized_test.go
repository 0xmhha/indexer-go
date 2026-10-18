package rpc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/source/rpc"
)

func TestFinalizedCheckpointNeverFallsBack(t *testing.T) {
	for _, test := range []struct {
		name, result       string
		valid, unavailable bool
	}{
		{"valid", `{"number":"0x2","hash":"0x0000000000000000000000000000000000000000000000000000000000000002"}`, true, false},
		{"null", `null`, false, true},
		{"missing-number", `{"hash":"0x0000000000000000000000000000000000000000000000000000000000000002"}`, false, false},
		{"zero-hash", `{"number":"0x2","hash":"0x0000000000000000000000000000000000000000000000000000000000000000"}`, false, false},
		{"bad-number", `{"number":"latest","hash":"0x0000000000000000000000000000000000000000000000000000000000000002"}`, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := make(chan []json.RawMessage, 2)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					ID     json.RawMessage   `json:"id"`
					Params []json.RawMessage `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				requests <- q.Params
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, q.ID, test.result)
			}))
			defer s.Close()
			n, h, err := rpc.New(dial(t, s.URL), evm.New("evm")).FinalizedCheckpoint(context.Background())
			if test.valid {
				require.NoError(t, err)
				require.EqualValues(t, 2, n)
				require.Equal(t, common.Hash{31: 2}, h)
			} else {
				require.Error(t, err)
			}
			if test.unavailable {
				require.ErrorIs(t, err, source.ErrFinalityUnavailable)
			}
			params := <-requests
			require.JSONEq(t, `"finalized"`, string(params[0]))
			require.JSONEq(t, `false`, string(params[1]))
			require.Empty(t, requests)
		})
	}
}
