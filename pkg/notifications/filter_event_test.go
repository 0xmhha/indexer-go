package notifications

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFilterInputParse: addresses, topics, participants and the event are
// kept as given, and malformed values are refused instead of dropped.
func TestFilterInputParse(t *testing.T) {
	topic := "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	addr := "0x00000000000000000000000000000000000000aa"
	f, err := FilterInput{
		Addresses: []string{addr}, Topics: [][]string{{topic}, {}}, Participants: []string{addr},
		Event: "Transfer(address indexed from, address indexed to, uint256 value)",
	}.Parse()
	require.NoError(t, err)
	assert.Equal(t, []common.Address{common.HexToAddress(addr)}, f.Addresses)
	assert.Equal(t, [][]common.Hash{{common.HexToHash(topic)}, {}}, f.Topics)
	assert.Equal(t, []common.Address{common.HexToAddress(addr)}, f.Participants)

	for name, in := range map[string]FilterInput{
		"address":          {Addresses: []string{"0x12"}},
		"participant":      {Participants: []string{"not-an-address"}},
		"short topic":      {Topics: [][]string{{"0x01"}}},
		"topic not hex":    {Topics: [][]string{{"zz"}}},
		"event syntax":     {Event: "Transfer(address"},
		"unnamed argument": {Event: "Transfer(address indexed, address indexed to, uint256 value)"},
	} {
		_, err := in.Parse()
		assert.Error(t, err, name)
	}
}
