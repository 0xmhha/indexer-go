package capture

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

// RowCodec identifies the bytes returned by Rows; chain extensions use the
// registered model codecs and are never silently dropped by JSON conversion.
func RowCodec(k RowKind) string {
	switch k {
	case RowHeader:
		return "model.block.v1"
	case RowTransaction:
		return "model.transaction.v1"
	case RowReceipt:
		return "model.receipt.v1"
	case RowBody:
		return "evm.body.json.v1"
	case RowLog:
		return "evm.log.json.v1"
	}
	return ""
}

// RowKind is a closed query schema. Accounts/code/storage/trace/blob rows are
// deliberately absent until an adapter can supply and verify those datasets.
type RowKind string

const (
	RowHeader      RowKind = "header"
	RowBody        RowKind = "body"
	RowTransaction RowKind = "transaction"
	RowReceipt     RowKind = "receipt"
	RowLog         RowKind = "log"
	RowSchema              = "evm.source.v1"
)

var rowKinds = []RowKind{RowHeader, RowBody, RowTransaction, RowReceipt, RowLog}

func validRowKind(k RowKind) bool {
	for _, v := range rowKinds {
		if k == v {
			return true
		}
	}
	return false
}

type Row struct {
	Kind  RowKind `json:"kind"`
	Index uint32  `json:"index"`
	Value []byte  `json:"value"`
}

// BodyFacts retains order and optional body lists. Exact encoded body bytes
// (including uncle headers and chain-specific suffixes) remain a dataset.
type BodyFacts struct {
	TransactionHashes []common.Hash      `json:"transactionHashes"`
	Uncles            []common.Hash      `json:"uncles"`
	Withdrawals       []model.Withdrawal `json:"withdrawals"`
}
type RowCount struct {
	Kind  RowKind `json:"kind"`
	Count uint32  `json:"count"`
}
type RowManifest struct {
	Schema string      `json:"schema"`
	Counts []RowCount  `json:"counts"`
	Digest common.Hash `json:"digest"`
}
type storedRow struct {
	Row    Row         `json:"row"`
	Digest common.Hash `json:"digest"`
	Owner  common.Hash `json:"owner"` // binds chain/block/plan/generation, even on page reads
}

const maxRows = 32768
const maxRowBytes = 8 << 20

func rowKey(id model.CaptureIdentity, k RowKind, i uint32) string {
	return fmt.Sprintf("%s/rows/%s/%010d", factsKey(id), k, i)
}
func hashRow(h hash.Hash, r Row) {
	var b [8]byte
	h.Write([]byte(r.Kind))
	h.Write([]byte{0})
	binary.BigEndian.PutUint32(b[:4], r.Index)
	binary.BigEndian.PutUint32(b[4:], uint32(len(r.Value)))
	h.Write(b[:])
	h.Write(r.Value)
}
func rowDigest(r Row) common.Hash {
	h := sha256.New()
	hashRow(h, r)
	return common.BytesToHash(h.Sum(nil))
}

func rowOwner(id model.CaptureIdentity) common.Hash {
	v, _ := json.Marshal(id)
	return sha256.Sum256(v)
}

