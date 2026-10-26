// Package executioncalls is the common, DB-only GraphQL/JSON-RPC contract.
package executioncalls

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"strconv"

	"github.com/0xmhha/indexer-go/execwire"
	nativecalls "github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace/native"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type Params struct {
	Address     string `json:"address"`
	FromBlock   string `json:"fromBlock"`
	ToBlock     string `json:"toBlock"`
	Limit       uint32 `json:"limit"`
	Selection   string `json:"selection"`
	OrphanEpoch string `json:"orphanEpoch"`
	Partial     bool   `json:"partial"`
	Cursor      string `json:"cursor"`
}

func (p Params) Query() (q port.ExecutionCallRangeQuery, err error) {
	if len(p.Address) != 42 || p.Address[:2] != "0x" || !common.IsHexAddress(p.Address) {
		return q, port.ErrInvalidData
	}
	q.Address = execwire.Address(common.HexToAddress(p.Address))
	q.FromBlock, err = strconv.ParseUint(p.FromBlock, 10, 63)
	if err != nil {
		return q, port.ErrInvalidData
	}
	q.ToBlock, err = strconv.ParseUint(p.ToBlock, 10, 63)
	if err != nil {
		return q, port.ErrInvalidData
	}
	q.Limit = p.Limit
	if q.Limit == 0 {
		q.Limit = 50
	}
	q.Selection = p.Selection
	if q.Selection == "" {
		q.Selection = "canonical"
	}
	if p.OrphanEpoch != "" {
		q.OrphanEpoch, err = strconv.ParseUint(p.OrphanEpoch, 10, 63)
		if err != nil || q.OrphanEpoch == 0 {
			return q, port.ErrInvalidData
		}
	}
	q.Partial = p.Partial
	q.Cursor = p.Cursor
	return q, q.Validate()
}

type ContractError struct {
	Code    string
	Missing []port.ExecutionCallMissingRange
}

func (e *ContractError) Error() string { return e.Code }
func (e *ContractError) Extensions() map[string]interface{} {
	out := map[string]interface{}{"code": e.Code}
	if e.Missing != nil {
		out["missing"] = missing(e.Missing)
	}
	return out
}
func Error(err error) *ContractError {
	e := &ContractError{Code: "execution_calls_unavailable"}
	switch {
	case errors.Is(err, port.ErrInvalidData):
		e.Code = "invalid_execution_call_query"
	case errors.Is(err, port.ErrExecutionStale):
		e.Code = "execution_call_cursor_stale"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		e.Code = "execution_call_cancelled"
	case errors.Is(err, port.ErrExecutionCoverage):
		e.Code = "execution_calls_not_collected"
		var c *port.ExecutionCallCoverageError
		if errors.As(err, &c) {
			e.Missing = c.Missing
		}
	}
	return e
}
func decimal(n uint64) string { return strconv.FormatUint(n, 10) }
func coverage(c port.ExecutionCallCoverage) map[string]interface{} {
	return map[string]interface{}{"height": decimal(c.Height), "collected": c.Collected, "epoch": decimal(c.Epoch), "generation": decimal(c.Ref.Generation), "blockHash": common.Hash(c.Ref.Hash).Hex(), "digest": common.Hash(c.Digest).Hex(), "profile": c.Profile}
}
func missing(ranges []port.ExecutionCallMissingRange) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, map[string]interface{}{"fromBlock": decimal(r.FromBlock), "toBlock": decimal(r.ToBlock)})
	}
	return out
}
func View(ctx context.Context, store any, p Params) (map[string]interface{}, error) {
	q, err := p.Query()
	if err != nil {
		return nil, Error(err)
	}
	reader, ok := store.(port.ExecutionCallRangeReader)
	if !ok {
		return nil, &ContractError{Code: "execution_calls_unavailable"}
	}
	result, err := reader.ExecutionCallRange(ctx, q)
	if err != nil {
		return nil, Error(err)
	}
	rows := make([]map[string]interface{}, 0, len(result.Calls))
	for _, item := range result.Calls {
		f := item.Call
		var to, value interface{}
		if f.To != nil {
			to = common.Address(*f.To).Hex()
		}
		if f.Value != nil {
			value = new(big.Int).SetBytes(f.Value[:]).String()
		}
		path := make([]int, len(f.Path))
		for i, n := range f.Path {
			path[i] = int(n)
		}
		rows = append(rows, map[string]interface{}{"coverage": coverage(item.Coverage), "verification": nativecalls.Verification, "ordinal": int(f.Ordinal), "parent": int(f.Parent), "path": path, "type": f.Type, "from": common.Address(f.From).Hex(), "to": to, "value": value, "transactionHash": common.Hash(f.TransactionHash).Hex(), "transactionIndex": int(f.TransactionIndex), "gas": decimal(f.Gas), "gasUsed": decimal(f.GasUsed), "input": hexutil.Encode(f.Input), "output": hexutil.Encode(f.Output), "error": f.Error, "internal": f.Internal, "reverted": f.Reverted, "ancestorReverted": f.AncestorReverted})
	}
	covered := make([]map[string]interface{}, 0, len(result.Coverage))
	for _, c := range result.Coverage {
		covered = append(covered, coverage(c))
	}
	return map[string]interface{}{"selection": result.Selection, "epoch": decimal(result.Epoch), "complete": result.Complete, "missing": missing(result.Missing), "coverage": covered, "calls": rows, "next": result.Next}, nil
}

// DecodeParams consumes one object of scalar parameters once. Duplicate keys,
// unknown keys and nulls cannot ambiguously opt a caller into partial coverage.
func DecodeParams(raw []byte) (p Params, err error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	start, e := dec.Token()
	if e != nil || start != json.Delim('{') {
		return p, port.ErrInvalidData
	}
	seen := map[string]bool{}
	for dec.More() {
		token, e := dec.Token()
		if e != nil {
			return p, port.ErrInvalidData
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return p, port.ErrInvalidData
		}
		seen[key] = true
		value, e := dec.Token()
		if e != nil {
			return p, port.ErrInvalidData
		}
		switch key {
		case "address", "fromBlock", "toBlock", "selection", "orphanEpoch", "cursor":
			str, ok := value.(string)
			if !ok {
				return p, port.ErrInvalidData
			}
			switch key {
			case "address":
				p.Address = str
			case "fromBlock":
				p.FromBlock = str
			case "toBlock":
				p.ToBlock = str
			case "selection":
				p.Selection = str
			case "orphanEpoch":
				p.OrphanEpoch = str
			case "cursor":
				p.Cursor = str
			}
		case "partial":
			b, ok := value.(bool)
			if !ok {
				return p, port.ErrInvalidData
			}
			p.Partial = b
		case "limit":
			n, ok := value.(json.Number)
			if !ok {
				return p, port.ErrInvalidData
			}
			limit, e := strconv.ParseUint(string(n), 10, 32)
			if e != nil || limit == 0 || limit > 256 {
				return p, port.ErrInvalidData
			}
			p.Limit = uint32(limit)
		default:
			return p, port.ErrInvalidData
		}
	}
	end, e := dec.Token()
	if e != nil || end != json.Delim('}') {
		return p, port.ErrInvalidData
	}
	if _, e = dec.Token(); e != io.EOF {
		return p, port.ErrInvalidData
	}
	return p, nil
}
