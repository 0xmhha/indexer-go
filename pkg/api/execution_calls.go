package api

import (
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"strconv"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.uber.org/zap"
)

func init() {
	RegisterRoute(Route{Method: http.MethodGet, Pattern: "/execution/traces/{kind}/{key}", Handler: nativeCallHandler})
}

type nativeCallFrame struct {
	evmtrace.Frame
	Ordinal  uint32 `json:"ordinal"`
	Parent   int32  `json:"parent"`
	Internal bool   `json:"internal"`
}
type nativeCallPage struct {
	Epoch        uint64            `json:"epoch"`
	Ref          execwire.Ref      `json:"ref"`
	Digest       execwire.Hash     `json:"digest"`
	Profile      string            `json:"profile"`
	Verification string            `json:"verification"`
	Total        uint32            `json:"total"`
	Frames       []nativeCallFrame `json:"frames"`
	Next         *uint32           `json:"next"`
}

func callQuery(r *http.Request) (q port.ExecutionCallQuery, err error) {
	return parseExecutionQuery(r, 256, 50, execwire.MaxCallFrames, true)
}
func parseExecutionQuery(r *http.Request, maxLimit, defaultLimit, maxAfter uint64, allowInternal bool) (q port.ExecutionCallQuery, err error) {
	q.Limit = uint32(defaultLimit)
	query, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return q, port.ErrInvalidData
	}
	for k, v := range query {
		if len(v) != 1 {
			return q, port.ErrInvalidData
		}
		switch k {
		case "limit", "after", "epoch":
			n, e := strconv.ParseUint(v[0], 10, 64)
			if e != nil {
				return q, port.ErrInvalidData
			}
			switch k {
			case "limit":
				if n == 0 || n > maxLimit {
					return q, port.ErrInvalidData
				}
				q.Limit = uint32(n)
			case "after":
				if n > maxAfter {
					return q, port.ErrInvalidData
				}
				q.After = uint32(n)
			case "epoch":
				if n == 0 {
					return q, port.ErrInvalidData
				}
				q.Epoch = n
			}
		case "hash":
			if e := q.Hash.UnmarshalText([]byte(v[0])); e != nil {
				return q, port.ErrInvalidData
			}
			if q.Hash == (execwire.Hash{}) {
				return q, port.ErrInvalidData
			}
		case "internal":
			if !allowInternal {
				return q, port.ErrInvalidData
			}
			if v[0] != "true" && v[0] != "false" {
				return q, port.ErrInvalidData
			}
			q.InternalOnly = v[0] == "true"
		default:
			return q, port.ErrInvalidData
		}
	}
	if (q.Epoch == 0) != (q.Hash == (execwire.Hash{})) || q.After > 0 && q.Epoch == 0 {
		return q, port.ErrInvalidData
	}
	switch URLParam(r, "kind") {
	case "blocks":
		q.Height, err = strconv.ParseUint(URLParam(r, "key"), 10, 63)
	case "transactions":
		hash := execwire.Hash{}
		err = hash.UnmarshalText([]byte(URLParam(r, "key")))
		if hash == (execwire.Hash{}) {
			err = port.ErrInvalidData
		}
		q.Transaction = &hash
	default:
		err = port.ErrInvalidData
	}
	if err != nil {
		return q, port.ErrInvalidData
	}
	return q, nil
}
func nativeCallHandler(store port.QueryStore, _ *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		fail := func(status int, code string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
		}
		q, e := callQuery(r)
		if e != nil {
			fail(400, "invalid_execution_call_query")
			return
		}
		reader, ok := store.(port.ExecutionCallReader)
		if !ok {
			fail(503, "execution_calls_unavailable")
			return
		}
		p, e := reader.ExecutionCalls(r.Context(), q)
		if e != nil {
			switch {
			case errors.Is(e, port.ErrNotFound):
				fail(404, "execution_calls_not_collected")
			case errors.Is(e, port.ErrInvalidData):
				fail(400, "invalid_execution_call_query")
			case errors.Is(e, port.ErrExecutionStale):
				fail(409, "execution_call_cursor_stale")
			case errors.Is(e, port.ErrExecutionRewinding), errors.Is(e, port.ErrExecutionUninitialized), errors.Is(e, port.ErrExecutionMaintenance):
				fail(503, "execution_calls_unavailable")
			default:
				fail(500, "execution_calls_error")
			}
			return
		}
		out := nativeCallPage{Epoch: p.Epoch, Ref: p.Ref, Digest: p.Digest, Profile: p.Profile, Verification: p.Verification, Total: p.Total, Next: p.Next, Frames: make([]nativeCallFrame, 0, len(p.Frames))}
		for _, f := range p.Frames {
			var target *common.Address
			if f.To != nil {
				a := common.Address(*f.To)
				target = &a
			}
			var value *hexutil.Big
			if f.Value != nil {
				v := new(big.Int).SetBytes(f.Value[:])
				value = (*hexutil.Big)(v)
			}
			input, output := hexutil.Bytes(f.Input), hexutil.Bytes(f.Output)
			out.Frames = append(out.Frames, nativeCallFrame{Ordinal: f.Ordinal, Parent: f.Parent, Internal: f.Internal, Frame: evmtrace.Frame{TransactionHash: common.Hash(f.TransactionHash), TransactionIndex: f.TransactionIndex, Path: f.Path, Type: f.Type, From: common.Address(f.From), To: target, Value: value, Gas: hexutil.Uint64(f.Gas), GasUsed: hexutil.Uint64(f.GasUsed), Input: &input, Output: &output, Error: f.Error, AncestorReverted: f.AncestorReverted, Reverted: f.Reverted}})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}
