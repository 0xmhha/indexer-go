package eventbus

import (
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONSerializer_ContentType(t *testing.T) {
	s := NewJSONSerializer()
	assert.Equal(t, "application/json", s.ContentType())
}

func TestJSONSerializer_BlockEvent(t *testing.T) {
	s := NewJSONSerializer()

	original := &events.BlockEvent{
		Number:    12345,
		Hash:      common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
		TxCount:   10,
		CreatedAt: time.Now().Truncate(time.Millisecond),
	}

	// Serialize
	data, err := s.Serialize(original)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Deserialize
	event, err := s.Deserialize(data)
	require.NoError(t, err)
	require.NotNil(t, event)

	// Verify
	be, ok := event.(*events.BlockEvent)
	require.True(t, ok)
	assert.Equal(t, original.Number, be.Number)
	assert.Equal(t, original.Hash, be.Hash)
	assert.Equal(t, original.TxCount, be.TxCount)
	assert.Equal(t, original.CreatedAt.UTC(), be.CreatedAt.UTC())
	assert.Nil(t, be.Block) // Block is not serialized
}

func TestJSONSerializer_TransactionEvent(t *testing.T) {
	s := NewJSONSerializer()

	toAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	original := &events.TransactionEvent{
		Hash:        common.HexToHash("0xdeadbeef"),
		BlockNumber: 100,
		BlockHash:   common.HexToHash("0xcafebabe"),
		Index:       5,
		From:        common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		To:          &toAddr,
		Value:       "1000000000000000000",
		CreatedAt:   time.Now().Truncate(time.Millisecond),
	}

	// Serialize
	data, err := s.Serialize(original)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Deserialize
	event, err := s.Deserialize(data)
	require.NoError(t, err)
	require.NotNil(t, event)

	// Verify
	te, ok := event.(*events.TransactionEvent)
	require.True(t, ok)
	assert.Equal(t, original.Hash, te.Hash)
	assert.Equal(t, original.BlockNumber, te.BlockNumber)
	assert.Equal(t, original.BlockHash, te.BlockHash)
	assert.Equal(t, original.Index, te.Index)
	assert.Equal(t, original.From, te.From)
	require.NotNil(t, te.To)
	assert.Equal(t, *original.To, *te.To)
	assert.Equal(t, original.Value, te.Value)
	assert.Nil(t, te.Tx) // Tx is not serialized
}

func TestJSONSerializer_TransactionEvent_NilTo(t *testing.T) {
	s := NewJSONSerializer()

	original := &events.TransactionEvent{
		Hash:        common.HexToHash("0xdeadbeef"),
		BlockNumber: 100,
		From:        common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		To:          nil, // Contract creation
		Value:       "0",
		CreatedAt:   time.Now().Truncate(time.Millisecond),
	}

	data, err := s.Serialize(original)
	require.NoError(t, err)

	event, err := s.Deserialize(data)
	require.NoError(t, err)

	te, ok := event.(*events.TransactionEvent)
	require.True(t, ok)
	assert.Nil(t, te.To)
}

func TestJSONSerializer_LogEvent(t *testing.T) {
	s := NewJSONSerializer()

	original := &events.LogEvent{
		Log: &types.Log{
			Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Topics: []common.Hash{
				common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"),
				common.HexToHash("0x000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			},
			Data:        []byte{0x01, 0x02, 0x03},
			BlockNumber: 200,
			TxHash:      common.HexToHash("0xabcd"),
			TxIndex:     3,
			BlockHash:   common.HexToHash("0xefgh"),
			Index:       7,
			Removed:     false,
		},
		CreatedAt: time.Now().Truncate(time.Millisecond),
	}

	// Serialize
	data, err := s.Serialize(original)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Deserialize
	event, err := s.Deserialize(data)
	require.NoError(t, err)
	require.NotNil(t, event)

	// Verify
	le, ok := event.(*events.LogEvent)
	require.True(t, ok)
	require.NotNil(t, le.Log)
	assert.Equal(t, original.Log.Address, le.Log.Address)
	assert.Equal(t, original.Log.Topics, le.Log.Topics)
	assert.Equal(t, original.Log.Data, le.Log.Data)
	assert.Equal(t, original.Log.BlockNumber, le.Log.BlockNumber)
	assert.Equal(t, original.Log.TxHash, le.Log.TxHash)
	assert.Equal(t, original.Log.Index, le.Log.Index)
	assert.Equal(t, original.Log.Removed, le.Log.Removed)
}

func TestJSONSerializer_ChainConfigEvent(t *testing.T) {
	s := NewJSONSerializer()

	original := &events.ChainConfigEvent{
		BlockNumber: 300,
		BlockHash:   common.HexToHash("0x1234"),
		Parameter:   "gasLimit",
		OldValue:    "8000000",
		NewValue:    "12000000",
		CreatedAt:   time.Now().Truncate(time.Millisecond),
	}

	data, err := s.Serialize(original)
	require.NoError(t, err)

	event, err := s.Deserialize(data)
	require.NoError(t, err)

	ce, ok := event.(*events.ChainConfigEvent)
	require.True(t, ok)
	assert.Equal(t, original.BlockNumber, ce.BlockNumber)
	assert.Equal(t, original.Parameter, ce.Parameter)
	assert.Equal(t, original.OldValue, ce.OldValue)
	assert.Equal(t, original.NewValue, ce.NewValue)
}

func TestJSONSerializer_ValidatorSetEvent(t *testing.T) {
	s := NewJSONSerializer()

	original := &events.ValidatorSetEvent{
		BlockNumber:      400,
		BlockHash:        common.HexToHash("0x5678"),
		ChangeType:       "added",
		Validator:        common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		ValidatorInfo:    `{"power": 100}`,
		ValidatorSetSize: 21,
		CreatedAt:        time.Now().Truncate(time.Millisecond),
	}

	data, err := s.Serialize(original)
	require.NoError(t, err)

	event, err := s.Deserialize(data)
	require.NoError(t, err)

	ve, ok := event.(*events.ValidatorSetEvent)
	require.True(t, ok)
	assert.Equal(t, original.BlockNumber, ve.BlockNumber)
	assert.Equal(t, original.ChangeType, ve.ChangeType)
	assert.Equal(t, original.Validator, ve.Validator)
	assert.Equal(t, original.ValidatorSetSize, ve.ValidatorSetSize)
}

func TestJSONSerializer_SystemContractEvent(t *testing.T) {
	s := NewJSONSerializer()

	original := &events.SystemContractEvent{
		Contract:    common.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc"),
		EventName:   events.SystemContractEventProposalCreated,
		BlockNumber: 500,
		TxHash:      common.HexToHash("0x9999"),
		LogIndex:    2,
		Data: map[string]interface{}{
			"proposalId": "123",
			"proposer":   "0xaaaa",
		},
		CreatedAt: time.Now().Truncate(time.Millisecond),
	}

	data, err := s.Serialize(original)
	require.NoError(t, err)

	event, err := s.Deserialize(data)
	require.NoError(t, err)

	se, ok := event.(*events.SystemContractEvent)
	require.True(t, ok)
	assert.Equal(t, original.Contract, se.Contract)
	assert.Equal(t, original.EventName, se.EventName)
	assert.Equal(t, original.BlockNumber, se.BlockNumber)
	assert.Equal(t, original.LogIndex, se.LogIndex)
	assert.Equal(t, "123", se.Data["proposalId"])
}

func TestJSONSerializer_ErrorCases(t *testing.T) {
	s := NewJSONSerializer()

	// Nil event
	_, err := s.Serialize(nil)
	assert.ErrorIs(t, err, ErrSerializationFailed)

	// Empty data
	_, err = s.Deserialize(nil)
	assert.ErrorIs(t, err, ErrDeserializationFailed)

	_, err = s.Deserialize([]byte{})
	assert.ErrorIs(t, err, ErrDeserializationFailed)

	// Invalid JSON
	_, err = s.Deserialize([]byte("not json"))
	assert.ErrorIs(t, err, ErrDeserializationFailed)

	// Unknown event type
	_, err = s.Deserialize([]byte(`{"type":"unknown","data":{}}`))
	assert.ErrorIs(t, err, ErrInvalidEventType)
}

func TestJSONSerializer_RoundTrip_AllEventTypes(t *testing.T) {
	s := NewJSONSerializer()

	testEvents := []events.Event{
		&events.BlockEvent{
			Number:    1,
			Hash:      common.HexToHash("0x1"),
			CreatedAt: time.Now(),
		},
		&events.TransactionEvent{
			Hash:        common.HexToHash("0x2"),
			BlockNumber: 2,
			From:        common.HexToAddress("0xa"),
			Value:       "100",
			CreatedAt:   time.Now(),
		},
		&events.LogEvent{
			Log: &types.Log{
				Address:     common.HexToAddress("0xb"),
				BlockNumber: 3,
			},
			CreatedAt: time.Now(),
		},
		&events.ChainConfigEvent{
			BlockNumber: 4,
			Parameter:   "test",
			CreatedAt:   time.Now(),
		},
		&events.ValidatorSetEvent{
			BlockNumber: 5,
			ChangeType:  "added",
			CreatedAt:   time.Now(),
		},
		&events.SystemContractEvent{
			BlockNumber: 6,
			EventName:   events.SystemContractEventMemberAdded,
			CreatedAt:   time.Now(),
		},
	}

	for _, original := range testEvents {
		t.Run(string(original.Type()), func(t *testing.T) {
			data, err := s.Serialize(original)
			require.NoError(t, err)

			restored, err := s.Deserialize(data)
			require.NoError(t, err)

			assert.Equal(t, original.Type(), restored.Type())
		})
	}
}
