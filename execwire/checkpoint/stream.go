// Package checkpoint defines the stdlib-only streaming bootstrap file format.
// Verification, trust, persistence and canonicality belong to its consumers.
package checkpoint

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/0xmhha/indexer-go/execwire"
)

const MaxRecordBytes = 2 << 20
const MaxRecords = 64_000_000

type Manifest struct {
	Version  int
	Mode     string
	Coverage string
	Chain    execwire.Chain
	Ref      execwire.Ref
	Root     execwire.Hash
	Header   []byte
}
type Record struct {
	Kind     string
	Manifest *Manifest         `json:",omitempty"`
	Address  execwire.Address  `json:",omitempty"`
	Account  *execwire.Account `json:",omitempty"`
	Slot     execwire.Word     `json:",omitempty"`
	Value    execwire.Word     `json:",omitempty"`
	Code     []byte            `json:",omitempty"`
	Proof    [][]byte          `json:",omitempty"`
}
type Decoder struct {
	scanner *bufio.Scanner
	count   int
}

func NewDecoder(r io.Reader) *Decoder {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), MaxRecordBytes+1)
	return &Decoder{scanner: s}
}
func (d *Decoder) Next() (Record, error) {
	if !d.scanner.Scan() {
		if err := d.scanner.Err(); err != nil {
			return Record{}, fmt.Errorf("%w: record", execwire.ErrLimit)
		}
		return Record{}, io.EOF
	}
	d.count++
	b := d.scanner.Bytes()
	if d.count > MaxRecords || len(b) > MaxRecordBytes {
		return Record{}, execwire.ErrLimit
	}
	var v Record
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, execwire.ErrProtocol
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return v, execwire.ErrProtocol
	}
	return v, nil
}
func Write(w io.Writer, r Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(b) > MaxRecordBytes {
		return execwire.ErrLimit
	}
	n, err := w.Write(append(b, '\n'))
	if err == nil && n != len(b)+1 {
		return io.ErrShortWrite
	}
	return err
}

// SourceObservation is diagnostic evidence, never an ACK or recovery command.
type SourceObservation struct {
	Chain                                              execwire.Chain
	Anchor, Committed, Parent                          execwire.Ref
	ParentRoot                                         execwire.Hash
	HeaderAvailable, StateAvailable, TransitionPending bool
}
