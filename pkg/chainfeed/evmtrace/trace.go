// Package evmtrace captures provider-reported call trees without executing an
// EVM. Header/transaction/receipt consistency is checked; internal execution
// and completeness cannot be proved by those commitments.
package evmtrace

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

const (
	Codec            = model.CallTraceCodec
	MaxResponseBytes = 4 << 20
	MaxFieldBytes    = 64 << 10
	MaxTransactions  = 4096
	MaxFrames        = 16384
	MaxDepth         = 64
	MaxErrorBytes    = 4096
)

var ErrTrace = errors.New("evmtrace: invalid, inconsistent or oversized call trace")
var ErrUnavailable = errors.New("evmtrace: hash-selected call tracing unavailable")

// Definition is an operator-pinned source label and fixed decoding contract.
// Provider is never an endpoint, credential or claim of independent execution.
type Definition = model.CallTraceDefinition

// Frame paths are deterministic child ordinals; [] is the transaction root.
// Value may be absent, particularly for STATICCALL. Never infer a native coin
// transfer from this field, DELEGATECALL context or a reverted subtree.
type Frame struct {
	TransactionHash  common.Hash     `json:"transactionHash"`
	TransactionIndex uint32          `json:"transactionIndex"`
	Path             []uint32        `json:"path"`
	Type             string          `json:"type"`
	From             common.Address  `json:"from"`
	To               *common.Address `json:"to"`
	Value            *hexutil.Big    `json:"value"`
	Gas              hexutil.Uint64  `json:"gas"`
	GasUsed          hexutil.Uint64  `json:"gasUsed"`
	Input            *hexutil.Bytes  `json:"input"`
	Output           *hexutil.Bytes  `json:"output"`
	Error            string          `json:"error,omitempty"`
	RevertReason     string          `json:"revertReason,omitempty"`
	AncestorReverted bool            `json:"ancestorReverted"`
	Reverted         bool            `json:"reverted"`
}
type boundedBytes hexutil.Bytes

func (b *boundedBytes) UnmarshalJSON(raw []byte) error {
	if len(raw) > MaxFieldBytes*2+4 || bytes.Equal(raw, []byte("null")) {
		return ErrTrace
	}
	var v hexutil.Bytes
	if e := json.Unmarshal(raw, &v); e != nil {
		return ErrTrace
	}
	*b = boundedBytes(v)
	return nil
}

type wireFrame struct {
	Type         string          `json:"type"`
	From         *common.Address `json:"from"`
	To           *common.Address `json:"to"`
	Value        *hexutil.Big    `json:"value"`
	Gas          *hexutil.Uint64 `json:"gas"`
	GasUsed      *hexutil.Uint64 `json:"gasUsed"`
	Input        *boundedBytes   `json:"input"`
	Output       *boundedBytes   `json:"output"`
	Error        string          `json:"error"`
	RevertReason string          `json:"revertReason"`
	Calls        []wireFrame     `json:"calls"`
}
type wireResult struct {
	Hash   *common.Hash `json:"txHash"`
	Result *wireFrame   `json:"result"`
	Error  string       `json:"error"`
}

// guardJSON rejects duplicate fields and excessive depth/cardinality before
// decoding recursive call slices. Its bounds apply to the whole response.
func guardJSON(raw []byte) error {
	if len(raw) == 0 || len(raw) > MaxResponseBytes {
		return ErrTrace
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	objects, tokens := 0, 0
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 2*MaxDepth+3 {
			return ErrTrace
		}
		tokens++
		if tokens > MaxFrames*48+MaxTransactions*8 {
			return ErrTrace
		}
		t, e := d.Token()
		if e != nil {
			return ErrTrace
		}
		switch t {
		case json.Delim('{'):
			objects++
			if objects > MaxFrames+MaxTransactions {
				return ErrTrace
			}
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				s, ok := k.(string)
				if e != nil || !ok || len(s) > 32 || len(seen) >= 16 || seen[s] {
					return ErrTrace
				}
				seen[s] = true
				if e := walk(depth + 1); e != nil {
					return e
				}
			}
			t, e = d.Token()
			if e != nil || t != json.Delim('}') {
				return ErrTrace
			}
		case json.Delim('['):
			count := 0
			for d.More() {
				count++
				if count > MaxFrames || depth == 0 && count > MaxTransactions {
					return ErrTrace
				}
				if e := walk(depth + 1); e != nil {
					return e
				}
			}
			t, e = d.Token()
			if e != nil || t != json.Delim(']') {
				return ErrTrace
			}
		}
		return nil
	}
	if e := walk(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrTrace
	}
	return nil
}
func knownType(t string) bool {
	switch t {
	case "CALL", "CALLCODE", "STATICCALL", "DELEGATECALL", "CREATE", "CREATE2", "SELFDESTRUCT":
		return true
	}
	return false
}

