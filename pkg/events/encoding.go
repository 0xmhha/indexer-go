package events

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Events are encoded as JSON values, one per event type, for the outbox
// (refactoring plan R3-1) and for event buses that carry events between
// processes. The built-in types are encoded here; packages that define
// their own event types register an EventCodec.
//
// The encoding keeps what subscribers read. A block event keeps the block
// header, not the transactions (decoded, Block holds the header only). A
// transaction event of a block drops Tx and Receipt: subscribers of
// committed transactions use the copied fields, and the transaction and
// receipt are stored with the block.

// blockEventData is the JSON form of BlockEvent.
type blockEventData struct {
	Number    uint64        `json:"number"`
	Hash      common.Hash   `json:"hash"`
	TxCount   int           `json:"tx_count"`
	CreatedAt time.Time     `json:"created_at"`
	Header    *types.Header `json:"header,omitempty"`
}

// transactionEventData is the JSON form of TransactionEvent.
type transactionEventData struct {
	Hash        common.Hash     `json:"hash"`
	BlockNumber uint64          `json:"block_number"`
	BlockHash   common.Hash     `json:"block_hash"`
	Index       uint            `json:"index"`
	From        common.Address  `json:"from"`
	To          *common.Address `json:"to,omitempty"`
	Value       string          `json:"value"`
	CreatedAt   time.Time       `json:"created_at"`
}

// logEventData is the JSON form of LogEvent.
type logEventData struct {
	Address     common.Address `json:"address"`
	Topics      []common.Hash  `json:"topics"`
	Data        []byte         `json:"data"`
	BlockNumber uint64         `json:"block_number"`
	TxHash      common.Hash    `json:"tx_hash"`
	TxIndex     uint           `json:"tx_index"`
	BlockHash   common.Hash    `json:"block_hash"`
	LogIndex    uint           `json:"log_index"`
	Removed     bool           `json:"removed"`
	CreatedAt   time.Time      `json:"created_at"`
}

// chainConfigEventData is the JSON form of ChainConfigEvent.
type chainConfigEventData struct {
	BlockNumber uint64      `json:"block_number"`
	BlockHash   common.Hash `json:"block_hash"`
	Parameter   string      `json:"parameter"`
	OldValue    string      `json:"old_value"`
	NewValue    string      `json:"new_value"`
	CreatedAt   time.Time   `json:"created_at"`
}

// validatorSetEventData is the JSON form of ValidatorSetEvent.
type validatorSetEventData struct {
	BlockNumber      uint64         `json:"block_number"`
	BlockHash        common.Hash    `json:"block_hash"`
	ChangeType       string         `json:"change_type"`
	Validator        common.Address `json:"validator"`
	ValidatorInfo    string         `json:"validator_info"`
	ValidatorSetSize int            `json:"validator_set_size"`
	CreatedAt        time.Time      `json:"created_at"`
}

