package porttest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// testRecords checks RecordReader and RecordWriter: a table's records are
// listed oldest first; a key finds the records with its values, oldest
// first; writing a record again replaces it without listing it twice;
// tables and keys are separate.
func testRecords(t *testing.T, newStore NewStore) {
	ctx := context.Background()
	record := func(table string, block uint64, logIndex uint, merchant, order, amount string) *port.Record {
		return &port.Record{Table: table, BlockNumber: block, BlockTime: 1_700_000_000 + block, TxHash: fixtureHash("rec", block<<8|uint64(logIndex)),
			LogIndex: logIndex, Address: dexManager, Fields: map[string]string{"merchant": merchant, "orderId": order, "amount": amount}}
	}
	keys := func(r *port.Record) []port.RecordKey {
		return []port.RecordKey{
			{ID: "merchant,orderId", Values: []string{r.Fields["merchant"], r.Fields["orderId"]}},
			{ID: "merchant", Values: []string{r.Fields["merchant"]}},
		}
	}

	t.Run("Empty", func(t *testing.T) {
		s := open[recordStore](t, newStore)
		got, next, err := s.ListRecords(ctx, "receipts", port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, got)
		assert.Empty(t, next)
		got, _, err = s.ListRecordsByKey(ctx, "receipts", port.RecordKey{ID: "merchant", Values: []string{"0xa"}}, port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("ListAndLookup", func(t *testing.T) {
		s := open[recordStore](t, newStore)
		// Saved out of order; another table with the same position.
		saved := []*port.Record{
			record("receipts", 9, 1, "0xa", "0x01", "30"),
			record("receipts", 5, 2, "0xa", "0x01", "10"), // the same order twice: a duplicate
			record("receipts", 5, 0, "0xb", "0x01", "20"),
			record("receipts", 7, 3, "0xa", "0x02", "40"),
			record("refunds", 5, 2, "0xa", "0x01", "1"),
		}
		for _, r := range saved {
			require.NoError(t, s.SaveRecord(ctx, r, keys(r)))
		}
		// Saving a record again replaces it.
		again := record("receipts", 7, 3, "0xa", "0x02", "41")
		require.NoError(t, s.SaveRecord(ctx, again, keys(again)))

		want := []*port.Record{saved[2], saved[1], again, saved[0]}
		all, _, err := s.ListRecords(ctx, "receipts", port.FirstPage(10))
		require.NoError(t, err)
		require.Len(t, all, len(want))
		for i := range want {
			sameJSON(t, want[i], all[i], "record %d", i)
		}
		pos := func(r *port.Record) [2]uint64 { return [2]uint64{r.BlockNumber, uint64(r.LogIndex)} }
		checkPaging(t, want, pos, func(page port.Page) ([]*port.Record, string, error) { return s.ListRecords(ctx, "receipts", page) })

		order1 := port.RecordKey{ID: "merchant,orderId", Values: []string{"0xa", "0x01"}}
		got, _, err := s.ListRecordsByKey(ctx, "receipts", order1, port.FirstPage(10))
		require.NoError(t, err)
		require.Len(t, got, 2, "both logs of the order, earliest first")
		sameJSON(t, saved[1], got[0])
		sameJSON(t, saved[0], got[1])

		byMerchant := port.RecordKey{ID: "merchant", Values: []string{"0xa"}}
		wantA := []*port.Record{saved[1], again, saved[0]}
		checkPaging(t, wantA, pos, func(page port.Page) ([]*port.Record, string, error) {
			return s.ListRecordsByKey(ctx, "receipts", byMerchant, page)
		})

		for name, k := range map[string]port.RecordKey{
			"other values":        {ID: "merchant,orderId", Values: []string{"0xb", "0x02"}},
			"values of other key": {ID: "merchant", Values: []string{"0xa", "0x01"}},
			"undeclared key":      {ID: "orderId", Values: []string{"0x01"}},
		} {
			got, _, err := s.ListRecordsByKey(ctx, "receipts", k, port.FirstPage(10))
			require.NoError(t, err)
			assert.Empty(t, got, name)
		}
		refunds, _, err := s.ListRecordsByKey(ctx, "refunds", order1, port.FirstPage(10))
		require.NoError(t, err)
		require.Len(t, refunds, 1, "tables are separate")
		sameJSON(t, saved[4], refunds[0])
	})

	t.Run("DeleteTable", func(t *testing.T) {
		s := open[recordStore](t, newStore)
		// receipts_v2 shares a name prefix with receipts.
		for _, r := range []*port.Record{
			record("receipts", 5, 0, "0xa", "0x01", "10"),
			record("receipts", 6, 1, "0xb", "0x02", "20"),
			record("receipts_v2", 5, 0, "0xa", "0x01", "10"),
			record("refunds", 5, 0, "0xa", "0x01", "1"),
		} {
			require.NoError(t, s.SaveRecord(ctx, r, keys(r)))
		}
		require.NoError(t, s.DeleteRecords(ctx, "receipts"))

		byMerchant := port.RecordKey{ID: "merchant", Values: []string{"0xa"}}
		got, _, err := s.ListRecords(ctx, "receipts", port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, got, "the table's records are gone")
		got, _, err = s.ListRecordsByKey(ctx, "receipts", byMerchant, port.FirstPage(10))
		require.NoError(t, err)
		assert.Empty(t, got, "and its key entries")
		for _, table := range []string{"receipts_v2", "refunds"} {
			got, _, err = s.ListRecordsByKey(ctx, table, byMerchant, port.FirstPage(10))
			require.NoError(t, err)
			assert.Len(t, got, 1, "%s is kept", table)
		}

		again := record("receipts", 7, 0, "0xa", "0x03", "30")
		require.NoError(t, s.SaveRecord(ctx, again, keys(again)))
		got, _, err = s.ListRecordsByKey(ctx, "receipts", byMerchant, port.FirstPage(10))
		require.NoError(t, err)
		require.Len(t, got, 1, "the table is written again")
		sameJSON(t, again, got[0])
	})
}
