package declared

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
)

const paymentSettled = "PaymentSettled(address indexed merchant, bytes32 indexed orderId, address device, uint256 amount)"

var (
	settlement = common.HexToAddress("0x00000000000000000000000000000000000057e1")
	merchant   = common.HexToAddress("0x000000000000000000000000000000000000AbCd")
	device     = common.HexToAddress("0x0000000000000000000000000000000000000De5")
)

func spec() Spec {
	return Spec{
		Sources: []Source{{Name: "settlement", Address: settlement.Hex(), StartBlock: 7,
			Events: []string{paymentSettled, "event Refunded(bytes32 indexed orderId, string reason)"}}},
		Tables: []Table{{Name: "receipts", Source: "settlement", Event: "PaymentSettled", Keys: [][]string{{"merchant", "orderId"}}}},
	}
}

func TestParseEvent(t *testing.T) {
	ev, err := ParseEvent("Transfer(address indexed from, address indexed to, uint256 value)")
	require.NoError(t, err)
	assert.Equal(t, common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"), ev.ID, "the ERC-20 Transfer topic")
	ev, err = ParseEvent(paymentSettled)
	require.NoError(t, err)
	assert.Equal(t, crypto.Keccak256Hash([]byte("PaymentSettled(address,bytes32,address,uint256)")), ev.ID)
	assert.True(t, ev.Inputs[0].Indexed)
	assert.False(t, ev.Inputs[2].Indexed)
	_, err = ParseEvent("event Ping()")
	require.NoError(t, err)

	for _, bad := range []string{"", "Transfer", "Transfer(address)", "Transfer(adress from)", "Transfer(address indexed)", "Transfer(address 1x)"} {
		_, err := ParseEvent(bad)
		assert.Error(t, err, bad)
	}
}

// receiptLog builds a PaymentSettled log the way the contract emits it.
func receiptLog(t *testing.T, orderID common.Hash, amount int64) *model.Log {
	t.Helper()
	ev, err := ParseEvent(paymentSettled)
	require.NoError(t, err)
	data, err := ev.Inputs.NonIndexed().Pack(device, big.NewInt(amount))
	require.NoError(t, err)
	return &model.Log{Address: settlement, Topics: []common.Hash{ev.ID, common.BytesToHash(merchant.Bytes()), orderID}, Data: data}
}

func TestCompileMatchDecode(t *testing.T) {
	p, err := Compile(spec())
	require.NoError(t, err)
	assert.Equal(t, []common.Address{settlement}, p.Addresses())
	require.Len(t, p.Topics(), 1, "only events with tables are wanted")
	assert.Equal(t, uint64(7), p.StartBlock())

	order := common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000AB")
	l := receiptLog(t, order, 2500)
	tables := p.Match(l)
	require.Len(t, tables, 1)
	fields, err := tables[0].Decode(l)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"merchant": "0x000000000000000000000000000000000000abcd",
		"orderId":  "0x00000000000000000000000000000000000000000000000000000000000000ab",
		"device":   "0x0000000000000000000000000000000000000de5",
		"amount":   "2500",
	}, fields)
	assert.Equal(t, []string{"merchant", "orderId", "device", "amount"}, tables[0].Fields())

	other := *l
	other.Address = device
	assert.Empty(t, p.Match(&other), "another contract")
	refund := *l
	refund.Topics = []common.Hash{crypto.Keccak256Hash([]byte("Refunded(bytes32,string)")), order}
	assert.Empty(t, p.Match(&refund), "an event without a table")
	short := *l
	short.Topics = l.Topics[:2]
	_, err = tables[0].Decode(&short)
	assert.Error(t, err, "a log with too few topics")

	key, ok := tables[0].Key([]string{"orderId", "merchant"})
	require.True(t, ok)
	assert.Equal(t, []string{"merchant", "orderId"}, key, "declared order")
	_, ok = tables[0].Key([]string{"merchant"})
	assert.False(t, ok)
}

func TestDecodeDynamicIndexed(t *testing.T) {
	s := spec()
	s.Tables = append(s.Tables, Table{Name: "refunds", Source: "settlement", Event: "Refunded"})
	p, err := Compile(s)
	require.NoError(t, err)
	refunds, ok := p.Table("refunds")
	require.True(t, ok)
	ev := refunds.Event
	data, err := ev.Inputs.NonIndexed().Pack("late")
	require.NoError(t, err)
	order := common.HexToHash("0x01")
	fields, err := refunds.Decode(&model.Log{Address: settlement, Topics: []common.Hash{ev.ID, order}, Data: data})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"orderId": order.Hex(), "reason": "late"}, fields)
}