// MarshalEvent encodes an event's value. The event's type is not part of
// the value; UnmarshalEvent needs it.
func MarshalEvent(event Event) ([]byte, error) {
	switch e := event.(type) {
	case nil:
		return nil, fmt.Errorf("events: marshal nil event")
	case *BlockEvent:
		d := blockEventData{Number: e.Number, Hash: e.Hash, TxCount: e.TxCount, CreatedAt: e.CreatedAt}
		if e.Block != nil {
			d.Header = e.Block.Header()
		}
		return json.Marshal(d)
	case *TransactionEvent:
		return json.Marshal(transactionEventData{
			Hash: e.Hash, BlockNumber: e.BlockNumber, BlockHash: e.BlockHash, Index: e.Index,
			From: e.From, To: e.To, Value: e.Value, CreatedAt: e.CreatedAt,
		})
	case *LogEvent:
		if e.Log == nil {
			return json.Marshal(logEventData{CreatedAt: e.CreatedAt})
		}
		return json.Marshal(logEventData{
			Address: e.Log.Address, Topics: e.Log.Topics, Data: e.Log.Data,
			BlockNumber: e.Log.BlockNumber, TxHash: e.Log.TxHash, TxIndex: e.Log.TxIndex,
			BlockHash: e.Log.BlockHash, LogIndex: e.Log.Index, Removed: e.Log.Removed,
			CreatedAt: e.CreatedAt,
		})
	case *ChainConfigEvent:
		return json.Marshal(chainConfigEventData{
			BlockNumber: e.BlockNumber, BlockHash: e.BlockHash, Parameter: e.Parameter,
			OldValue: e.OldValue, NewValue: e.NewValue, CreatedAt: e.CreatedAt,
		})
	case *ValidatorSetEvent:
		return json.Marshal(validatorSetEventData{
			BlockNumber: e.BlockNumber, BlockHash: e.BlockHash, ChangeType: e.ChangeType,
			Validator: e.Validator, ValidatorInfo: e.ValidatorInfo, ValidatorSetSize: e.ValidatorSetSize,
			CreatedAt: e.CreatedAt,
		})
	case *ReorgEvent:
		return json.Marshal(e) // plain fields only
	}
	codec, ok := CodecOf(event.Type())
	if !ok {
		return nil, fmt.Errorf("events: no codec for event type %q (%T)", event.Type(), event)
	}
	v, err := codec.Encode(event)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// UnmarshalEvent decodes an event of type t encoded by MarshalEvent.
func UnmarshalEvent(t EventType, data []byte) (Event, error) {
	switch t {
	case EventTypeBlock:
		var d blockEventData
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		e := &BlockEvent{Number: d.Number, Hash: d.Hash, TxCount: d.TxCount, CreatedAt: d.CreatedAt}
		if d.Header != nil {
			e.Block = types.NewBlockWithHeader(d.Header)
		}
		return e, nil
	case EventTypeTransaction:
		var d transactionEventData
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		return &TransactionEvent{
			Hash: d.Hash, BlockNumber: d.BlockNumber, BlockHash: d.BlockHash, Index: d.Index,
			From: d.From, To: d.To, Value: d.Value, CreatedAt: d.CreatedAt,
		}, nil
	case EventTypeLog:
		var d logEventData
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		return &LogEvent{
			Log: &types.Log{
				Address: d.Address, Topics: d.Topics, Data: d.Data,
				BlockNumber: d.BlockNumber, TxHash: d.TxHash, TxIndex: d.TxIndex,
				BlockHash: d.BlockHash, Index: d.LogIndex, Removed: d.Removed,
			},
			CreatedAt: d.CreatedAt,
		}, nil
	case EventTypeChainConfig:
		var d chainConfigEventData
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		return &ChainConfigEvent{
			BlockNumber: d.BlockNumber, BlockHash: d.BlockHash, Parameter: d.Parameter,
			OldValue: d.OldValue, NewValue: d.NewValue, CreatedAt: d.CreatedAt,
		}, nil
	case EventTypeValidatorSet:
		var d validatorSetEventData
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		return &ValidatorSetEvent{
			BlockNumber: d.BlockNumber, BlockHash: d.BlockHash, ChangeType: d.ChangeType,
			Validator: d.Validator, ValidatorInfo: d.ValidatorInfo, ValidatorSetSize: d.ValidatorSetSize,
			CreatedAt: d.CreatedAt,
		}, nil
	case EventTypeReorg:
		var e ReorgEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		return &e, nil
	}
	codec, ok := CodecOf(t)
	if !ok {
		return nil, fmt.Errorf("events: no codec for event type %q", t)
	}
	return codec.Decode(data)
}

// StructCodec is the codec of an event type whose exported fields are its
// JSON form. Numbers in interface{} values decode as json.Number, so they
// encode again to the same digits.
func StructCodec[T any, PT interface {
	*T
	Event
}]() EventCodec {
	return EventCodec{
		Encode: func(ev Event) (interface{}, error) {
			e, ok := ev.(PT)
			if !ok {
				return nil, fmt.Errorf("unexpected event %T", ev)
			}
			return e, nil
		},
		Decode: func(data []byte) (Event, error) {
			var v T
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.UseNumber()
			if err := dec.Decode(&v); err != nil {
				return nil, err
			}
			return PT(&v), nil
		},
	}
}
