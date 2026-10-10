package port

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

// Records are the logs a project declared tables for (refactoring plan
// R6-1): each record is one log, identified by its table and log position,
// with its event's arguments as fields. A table's keys are the lookups it
// declares; SaveRecord indexes the record under its values for each.

// Record is one log stored in a declared table.
type Record struct {
	Table       string         `json:"table"`
	BlockNumber uint64         `json:"blockNumber"`
	BlockTime   uint64         `json:"blockTime"` // Unix seconds
	TxHash      common.Hash    `json:"txHash"`
	LogIndex    uint           `json:"logIndex"`
	Address     common.Address `json:"address"`
	// Fields are the event's arguments, formatted as declared.Format does.
	Fields map[string]string `json:"fields"`
}

// RecordKey is a lookup of a table (its ID names the fields) with the
// values a record has, or a query asks for, in the key's field order.
type RecordKey struct {
	ID     string
	Values []string
}

// RecordReader reads records.
type RecordReader interface {
	// ListRecords returns one page of a table's records, oldest first (by
	// block and log index), and the cursor of the next page.
	ListRecords(ctx context.Context, table string, page Page) ([]*Record, string, error)
	// ListRecordsByKey returns one page of the records of a table with the
	// key's values, oldest first.
	ListRecordsByKey(ctx context.Context, table string, key RecordKey, page Page) ([]*Record, string, error)
}

// RecordWriter writes records. A record is identified by its table and
// log position: writing it again replaces it and its key entries.
type RecordWriter interface {
	SaveRecord(ctx context.Context, record *Record, keys []RecordKey) error
	// DeleteRecords removes every record of a table and its key entries
	// (a table rebuilt under a changed definition); other tables are kept.
	DeleteRecords(ctx context.Context, table string) error
}
