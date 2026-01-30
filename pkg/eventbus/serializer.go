package eventbus

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// JSONSerializer implements EventSerializer using JSON encoding
type JSONSerializer struct{}

// NewJSONSerializer creates a new JSON serializer
func NewJSONSerializer() *JSONSerializer {
	return &JSONSerializer{}
}

// Ensure JSONSerializer implements EventSerializer
var _ EventSerializer = (*JSONSerializer)(nil)

// eventEnvelope wraps an event with type information for deserialization
type eventEnvelope struct {
	Type      events.EventType `json:"type"`
	Timestamp time.Time        `json:"timestamp"`
	NodeID    string           `json:"node_id,omitempty"`
	ChainID   string           `json:"chain_id,omitempty"`
	Data      json.RawMessage  `json:"data"`
}

// blockEventData is the JSON representation of BlockEvent
type blockEventData struct {
	Number    uint64      `json:"number"`
	Hash      common.Hash `json:"hash"`
	TxCount   int         `json:"tx_count"`
	CreatedAt time.Time   `json:"created_at"`
	// Note: Block data is not serialized for distributed messaging
	// Subscribers can fetch full block data using the hash if needed
}

// transactionEventData is the JSON representation of TransactionEvent
type transactionEventData struct {
	Hash        common.Hash     `json:"hash"`
	BlockNumber uint64          `json:"block_number"`
	BlockHash   common.Hash     `json:"block_hash"`
	Index       uint            `json:"index"`
	From        common.Address  `json:"from"`
	To          *common.Address `json:"to,omitempty"`
	Value       string          `json:"value"`
	CreatedAt   time.Time       `json:"created_at"`
	// Note: Tx and Receipt data are not serialized for distributed messaging
}

// logEventData is the JSON representation of LogEvent
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

// chainConfigEventData is the JSON representation of ChainConfigEvent
type chainConfigEventData struct {
	BlockNumber uint64      `json:"block_number"`
	BlockHash   common.Hash `json:"block_hash"`
	Parameter   string      `json:"parameter"`
	OldValue    string      `json:"old_value"`
	NewValue    string      `json:"new_value"`
	CreatedAt   time.Time   `json:"created_at"`
}

// validatorSetEventData is the JSON representation of ValidatorSetEvent
type validatorSetEventData struct {
	BlockNumber      uint64         `json:"block_number"`
	BlockHash        common.Hash    `json:"block_hash"`
	ChangeType       string         `json:"change_type"`
	Validator        common.Address `json:"validator"`
	ValidatorInfo    string         `json:"validator_info"`
	ValidatorSetSize int            `json:"validator_set_size"`
	CreatedAt        time.Time      `json:"created_at"`
}

// systemContractEventData is the JSON representation of SystemContractEvent
type systemContractEventData struct {
	Contract    common.Address                 `json:"contract"`
	EventName   events.SystemContractEventType `json:"event_name"`
	BlockNumber uint64                         `json:"block_number"`
	TxHash      common.Hash                    `json:"tx_hash"`
	LogIndex    uint                           `json:"log_index"`
	Data        map[string]interface{}         `json:"data"`
	CreatedAt   time.Time                      `json:"created_at"`
}

// Serialize converts an event to JSON bytes
func (s *JSONSerializer) Serialize(event events.Event) ([]byte, error) {
	if event == nil {
		return nil, ErrSerializationFailed
	}

	var data json.RawMessage
	var err error

	switch e := event.(type) {
	case *events.BlockEvent:
		data, err = json.Marshal(blockEventData{
			Number:    e.Number,
			Hash:      e.Hash,
			TxCount:   e.TxCount,
			CreatedAt: e.CreatedAt,
		})
	case *events.TransactionEvent:
		data, err = json.Marshal(transactionEventData{
			Hash:        e.Hash,
			BlockNumber: e.BlockNumber,
			BlockHash:   e.BlockHash,
			Index:       e.Index,
			From:        e.From,
			To:          e.To,
			Value:       e.Value,
			CreatedAt:   e.CreatedAt,
		})
	case *events.LogEvent:
		if e.Log != nil {
			data, err = json.Marshal(logEventData{
				Address:     e.Log.Address,
				Topics:      e.Log.Topics,
				Data:        e.Log.Data,
				BlockNumber: e.Log.BlockNumber,
				TxHash:      e.Log.TxHash,
				TxIndex:     e.Log.TxIndex,
				BlockHash:   e.Log.BlockHash,
				LogIndex:    e.Log.Index,
				Removed:     e.Log.Removed,
				CreatedAt:   e.CreatedAt,
			})
		} else {
			data, err = json.Marshal(logEventData{CreatedAt: e.CreatedAt})
		}
	case *events.ChainConfigEvent:
		data, err = json.Marshal(chainConfigEventData{
			BlockNumber: e.BlockNumber,
			BlockHash:   e.BlockHash,
			Parameter:   e.Parameter,
			OldValue:    e.OldValue,
			NewValue:    e.NewValue,
			CreatedAt:   e.CreatedAt,
		})
	case *events.ValidatorSetEvent:
		data, err = json.Marshal(validatorSetEventData{
			BlockNumber:      e.BlockNumber,
			BlockHash:        e.BlockHash,
			ChangeType:       e.ChangeType,
			Validator:        e.Validator,
			ValidatorInfo:    e.ValidatorInfo,
			ValidatorSetSize: e.ValidatorSetSize,
			CreatedAt:        e.CreatedAt,
		})
	case *events.SystemContractEvent:
		data, err = json.Marshal(systemContractEventData{
			Contract:    e.Contract,
			EventName:   e.EventName,
			BlockNumber: e.BlockNumber,
			TxHash:      e.TxHash,
			LogIndex:    e.LogIndex,
			Data:        e.Data,
			CreatedAt:   e.CreatedAt,
		})
	default:
		return nil, fmt.Errorf("%w: unknown event type %T", ErrInvalidEventType, event)
	}

	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSerializationFailed, err)
	}

	envelope := eventEnvelope{
		Type:      event.Type(),
		Timestamp: event.Timestamp(),
		Data:      data,
	}

	result, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSerializationFailed, err)
	}

	return result, nil
}