// DescribeRows validates canonical kind/index order, cardinality and total
// bytes. Exactly one header/body and equal transaction/receipt counts are
// required, including explicit zero counts for an empty verified block.
func DescribeRows(rows []Row) (*RowManifest, error) {
	m := &RowManifest{Schema: RowSchema}
	h := sha256.New()
	h.Write([]byte(RowSchema + "\x00"))
	pos, total := 0, 0
	for _, k := range rowKinds {
		var n uint32
		for pos < len(rows) && rows[pos].Kind == k {
			r := rows[pos]
			if r.Index != n || n >= maxRows || len(r.Value) == 0 || len(r.Value) > maxRowBytes {
				return nil, port.ErrInvalidData
			}
			total += len(r.Value)
			if total > maxBlockBytes {
				return nil, port.ErrInvalidData
			}
			hashRow(h, r)
			n++
			pos++
		}
		m.Counts = append(m.Counts, RowCount{k, n})
	}
	if pos != len(rows) || m.Counts[0].Count != 1 || m.Counts[1].Count != 1 || m.Counts[2].Count != m.Counts[3].Count {
		return nil, port.ErrInvalidData
	}
	m.Digest = common.BytesToHash(h.Sum(nil))
	return m, nil
}
func validateRowManifest(m *RowManifest) error {
	if m == nil {
		return nil
	}
	if m.Schema != RowSchema || len(m.Counts) != len(rowKinds) || m.Digest == (common.Hash{}) {
		return port.ErrInvalidData
	}
	for i, c := range m.Counts {
		if c.Kind != rowKinds[i] || c.Count > maxRows {
			return port.ErrInvalidData
		}
	}
	if m.Counts[0].Count != 1 || m.Counts[1].Count != 1 || m.Counts[2].Count != m.Counts[3].Count {
		return port.ErrInvalidData
	}
	return nil
}
func validateRows(in Input) error {
	if in.Normalized == nil {
		if len(in.Rows) > 0 {
			return port.ErrInvalidData
		}
		return nil
	}
	if e := validateRowManifest(in.Normalized); e != nil {
		return e
	}
	m, e := DescribeRows(in.Rows)
	if e != nil {
		return e
	}
	if !equalJSON(m, in.Normalized) {
		return ErrConflict
	}
	return nil
}
func storeRows(tx port.CaptureWriter, id model.CaptureIdentity, rows []Row) error {
	for _, r := range rows {
		if e := putJSON(tx, rowKey(id, r.Kind, r.Index), storedRow{Row: r, Digest: rowDigest(r), Owner: rowOwner(id)}); e != nil {
			return e
		}
	}
	return nil
}
func loadRow(tx port.CaptureReader, id model.CaptureIdentity, k RowKind, i uint32) (Row, error) {
	var r storedRow
	e := getJSON(tx, rowKey(id, k, i), &r)
	if e != nil {
		return Row{}, e
	}
	if r.Owner != rowOwner(id) || r.Row.Kind != k || r.Row.Index != i || len(r.Row.Value) == 0 || len(r.Row.Value) > maxRowBytes || rowDigest(r.Row) != r.Digest {
		return Row{}, ErrConflict
	}
	return r.Row, nil
}
func verifyRows(tx port.CaptureReader, id model.CaptureIdentity, m *RowManifest) error {
	if e := validateRowManifest(m); e != nil {
		return e
	}
	if m == nil {
		return nil
	}
	h := sha256.New()
	h.Write([]byte(m.Schema + "\x00"))
	total := 0
	for _, c := range m.Counts {
		for i := uint32(0); i < c.Count; i++ {
			r, e := loadRow(tx, id, c.Kind, i)
			if e != nil {
				return e
			}
			total += len(r.Value)
			if total > maxBlockBytes {
				return port.ErrInvalidData
			}
			hashRow(h, r)
		}
	}
	if common.BytesToHash(h.Sum(nil)) != m.Digest {
		return ErrConflict
	}
	return nil
}

type RowPage struct {
	Rows []Row
	Next uint32
	Done bool
}

// Rows queries immutable source facts by block/plan/generation and row kind.
// Offset is the next ordinal, limit <=256; pages have at most 8 MiB of values.
// Missing normalization is an explicit coverage gap, never an empty success.
// Call CanonicalAt separately when selecting the current canonical fork.
func (s *Store) Rows(ctx context.Context, id model.CaptureIdentity, kind RowKind, offset uint32, limit int) (RowPage, error) {
	page := RowPage{Rows: []Row{}, Next: offset}
	if id.Block.Chain != s.chain || !validRowKind(kind) || limit < 1 || limit > 256 {
		return page, port.ErrInvalidData
	}
	err := s.db.CaptureView(ctx, s.chain, func(tx port.CaptureReader) error {
		var in Input
		if e := getJSON(tx, factsKey(id), &in); e != nil {
			return e
		}
		if in.Manifest.Identity != id {
			return ErrConflict
		}
		if in.Normalized == nil {
			return model.ErrCaptureIncomplete
		}
		if e := validateRowManifest(in.Normalized); e != nil {
			return e
		}
		var count uint32
		for _, c := range in.Normalized.Counts {
			if c.Kind == kind {
				count = c.Count
			}
		}
		if offset > count {
			return port.ErrInvalidData
		}
		total := 0
		for page.Next < count && len(page.Rows) < limit {
			r, e := loadRow(tx, id, kind, page.Next)
			if e != nil {
				return e
			}
			if total+len(r.Value) > maxRowBytes {
				break
			}
			total += len(r.Value)
			page.Rows = append(page.Rows, r)
			page.Next++
		}
		page.Done = page.Next == count
		return nil
	})
	return page, err
}
