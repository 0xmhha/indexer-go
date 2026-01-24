package events

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestFilter_Validate(t *testing.T) {
	tests := []struct {
		name    string
		filter  *Filter
		wantErr bool
	}{
		{
			name:    "empty filter",
			filter:  NewFilter(),
			wantErr: false,
		},
		{
			name: "valid min/max value",
			filter: &Filter{
				MinValue: big.NewInt(100),
				MaxValue: big.NewInt(1000),
			},
			wantErr: false,
		},
		{
			name: "invalid min > max value",
			filter: &Filter{
				MinValue: big.NewInt(1000),
				MaxValue: big.NewInt(100),
			},
			wantErr: true,
		},
		{
			name: "valid from/to block",
			filter: &Filter{
				FromBlock: 100,
				ToBlock:   1000,
			},
			wantErr: false,
		},
		{
			name: "invalid fromBlock > toBlock",
			filter: &Filter{
				FromBlock: 1000,
				ToBlock:   100,
			},
			wantErr: true,
		},
		{
			name: "negative minValue",
			filter: &Filter{
				MinValue: big.NewInt(-100),
			},
			wantErr: true,
		},
		{
			name: "negative maxValue",
			filter: &Filter{
				MaxValue: big.NewInt(-100),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFilter_MatchBlock(t *testing.T) {
	tests := []struct {
		name   string
		filter *Filter
		block  *BlockEvent
		want   bool
	}{
		{
			name:   "empty filter matches all",
			filter: NewFilter(),
			block:  NewBlockEvent(types.NewBlockWithHeader(&types.Header{Number: big.NewInt(100)})),
			want:   true,
		},
		{
			name: "fromBlock filter matches",
			filter: &Filter{
				FromBlock: 50,
			},
			block: NewBlockEvent(types.NewBlockWithHeader(&types.Header{Number: big.NewInt(100)})),
			want:  true,
		},
		{
			name: "fromBlock filter does not match",
			filter: &Filter{
				FromBlock: 200,
			},
			block: NewBlockEvent(types.NewBlockWithHeader(&types.Header{Number: big.NewInt(100)})),
			want:  false,
		},
		{
			name: "toBlock filter matches",
			filter: &Filter{
				ToBlock: 200,
			},
			block: NewBlockEvent(types.NewBlockWithHeader(&types.Header{Number: big.NewInt(100)})),
			want:  true,
		},
		{
			name: "toBlock filter does not match",
			filter: &Filter{
				ToBlock: 50,
			},
			block: NewBlockEvent(types.NewBlockWithHeader(&types.Header{Number: big.NewInt(100)})),
			want:  false,
		},
		{
			name: "block range filter matches",
			filter: &Filter{
				FromBlock: 50,
				ToBlock:   200,
			},
			block: NewBlockEvent(types.NewBlockWithHeader(&types.Header{Number: big.NewInt(100)})),
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.MatchBlock(tt.block); got != tt.want {
				t.Errorf("MatchBlock() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_MatchTransaction(t *testing.T) {
	addr1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	addr3 := common.HexToAddress("0x3333333333333333333333333333333333333333")

	tests := []struct {
		name   string
		filter *Filter
		tx     *TransactionEvent
		want   bool
	}{
		{
			name:   "empty filter matches all",
			filter: NewFilter(),
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: true,
		},
		{
			name: "from address filter matches",
			filter: &Filter{
				FromAddresses: []common.Address{addr1},
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: true,
		},
		{
			name: "from address filter does not match",
			filter: &Filter{
				FromAddresses: []common.Address{addr3},
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: false,
		},
		{
			name: "to address filter matches",
			filter: &Filter{
				ToAddresses: []common.Address{addr2},
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: true,
		},
		{
			name: "to address filter does not match",
			filter: &Filter{
				ToAddresses: []common.Address{addr3},
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: false,
		},
		{
			name: "addresses filter matches from",
			filter: &Filter{
				Addresses: []common.Address{addr1, addr3},
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: true,
		},
		{
			name: "addresses filter matches to",
			filter: &Filter{
				Addresses: []common.Address{addr2, addr3},
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: true,
		},
		{
			name: "addresses filter does not match",
			filter: &Filter{
				Addresses: []common.Address{addr3},
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: false,
		},
		{
			name: "minValue filter matches",
			filter: &Filter{
				MinValue: big.NewInt(50),
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: true,
		},
		{
			name: "minValue filter does not match",
			filter: &Filter{
				MinValue: big.NewInt(200),
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: false,
		},
		{
			name: "maxValue filter matches",
			filter: &Filter{
				MaxValue: big.NewInt(200),
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: true,
		},
		{
			name: "maxValue filter does not match",
			filter: &Filter{
				MaxValue: big.NewInt(50),
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: false,
		},
		{
			name: "value range filter matches",
			filter: &Filter{
				MinValue: big.NewInt(50),
				MaxValue: big.NewInt(200),
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: true,
		},
		{
			name: "contract creation with to filter does not match",
			filter: &Filter{
				ToAddresses: []common.Address{addr2},
			},
			tx: NewTransactionEvent(
				types.NewContractCreation(0, big.NewInt(100), 21000, big.NewInt(1), []byte{0x60}),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: false,
		},
		{
			name: "complex filter matches",
			filter: &Filter{
				FromAddresses: []common.Address{addr1},
				ToAddresses:   []common.Address{addr2},
				MinValue:      big.NewInt(50),
				MaxValue:      big.NewInt(200),
				FromBlock:     50,
				ToBlock:       200,
			},
			tx: NewTransactionEvent(
				types.NewTransaction(0, addr2, big.NewInt(100), 21000, big.NewInt(1), nil),
				100, common.Hash{}, 0, addr1, nil,
			),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.MatchTransaction(tt.tx); got != tt.want {
				t.Errorf("MatchTransaction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_MatchLog(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	altAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")
	topic0 := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	topic1 := common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	log := &types.Log{
		Address:     addr,
		Topics:      []common.Hash{topic0, topic1},
		BlockNumber: 100,
	}
	event := &LogEvent{Log: log}

	tests := []struct {
		name   string
		filter *Filter
		want   bool
	}{
		{
			name:   "empty filter matches",
			filter: NewFilter(),
			want:   true,
		},
		{
			name:   "address filter matches",
			filter: &Filter{Addresses: []common.Address{addr}},
			want:   true,
		},
		{
			name:   "address filter mismatch",
			filter: &Filter{Addresses: []common.Address{altAddr}},
			want:   false,
		},
		{
			name:   "topic filter matches",
			filter: &Filter{Topics: [][]common.Hash{{topic0}}},
			want:   true,
		},
		{
			name:   "topic filter mismatch",
			filter: &Filter{Topics: [][]common.Hash{{common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")}}},
			want:   false,
		},
		{
			name:   "block range filter",
			filter: &Filter{FromBlock: 50, ToBlock: 150},
			want:   true,
		},
		{
			name:   "block range filter mismatch",
			filter: &Filter{FromBlock: 150, ToBlock: 200},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.MatchLog(event); got != tt.want {
				t.Errorf("MatchLog() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_IsEmpty(t *testing.T) {
	tests := []struct {
		name   string
		filter *Filter
		want   bool
	}{
		{
			name:   "new filter is empty",
			filter: NewFilter(),
			want:   true,
		},
		{
			name: "filter with addresses is not empty",
			filter: &Filter{
				Addresses: []common.Address{common.HexToAddress("0x1")},
			},
			want: false,
		},
		{
			name: "filter with fromAddresses is not empty",
			filter: &Filter{
				FromAddresses: []common.Address{common.HexToAddress("0x1")},
			},
			want: false,
		},
		{
			name: "filter with minValue is not empty",
			filter: &Filter{
				MinValue: big.NewInt(100),
			},
			want: false,
		},
		{
			name: "filter with fromBlock is not empty",
			filter: &Filter{
				FromBlock: 100,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.IsEmpty(); got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_Clone(t *testing.T) {
	original := &Filter{
		Addresses:     []common.Address{common.HexToAddress("0x1")},
		FromAddresses: []common.Address{common.HexToAddress("0x2")},
		ToAddresses:   []common.Address{common.HexToAddress("0x3")},
		MinValue:      big.NewInt(100),
		MaxValue:      big.NewInt(1000),
		FromBlock:     10,
		ToBlock:       100,
	}

	clone := original.Clone()

	// Verify all fields are copied
	if len(clone.Addresses) != len(original.Addresses) {
		t.Error("Addresses not cloned correctly")
	}
	if len(clone.FromAddresses) != len(original.FromAddresses) {
		t.Error("FromAddresses not cloned correctly")
	}
	if len(clone.ToAddresses) != len(original.ToAddresses) {
		t.Error("ToAddresses not cloned correctly")
	}
	if clone.MinValue.Cmp(original.MinValue) != 0 {
		t.Error("MinValue not cloned correctly")
	}
	if clone.MaxValue.Cmp(original.MaxValue) != 0 {
		t.Error("MaxValue not cloned correctly")
	}
	if clone.FromBlock != original.FromBlock {
		t.Error("FromBlock not cloned correctly")
	}
	if clone.ToBlock != original.ToBlock {
		t.Error("ToBlock not cloned correctly")
	}

	// Verify it's a deep copy (modifying clone doesn't affect original)
	clone.Addresses[0] = common.HexToAddress("0xAAAA")
	if original.Addresses[0] == clone.Addresses[0] {
		t.Error("Clone is not a deep copy (addresses)")
	}

	clone.MinValue.SetInt64(999)
	if original.MinValue.Cmp(big.NewInt(100)) != 0 {
		t.Error("Clone is not a deep copy (minValue)")
	}
}
