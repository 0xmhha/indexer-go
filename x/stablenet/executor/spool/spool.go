// Package spool persists execution results until an exact durable indexer ACK.
package spool

import (
	"errors"
	"os"

	"github.com/0xmhha/indexer-go/execwire"
)

var (
	ErrFull     = errors.New("spool: capacity exceeded")
	ErrConflict = errors.New("spool: conflicting reference or digest")
	ErrSequence = errors.New("spool: noncontiguous result or ACK")
	ErrCorrupt  = errors.New("spool: corrupt persisted data")
	ErrBusy     = errors.New("spool: already owned")
	ErrClosed   = errors.New("spool: closed")
	ErrRecovery = errors.New("spool: reopen required after interrupted durable operation")
)

type Options struct {
	Path       string
	Chain      execwire.Chain
	Anchor     execwire.Ref
	MaxBytes   uint64
	MaxRecords int
}
type Entry struct {
	Ref    execwire.Ref
	Parent execwire.Hash
	Digest execwire.Hash
	Size   uint64
}
type Status struct {
	Pending     int
	Bytes       uint64
	Committed   execwire.Ref
	Digest      execwire.Hash
	Quarantined int
}
type fileOps struct {
	syncFile func(*os.File) error
	syncDir  func(string) error
	rename   func(string, string) error
	remove   func(string) error
}