// Decode checks every transaction root against profile-verified facts. Child
// calls remain provider reports; even a plausible omitted child is undetectable
// without independent execution. No state-root proof is implied.
func Decode(raw []byte, b *model.Block, receipts []*model.Receipt) ([]Frame, error) {
	if b == nil || len(b.Transactions) > MaxTransactions || len(receipts) != len(b.Transactions) || guardJSON(raw) != nil {
		return nil, ErrTrace
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, ErrTrace
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var roots []wireResult
	if e := d.Decode(&roots); e != nil || len(roots) != len(b.Transactions) {
		return nil, ErrTrace
	}
	out := make([]Frame, 0, len(roots))
	var visit func(wireFrame, uint32, []uint32, bool) error
	visit = func(f wireFrame, tx uint32, path []uint32, parentReverted bool) error {
		if len(path) > MaxDepth || len(out) >= MaxFrames || !knownType(f.Type) || f.From == nil || f.Gas == nil || f.GasUsed == nil || len(f.Error) > MaxErrorBytes || len(f.RevertReason) > MaxErrorBytes {
			return ErrTrace
		}
		failed := f.Error != ""
		if f.To == nil && ((f.Type != "CREATE" && f.Type != "CREATE2") || !failed) {
			return ErrTrace
		}
		if f.Value != nil && f.Value.ToInt().Sign() < 0 {
			return ErrTrace
		}
		if f.Type != "SELFDESTRUCT" && f.Input == nil {
			return ErrTrace
		}
		if f.Type == "SELFDESTRUCT" && len(f.Calls) > 0 {
			return ErrTrace
		}
		frame := Frame{TransactionHash: b.Transactions[tx].Hash, TransactionIndex: tx, Path: append([]uint32{}, path...), Type: f.Type, From: *f.From, To: f.To, Value: f.Value, Gas: *f.Gas, GasUsed: *f.GasUsed, Error: f.Error, RevertReason: f.RevertReason, AncestorReverted: parentReverted, Reverted: parentReverted || failed}
		if f.Input != nil {
			v := hexutil.Bytes(*f.Input)
			frame.Input = &v
		}
		if f.Output != nil {
			v := hexutil.Bytes(*f.Output)
			frame.Output = &v
		}
		out = append(out, frame)
		for i, child := range f.Calls {
			next := append(append([]uint32{}, path...), uint32(i))
			if e := visit(child, tx, next, frame.Reverted); e != nil {
				return e
			}
		}
		return nil
	}
	for i, w := range roots {
		tx, r := b.Transactions[i], receipts[i]
		if tx == nil || r == nil || tx.Opaque || tx.Hash == (common.Hash{}) || w.Hash == nil || *w.Hash != tx.Hash || w.Result == nil || w.Error != "" || r.TxHash != tx.Hash || r.BlockHash != b.Hash || r.BlockNumber != b.Number || r.TxIndex != uint(i) || r.Status > 1 {
			return nil, ErrTrace
		}
		f := w.Result
		if f.From == nil || *f.From != tx.From || f.Value == nil || tx.Value == nil || f.Value.ToInt().Cmp(tx.Value) != 0 || f.Gas == nil || uint64(*f.Gas) != tx.Gas || f.GasUsed == nil || uint64(*f.GasUsed) != r.GasUsed || f.Input == nil || !bytes.Equal([]byte(*f.Input), tx.Input) || (f.Error == "") != (r.Status == types.ReceiptStatusSuccessful) {
			return nil, ErrTrace
		}
		if tx.To != nil {
			if f.Type != "CALL" || f.To == nil || *f.To != *tx.To {
				return nil, ErrTrace
			}
		} else {
			if f.Type != "CREATE" {
				return nil, ErrTrace
			}
			if r.Status == types.ReceiptStatusSuccessful {
				if f.To == nil || r.ContractAddress == nil || *f.To != *r.ContractAddress {
					return nil, ErrTrace
				}
			} else if f.To != nil {
				return nil, ErrTrace
			}
		}
		if e := visit(*f, uint32(i), nil, false); e != nil {
			return nil, e
		}
	}
	return out, nil
}
