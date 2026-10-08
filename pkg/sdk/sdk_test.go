package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/declared"
	"github.com/0xmhha/indexer-go/pkg/features/records"
)

type recordStore struct {
	asked []port.RecordKey
}

func (s *recordStore) ListRecords(context.Context, string, port.Page) ([]*port.Record, string, error) {
	return nil, "", nil
}

func (s *recordStore) ListRecordsByKey(_ context.Context, table string, key port.RecordKey, _ port.Page) ([]*port.Record, string, error) {
	s.asked = append(s.asked, key)
	return []*port.Record{{Table: table}}, "", nil
}

// TestLookupRecords: a lookup by field values finds the declared key, in
// its order, with the values normalized; tables and keys that are not
// declared are errors.
func TestLookupRecords(t *testing.T) {
	store := &recordStore{}
	_, _, err := LookupRecords(context.Background(), store, "receipts", nil, port.FirstPage(1))
	assert.ErrorContains(t, err, "no declared tables")

	plan, err := declared.Compile(declared.Spec{
		Sources: []declared.Source{{Name: "s", Address: "0x00000000000000000000000000000000000000aa",
			Events: []string{"Paid(address indexed merchant, uint256 indexed orderId, uint256 amount)"}}},
		Tables: []declared.Table{{Name: "receipts", Source: "s", Event: "Paid", Keys: [][]string{{"merchant", "orderId"}}}},
	})
	require.NoError(t, err)
	records.Attach(store, plan)
	defer records.Detach(store)

	got, _, err := LookupRecords(context.Background(), store, "receipts",
		map[string]string{"orderId": "0x10", "merchant": "0x00000000000000000000000000000000000000AB"}, port.FirstPage(1))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, []port.RecordKey{{ID: "merchant,orderId", Values: []string{"0x00000000000000000000000000000000000000ab", "16"}}}, store.asked)

	_, _, err = LookupRecords(context.Background(), store, "refunds", map[string]string{"merchant": "0x01"}, port.FirstPage(1))
	assert.ErrorContains(t, err, "no table refunds")
	_, _, err = LookupRecords(context.Background(), store, "receipts", map[string]string{"merchant": "0x01"}, port.FirstPage(1))
	assert.ErrorContains(t, err, "declares no key")
	_, _, err = LookupRecords(context.Background(), store, "receipts", map[string]string{"merchant": "x", "orderId": "1"}, port.FirstPage(1))
	assert.ErrorContains(t, err, "not an address")

	_, err = KVOf(store)
	assert.Error(t, err, "a store without a key-value store")
}