func TestCompileRejects(t *testing.T) {
	for name, change := range map[string]func(*Spec){
		"no tables":        func(s *Spec) { s.Tables = nil },
		"bad table name":   func(s *Spec) { s.Tables[0].Name = "Receipts" },
		"duplicate table":  func(s *Spec) { s.Tables = append(s.Tables, s.Tables[0]) },
		"unknown source":   func(s *Spec) { s.Tables[0].Source = "nope" },
		"unknown event":    func(s *Spec) { s.Tables[0].Event = "Settled" },
		"key not an arg":   func(s *Spec) { s.Tables[0].Keys = [][]string{{"merchant", "order"}} },
		"empty key":        func(s *Spec) { s.Tables[0].Keys = [][]string{{}} },
		"key field twice":  func(s *Spec) { s.Tables[0].Keys = [][]string{{"merchant", "merchant"}} },
		"key twice":        func(s *Spec) { s.Tables[0].Keys = [][]string{{"merchant", "orderId"}, {"orderId", "merchant"}} },
		"no address":       func(s *Spec) { s.Sources[0].Address = "" },
		"bad address":      func(s *Spec) { s.Sources[0].Address = "0x12" },
		"no events":        func(s *Spec) { s.Sources[0].Events = nil },
		"bad signature":    func(s *Spec) { s.Sources[0].Events = []string{"PaymentSettled(address)"} },
		"duplicate source": func(s *Spec) { s.Sources = append(s.Sources, s.Sources[0]) },
		"duplicate event":  func(s *Spec) { s.Sources[0].Events = append(s.Sources[0].Events, paymentSettled) },
		"missing ABI file": func(s *Spec) { s.Sources[0].ABI = "/no/such/abi.json" },
		"unnamed event args": func(s *Spec) {
			s.Sources[0].ABI = writeABI(t, `[{"type":"event","name":"PaymentSettled","inputs":[{"type":"address","indexed":true}]}]`)
		},
	} {
		s := spec()
		change(&s)
		_, err := Compile(s)
		assert.Error(t, err, name)
	}
}

func writeABI(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "abi.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// TestCompileFromABI: an ABI file and event names declare the same event as
// its signature.
func TestCompileFromABI(t *testing.T) {
	s := spec()
	s.Sources[0].ABI = writeABI(t, `[{"type":"event","name":"PaymentSettled","anonymous":false,"inputs":[
		{"name":"merchant","type":"address","indexed":true},{"name":"orderId","type":"bytes32","indexed":true},
		{"name":"device","type":"address","indexed":false},{"name":"amount","type":"uint256","indexed":false}]}]`)
	s.Sources[0].Events = []string{"PaymentSettled"}
	p, err := Compile(s)
	require.NoError(t, err)
	l := receiptLog(t, common.HexToHash("0x02"), 1)
	require.Len(t, p.Match(l), 1)
	fields, err := p.Tables[0].Decode(l)
	require.NoError(t, err)
	assert.Equal(t, "1", fields["amount"])
}

func TestFormatInput(t *testing.T) {
	ev, err := ParseEvent("E(address a, uint256 n, bytes32 b, bool f, string s, string indexed h)")
	require.NoError(t, err)
	arg := func(i int) abi.Argument { return ev.Inputs[i] }
	for _, c := range []struct {
		arg       int
		in, want  string
		wantError bool
	}{
		{0, "0x000000000000000000000000000000000000ABCD", "0x000000000000000000000000000000000000abcd", false},
		{0, "abcd", "", true},
		{1, "0x10", "16", false},
		{1, "16", "16", false},
		{1, "x", "", true},
		{2, "0x" + "AB" + "00000000000000000000000000000000000000000000000000000000000000"[:62], "0xab" + "00000000000000000000000000000000000000000000000000000000000000"[:62], false},
		{2, "0xab", "", true},
		{3, "true", "true", false},
		{3, "yes", "", true},
		{4, "any text", "any text", false},
		{5, "0x" + "11" + "00000000000000000000000000000000000000000000000000000000000000"[:62], common.HexToHash("0x11" + "00000000000000000000000000000000000000000000000000000000000000"[:62]).Hex(), false},
		{5, "plain", "", true},
	} {
		got, err := FormatInput(arg(c.arg), c.in)
		if c.wantError {
			assert.Error(t, err, c.in)
			continue
		}
		require.NoError(t, err, c.in)
		assert.Equal(t, c.want, got, c.in)
	}
}