// Deserialize converts JSON bytes back to an event
func (s *JSONSerializer) Deserialize(data []byte) (events.Event, error) {
	if len(data) == 0 {
		return nil, ErrDeserializationFailed
	}

	var envelope eventEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDeserializationFailed, err)
	}

	switch envelope.Type {
	case events.EventTypeBlock:
		var ed blockEventData
		if err := json.Unmarshal(envelope.Data, &ed); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDeserializationFailed, err)
		}
		return &events.BlockEvent{
			Number:    ed.Number,
			Hash:      ed.Hash,
			TxCount:   ed.TxCount,
			CreatedAt: ed.CreatedAt,
			// Block is nil - can be fetched separately if needed
		}, nil

	case events.EventTypeTransaction:
		var ed transactionEventData
		if err := json.Unmarshal(envelope.Data, &ed); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDeserializationFailed, err)
		}
		return &events.TransactionEvent{
			Hash:        ed.Hash,
			BlockNumber: ed.BlockNumber,
			BlockHash:   ed.BlockHash,
			Index:       ed.Index,
			From:        ed.From,
			To:          ed.To,
			Value:       ed.Value,
			CreatedAt:   ed.CreatedAt,
			// Tx and Receipt are nil - can be fetched separately if needed
		}, nil

	case events.EventTypeLog:
		var ed logEventData
		if err := json.Unmarshal(envelope.Data, &ed); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDeserializationFailed, err)
		}
		return &events.LogEvent{
			Log: &types.Log{
				Address:     ed.Address,
				Topics:      ed.Topics,
				Data:        ed.Data,
				BlockNumber: ed.BlockNumber,
				TxHash:      ed.TxHash,
				TxIndex:     ed.TxIndex,
				BlockHash:   ed.BlockHash,
				Index:       ed.LogIndex,
				Removed:     ed.Removed,
			},
			CreatedAt: ed.CreatedAt,
		}, nil

	case events.EventTypeChainConfig:
		var ed chainConfigEventData
		if err := json.Unmarshal(envelope.Data, &ed); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDeserializationFailed, err)
		}
		return &events.ChainConfigEvent{
			BlockNumber: ed.BlockNumber,
			BlockHash:   ed.BlockHash,
			Parameter:   ed.Parameter,
			OldValue:    ed.OldValue,
			NewValue:    ed.NewValue,
			CreatedAt:   ed.CreatedAt,
		}, nil

	case events.EventTypeValidatorSet:
		var ed validatorSetEventData
		if err := json.Unmarshal(envelope.Data, &ed); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDeserializationFailed, err)
		}
		return &events.ValidatorSetEvent{
			BlockNumber:      ed.BlockNumber,
			BlockHash:        ed.BlockHash,
			ChangeType:       ed.ChangeType,
			Validator:        ed.Validator,
			ValidatorInfo:    ed.ValidatorInfo,
			ValidatorSetSize: ed.ValidatorSetSize,
			CreatedAt:        ed.CreatedAt,
		}, nil

	case events.EventTypeSystemContract:
		var ed systemContractEventData
		if err := json.Unmarshal(envelope.Data, &ed); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDeserializationFailed, err)
		}
		return &events.SystemContractEvent{
			Contract:    ed.Contract,
			EventName:   ed.EventName,
			BlockNumber: ed.BlockNumber,
			TxHash:      ed.TxHash,
			LogIndex:    ed.LogIndex,
			Data:        ed.Data,
			CreatedAt:   ed.CreatedAt,
		}, nil

	default:
		return nil, fmt.Errorf("%w: unknown event type %s", ErrInvalidEventType, envelope.Type)
	}
}

// ContentType returns the MIME type for JSON
func (s *JSONSerializer) ContentType() string {
	return "application/json"
}

