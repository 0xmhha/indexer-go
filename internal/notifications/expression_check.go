package notifications

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// ExpressionCheck is a dry run of a setting's expressions (the
// subscriptions design's phase 5b): they are compiled as registration
// does and, given a sample log or transaction, evaluated once over it.
// Nothing is stored or sent.
type ExpressionCheck struct {
	// Event is the filter event signature (filter.event), if any.
	Event     string `json:"event,omitempty"`
	Condition string `json:"condition,omitempty"`
	Payload   string `json:"payload,omitempty"`
	// At most one sample event: a log or a transaction.
	Log         *SampleLog         `json:"log,omitempty"`
	Transaction *SampleTransaction `json:"transaction,omitempty"`
	// Block is the sample event's block.
	Block *SampleBlock `json:"block,omitempty"`
}

// SampleLog is a log as eth_getLogs gives it (hex strings).
type SampleLog struct {
	Address         string   `json:"address"`
	Topics          []string `json:"topics"`
	Data            string   `json:"data"`
	Index           uint64   `json:"index"`
	TransactionHash string   `json:"transactionHash,omitempty"`
}

// SampleTransaction is a transaction; Value is decimal.
type SampleTransaction struct {
	Hash  string `json:"hash"`
	From  string `json:"from"`
	To    string `json:"to,omitempty"`
	Value string `json:"value,omitempty"`
}

// SampleBlock is the block of a sample event; Time is Unix seconds.
type SampleBlock struct {
	Number uint64 `json:"number"`
	Time   uint64 `json:"time"`
	Hash   string `json:"hash,omitempty"`
}

// ExpressionCheckResult is the outcome of a dry run. Valid reports whether
// the expressions would be accepted at registration (Error says why not).
// With a sample event, Notify reports whether it would be notified, with
// Result (the payload's value) and Decoded (the filter event's
// arguments); Error then holds an evaluation error or why the sample is
// not the filter event.
type ExpressionCheckResult struct {
	Valid   bool              `json:"valid"`
	Error   string            `json:"error,omitempty"`
	Notify  bool              `json:"notify"`
	Result  json.RawMessage   `json:"result,omitempty"`
	Decoded map[string]string `json:"decoded,omitempty"`
}

// errSampleInput reports a malformed sample, as opposed to expressions
// that do not compile or evaluate (reported in the result).
var errSampleInput = errors.New("invalid sample")

func sampleHash(field, s string) (common.Hash, error) {
	if s == "" {
		return common.Hash{}, nil
	}
	b, err := hexutil.Decode(s)
	if err != nil || len(b) != common.HashLength {
		return common.Hash{}, fmt.Errorf("%w: %s %q is not a 32-byte hex value", errSampleInput, field, s)
	}
	return common.BytesToHash(b), nil
}

func sampleAddress(field, s string) (common.Address, error) {
	if !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("%w: %s %q is not an address", errSampleInput, field, s)
	}
	return common.HexToAddress(s), nil
}

// sampleEvent builds the event of a check's sample, nil without one.
func (in *ExpressionCheck) sampleEvent() (events.Event, error) {
	if in.Log != nil && in.Transaction != nil {
		return nil, fmt.Errorf("%w: give a log or a transaction, not both", errSampleInput)
	}
	var blk SampleBlock
	if in.Block != nil {
		blk = *in.Block
	}
	blockHash, err := sampleHash("block.hash", blk.Hash)
	if err != nil {
		return nil, err
	}
	at := time.Unix(int64(blk.Time), 0)
	switch {
	case in.Log != nil:
		addr, err := sampleAddress("log.address", in.Log.Address)
		if err != nil {
			return nil, err
		}
		l := &types.Log{Address: addr, BlockNumber: blk.Number, BlockHash: blockHash, Index: uint(in.Log.Index)}
		for i, t := range in.Log.Topics {
			h, err := sampleHash(fmt.Sprintf("log.topics[%d]", i), t)
			if err != nil {
				return nil, err
			}
			l.Topics = append(l.Topics, h)
		}
		if in.Log.Data != "" {
			if l.Data, err = hexutil.Decode(in.Log.Data); err != nil {
				return nil, fmt.Errorf("%w: log.data is not hex: %v", errSampleInput, err)
			}
		}
		if l.TxHash, err = sampleHash("log.transactionHash", in.Log.TransactionHash); err != nil {
			return nil, err
		}
		ev := events.NewLogEvent(l)
		ev.CreatedAt = at
		return ev, nil
	case in.Transaction != nil:
		tx := in.Transaction
		e := &events.TransactionEvent{BlockNumber: blk.Number, BlockHash: blockHash, Value: tx.Value, CreatedAt: at}
		if e.Hash, err = sampleHash("transaction.hash", tx.Hash); err != nil {
			return nil, err
		}
		if e.From, err = sampleAddress("transaction.from", tx.From); err != nil {
			return nil, err
		}
		if tx.To != "" {
			to, err := sampleAddress("transaction.to", tx.To)
			if err != nil {
				return nil, err
			}
			e.To = &to
		}
		return e, nil
	}
	return nil, nil
}

// CheckExpressions runs a dry run. A malformed sample is an error; the
// expressions' own errors are in the result.
func (s *NotificationService) CheckExpressions(in ExpressionCheck) (*ExpressionCheckResult, error) {
	event, err := in.sampleEvent()
	if err != nil {
		return nil, err
	}
	setting := &NotificationSetting{Condition: in.Condition, Payload: in.Payload}
	if in.Event != "" {
		setting.Filter = &NotifyFilter{Event: in.Event}
		if err := validateFilter(setting.Filter); err != nil {
			return &ExpressionCheckResult{Error: err.Error()}, nil
		}
	}
	x, err := compileExpressions(setting)
	if err != nil {
		return &ExpressionCheckResult{Error: err.Error()}, nil
	}
	out := &ExpressionCheckResult{Valid: true}
	if event == nil {
		return out, nil
	}
	if le, ok := event.(*events.LogEvent); ok && setting.Filter != nil {
		if !matchesEvent(setting.Filter, le.Log.Topics) {
			out.Error = "the sample log is not the filter event (first topic or number of indexed topics differ)"
			return out, nil
		}
		if out.Decoded = decodeFilterEvent(setting.Filter, le.Log); out.Decoded == nil {
			out.Error = "the sample log's data does not decode as the filter event"
			return out, nil
		}
	}
	if x == nil {
		out.Notify = true // no expressions: every matching event is notified
		return out, nil
	}
	notify, result, err := x.evaluate(x.activation(s.chainID.Load(), event, out.Decoded))
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	out.Notify, out.Result = notify, result
	return out, nil
}

// IsSampleError reports whether err is a malformed sample given to
// CheckExpressions.
func IsSampleError(err error) bool { return errors.Is(err, errSampleInput) }
